package tgproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Stats — счётчики для окна.
type Stats struct {
	Active   int64 `json:"active"`
	Total    int64 `json:"total"`
	BytesUp  int64 `json:"bytesUp"`
	BytesDn  int64 `json:"bytesDown"`
	ViaWS    int64 `json:"viaWs"`
	ViaCF    int64 `json:"viaCf"`
	ViaTCP   int64 `json:"viaTcp"`
	Failed   int64 `json:"failed"`
	BadHello int64 `json:"badHello"`
}

// Proxy — локальный MTProto-прокси.
type Proxy struct {
	secret      []byte
	dcRedirects map[int]string
	dial        dialFunc
	tlsConfig   *tls.Config // nil — системные корневые сертификаты
	cf          *cfDomains
	pool        *wsPool
	logf        func(format string, a ...any)

	ctx    context.Context
	cancel context.CancelFunc
	ln     net.Listener
	wg     sync.WaitGroup

	connsMu sync.Mutex
	conns   map[net.Conn]struct{}

	tcpMu      sync.Mutex
	tcpRetryAt map[string]time.Time
	badLogAt   time.Time

	stats Stats
}

// Options — настройки прокси (нулевые значения — по умолчанию).
type Options struct {
	Listen      string // "127.0.0.1:1443"
	Secret      []byte // 16 байт
	DCRedirects map[int]string
	Dial        func(ctx context.Context, network, addr string) (net.Conn, error)
	TLSConfig   *tls.Config
	NoCF        bool
	Logf        func(format string, a ...any)
}

// Лимит одновременных клиентов: Telegram Desktop держит десятки соединений,
// но не тысячи — защита от того, чтобы кто-то локально завалил процесс.
const maxClients = 512

func Start(o Options) (*Proxy, error) {
	if len(o.Secret) != 16 {
		return nil, errBadSecret
	}
	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		secret: o.Secret, dcRedirects: o.DCRedirects, dial: o.Dial, tlsConfig: o.TLSConfig,
		logf: o.Logf, ctx: ctx, cancel: cancel, ln: ln,
		conns: map[net.Conn]struct{}{}, tcpRetryAt: map[string]time.Time{},
	}
	if p.dcRedirects == nil {
		p.dcRedirects = defaultDCRedirects
	}
	if p.dial == nil {
		p.dial = defaultDial
	}
	if p.logf == nil {
		p.logf = func(string, ...any) {}
	}
	if !o.NoCF {
		p.cf = newCFDomains()
		go func() {
			p.cf.refresh(ctx)
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					p.cf.refresh(ctx)
				}
			}
		}()
	}
	p.pool = newWSPool(p)
	p.pool.warmup()
	go p.pool.maintain()
	p.wg.Add(1)
	go p.serve()
	return p, nil
}

// Addr — фактический адрес (если слушали порт 0).
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// CFDomains — домены Cloudflare-фолбэка (для списков обхода zapret).
func (p *Proxy) CFDomains() []string {
	if p.cf == nil {
		return nil
	}
	return p.cf.all()
}

func (p *Proxy) Stats() Stats {
	return Stats{
		Active: atomic.LoadInt64(&p.stats.Active), Total: atomic.LoadInt64(&p.stats.Total),
		BytesUp: atomic.LoadInt64(&p.stats.BytesUp), BytesDn: atomic.LoadInt64(&p.stats.BytesDn),
		ViaWS: atomic.LoadInt64(&p.stats.ViaWS), ViaCF: atomic.LoadInt64(&p.stats.ViaCF),
		ViaTCP: atomic.LoadInt64(&p.stats.ViaTCP), Failed: atomic.LoadInt64(&p.stats.Failed),
		BadHello: atomic.LoadInt64(&p.stats.BadHello),
	}
}

// Close останавливает приём и закрывает все соединения.
func (p *Proxy) Close() {
	p.cancel()
	_ = p.ln.Close()
	p.connsMu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.connsMu.Unlock()
	p.pool.close()
	p.wg.Wait()
}

func (p *Proxy) track(c net.Conn, on bool) {
	p.connsMu.Lock()
	if on {
		p.conns[c] = struct{}{}
	} else {
		delete(p.conns, c)
	}
	p.connsMu.Unlock()
}

func (p *Proxy) serve() {
	defer p.wg.Done()
	sem := make(chan struct{}, maxClients)
	for {
		c, err := p.ln.Accept()
		if err != nil {
			if p.ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			_ = c.Close() // перегрузка
			continue
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() { <-sem }()
			p.handle(c)
		}()
	}
}

