package tgproxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Адреса фронта web.telegram.org: на него ходят WebSocket-соединения
// kwsN.web.telegram.org. Как в оригинале: DC2 и DC4 через 149.154.167.220,
// остальные DC — запасными путями (Cloudflare, прямой TCP).
var defaultDCRedirects = map[int]string{2: "149.154.167.220", 4: "149.154.167.220"}

// Прямые адреса дата-центров — последний запасной путь (обычный MTProto по TCP).
var dcDirectIPs = map[int]string{
	1: "149.154.175.50", 2: "149.154.167.51", 3: "149.154.175.100",
	4: "149.154.167.91", 5: "149.154.171.5", 203: "91.105.192.100",
}

const (
	poolSize   = 4
	poolMaxAge = 110 * time.Second // сервер рвёт простаивающие WS около 120 с
	// Домен для доменного фронтинга, если прямой SNI kwsN.web.telegram.org
	// режут (тот же приём, что в оригинале).
	frontingSNI = "sprinthost.ru"
)

func wsDomains(dc int, media bool) []string {
	if dc == 203 {
		dc = 2
	}
	if !media {
		return []string{fmt.Sprintf("kws%d.web.telegram.org", dc)}
	}
	return []string{fmt.Sprintf("kws%d-1.web.telegram.org", dc), fmt.Sprintf("kws%d.web.telegram.org", dc)}
}

type poolKey struct {
	dc          int
	media, test bool
}

type idleWS struct {
	ws *wsConn
}

// wsPool держит наготове несколько открытых WebSocket к каждому DC, чтобы
// Telegram подключался без задержки на TLS-рукопожатие.
type wsPool struct {
	p *Proxy

	mu        sync.Mutex
	idle      map[poolKey][]*wsConn
	refilling map[poolKey]bool
	retryAt   map[poolKey]time.Time
	failures  map[poolKey]int
	fronting  bool // в прошлый раз сработал фронтинг — пробуем его первым
	closed    bool
}

func newWSPool(p *Proxy) *wsPool {
	return &wsPool{p: p, idle: map[poolKey][]*wsConn{}, refilling: map[poolKey]bool{},
		retryAt: map[poolKey]time.Time{}, failures: map[poolKey]int{}}
}

func (wp *wsPool) target(dc int) string {
	return wp.p.dcRedirects[dc]
}

// get отдаёт готовое соединение или nil (тогда — запасные пути).
func (wp *wsPool) get(key poolKey) *wsConn {
	if wp.target(key.dc) == "" {
		return nil
	}
	wp.mu.Lock()
	var got *wsConn
	for len(wp.idle[key]) > 0 {
		ws := wp.idle[key][0]
		wp.idle[key] = wp.idle[key][1:]
		if time.Since(ws.created) < poolMaxAge && ws.alive() {
			got = ws
			break
		}
		go ws.close()
	}
	wp.mu.Unlock()
	wp.refill(key)
	if got != nil {
		return got
	}
	// Пул пуст — пробуем подключиться прямо сейчас (не ждать фоновой докачки).
	ctx, cancel := context.WithTimeout(wp.p.ctx, 8*time.Second)
	defer cancel()
	ws, _ := wp.connect(ctx, key)
	return ws
}

func (wp *wsPool) refill(key poolKey) {
	wp.mu.Lock()
	if wp.closed || wp.refilling[key] || time.Now().Before(wp.retryAt[key]) {
		wp.mu.Unlock()
		return
	}
	need := poolSize - len(wp.idle[key])
	if need <= 0 {
		wp.mu.Unlock()
		return
	}
	wp.refilling[key] = true
	wp.mu.Unlock()

	go func() {
		ok := 0
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := 0; i < need; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(wp.p.ctx, 10*time.Second)
				defer cancel()
				ws, err := wp.connect(ctx, key)
				if err != nil {
					return
				}
				wp.mu.Lock()
				if wp.closed {
					wp.mu.Unlock()
					ws.close()
					return
				}
				wp.idle[key] = append(wp.idle[key], ws)
				wp.mu.Unlock()
				mu.Lock()
				ok++
				mu.Unlock()
			}()
		}
		wg.Wait()
		wp.mu.Lock()
		delete(wp.refilling, key)
		if ok > 0 {
			delete(wp.failures, key)
			delete(wp.retryAt, key)
		} else {
			wp.failures[key]++
			backoff := time.Second << min(wp.failures[key]-1, 10)
			if backoff > 15*time.Minute {
				backoff = 15 * time.Minute
			}
			wp.retryAt[key] = time.Now().Add(backoff)
		}
		wp.mu.Unlock()
	}()
}

// connect: WS к фронту Telegram — сначала с честным SNI, потом фронтингом
// (или наоборот, если в прошлый раз сработал фронтинг).
func (wp *wsPool) connect(ctx context.Context, key poolKey) (*wsConn, error) {
	addr := net.JoinHostPort(wp.target(key.dc), "443")
	path := "/apiws"
	if key.test {
		path = "/apiws_test"
	}
	wp.mu.Lock()
	order := []bool{false, true}
	if wp.fronting {
		order = []bool{true, false}
	}
	wp.mu.Unlock()
	var lastErr error
	for _, domain := range wsDomains(key.dc, key.media) {
		for _, fronted := range order {
			sni := ""
			if fronted {
				sni = frontingSNI
			}
			ws, err := wsDial(ctx, wp.p.dial, addr, domain, sni, path, true, fronted, wp.p.tlsConfig)
			if err != nil {
				lastErr = err
				continue
			}
			wp.mu.Lock()
			wp.fronting = fronted
			wp.mu.Unlock()
			return ws, nil
		}
	}
	return nil, lastErr
}

// warmup — заранее открыть соединения к DC с фронтом.
func (wp *wsPool) warmup() {
	for dc := range wp.p.dcRedirects {
		wp.refill(poolKey{dc: dc})
		wp.refill(poolKey{dc: dc, media: true})
	}
}

// maintain выкидывает протухшие соединения и докачивает пул.
func (wp *wsPool) maintain() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-wp.p.ctx.Done():
			return
		case <-t.C:
		}
		wp.mu.Lock()
		var keys []poolKey
		for k, list := range wp.idle {
			var keep []*wsConn
			for _, ws := range list {
				if time.Since(ws.created) < poolMaxAge {
					keep = append(keep, ws)
				} else {
					go ws.close()
				}
			}
			wp.idle[k] = keep
			keys = append(keys, k)
		}
		wp.mu.Unlock()
		for _, k := range keys {
			wp.refill(k)
		}
	}
}

func (wp *wsPool) close() {
	wp.mu.Lock()
	wp.closed = true
	all := wp.idle
	wp.idle = map[poolKey][]*wsConn{}
	wp.mu.Unlock()
	for _, list := range all {
		for _, ws := range list {
			ws.close()
		}
	}
}