func (p *Proxy) handle(c net.Conn) {
	p.track(c, true)
	defer p.track(c, false)
	defer c.Close()
	atomic.AddInt64(&p.stats.Total, 1)
	atomic.AddInt64(&p.stats.Active, 1)
	defer atomic.AddInt64(&p.stats.Active, -1)
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}

	// Заголовок должен прийти быстро: иначе это не Telegram.
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	hs := make([]byte, handshakeLen)
	if _, err := io.ReadFull(c, hs); err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	h := parseClientHello(hs, p.secret)
	if h == nil {
		// В журнал — не чаще раза в минуту: иначе любая локальная программа
		// засыпала бы его мусорными подключениями.
		if n := atomic.AddInt64(&p.stats.BadHello, 1); n == 1 || time.Since(p.lastBadLog()) > time.Minute {
			p.logf("tg: чужое подключение (неверный секрет или протокол), всего %d", n)
		}
		return
	}
	test := false
	dc := h.dc
	if dc >= 10000 {
		dc -= 10000
		test = true
	}
	dcIdx := dc
	if h.media {
		dcIdx = -dc
	}
	relay, err := relayInit(h.protoTag, dcIdx)
	if err != nil {
		return
	}
	ctx := newCryptoCtx(h, p.secret, relay)
	key := poolKey{dc: dc, media: h.media, test: test}

	if ws := p.pool.get(key); ws != nil {
		atomic.AddInt64(&p.stats.ViaWS, 1)
		p.bridgeWS(c, ws, relay, ctx, h.protoTag)
		return
	}
	if !test && p.cf != nil {
		if ws := p.cfConnect(dc, h.media); ws != nil {
			atomic.AddInt64(&p.stats.ViaCF, 1)
			p.bridgeWS(c, ws, relay, ctx, h.protoTag)
			return
		}
	}
	if !test {
		if rc := p.tcpConnect(dc); rc != nil {
			atomic.AddInt64(&p.stats.ViaTCP, 1)
			p.bridgeTCP(c, rc, relay, ctx)
			return
		}
	}
	atomic.AddInt64(&p.stats.Failed, 1)
	p.logf("tg: DC%d — ни один путь до Telegram не сработал", dc)
}

// cfConnect — WS через Cloudflare: kwsN.<домен>, TLS к самому домену.
func (p *Proxy) cfConnect(dc int, media bool) *wsConn {
	if dc == 203 {
		dc = 2
	}
	for i, base := range p.cf.forDC(dc) {
		if i >= 4 { // не больше четырёх попыток на соединение
			break
		}
		domain := fmt.Sprintf("kws%d.%s", dc, base)
		ctx, cancel := context.WithTimeout(p.ctx, 8*time.Second)
		ws, err := wsDial(ctx, p.dial, net.JoinHostPort(domain, "443"), domain, "", "/apiws", true, false, p.tlsConfig)
		cancel()
		if err == nil {
			p.cf.worked(dc, base)
			return ws
		}
	}
	return nil
}

// tcpConnect — прямой MTProto по TCP до DC (с откатом после неудач).
func (p *Proxy) tcpConnect(dc int) net.Conn {
	ip := dcDirectIPs[dc]
	if ip == "" {
		return nil
	}
	addr := net.JoinHostPort(ip, "443")
	p.tcpMu.Lock()
	if time.Now().Before(p.tcpRetryAt[addr]) {
		p.tcpMu.Unlock()
		return nil
	}
	p.tcpMu.Unlock()
	ctx, cancel := context.WithTimeout(p.ctx, 8*time.Second)
	defer cancel()
	rc, err := p.dial(ctx, "tcp", addr)
	if err != nil {
		p.tcpMu.Lock()
		p.tcpRetryAt[addr] = time.Now().Add(30 * time.Second)
		p.tcpMu.Unlock()
		return nil
	}
	return rc
}

// bridgeWS — клиент (TCP) ⇄ Telegram (WS) с перешифрованием.
func (p *Proxy) bridgeWS(c net.Conn, ws *wsConn, relay []byte, cc *cryptoCtx, tag [4]byte) {
	defer ws.close()
	if err := ws.send(relay); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	sp := &splitter{tag: tag}
	go func() { // клиент → Telegram
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64<<10)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				atomic.AddInt64(&p.stats.BytesUp, int64(n))
				plain := make([]byte, n)
				cc.cltDec.XORKeyStream(plain, buf[:n])
				enc := make([]byte, n)
				cc.tgEnc.XORKeyStream(enc, plain)
				if parts := sp.split(enc, plain); len(parts) > 0 {
					if ws.send(parts...) != nil {
						return
					}
				}
			}
			if err != nil {
				if tail := sp.flush(); tail != nil {
					_ = ws.send(tail)
				}
				return
			}
		}
	}()
	go func() { // Telegram → клиент
		defer func() { done <- struct{}{} }()
		for {
			data, err := ws.recv()
			if err != nil {
				return
			}
			atomic.AddInt64(&p.stats.BytesDn, int64(len(data)))
			cc.tgDec.XORKeyStream(data, data)
			cc.cltEnc.XORKeyStream(data, data)
			if _, err := c.Write(data); err != nil {
				return
			}
		}
	}()
	<-done
	// Одна сторона закрылась — закрываем обе, чтобы вторая горутина вышла.
	_ = c.Close()
	ws.close()
	<-done
}

// bridgeTCP — клиент ⇄ DC напрямую по TCP.
func (p *Proxy) bridgeTCP(c net.Conn, rc net.Conn, relay []byte, cc *cryptoCtx) {
	defer rc.Close()
	if _, err := rc.Write(relay); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	pipe := func(src, dst net.Conn, dec, enc interface{ XORKeyStream(dst, src []byte) }, ctr *int64) {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				atomic.AddInt64(ctr, int64(n))
				b := buf[:n]
				dec.XORKeyStream(b, b)
				enc.XORKeyStream(b, b)
				if _, werr := dst.Write(b); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go pipe(c, rc, cc.cltDec, cc.tgEnc, &p.stats.BytesUp)
	go pipe(rc, c, cc.tgDec, cc.cltEnc, &p.stats.BytesDn)
	<-done
	_ = c.Close()
	_ = rc.Close()
	<-done
}

// lastBadLog возвращает время прошлой записи о чужом подключении и
// запоминает текущее.
func (p *Proxy) lastBadLog() time.Time {
	p.tcpMu.Lock()
	defer p.tcpMu.Unlock()
	prev := p.badLogAt
	p.badLogAt = time.Now()
	return prev
}
