// Package health проверяет, открываются ли сервисы с этого компьютера.
//
// Проверка идёт обычным HTTPS-запросом — так же, как ходит браузер, поэтому
// winws обрабатывает её точно так же, как настоящий трафик.
package health

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Result struct {
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Latency int64  `json:"latencyMs"`
	// DNSBlocked — DNS провайдера не отдаёт адрес, а защищённый DNS отдаёт:
	// браузер с обычным DNS сайт не откроет, даже если обход работает.
	DNSBlocked bool `json:"dnsBlocked"`
}

type target struct {
	key, url string
	// minBytes: ТСПУ часто не рвёт соединение, а «замедляет» его после первых
	// ~16 КБ. Читаем больше этого порога, чтобы замедление считалось провалом.
	minBytes int64
}

var targets = []target{
	{"discord", "https://discord.com/", 48 << 10},
	{"youtube", "https://www.youtube.com/", 96 << 10},
}

var client = newClient(nil)

// newClient — клиент проверки. resolved != nil — соединяться по этому
// адресу вместо системного DNS (SNI и заголовок Host остаются прежними,
// поэтому для обхода и сайта запрос неотличим от обычного).
func newClient(resolved net.IP) *http.Client {
	tr := &http.Transport{
		Proxy:             nil, // системный прокси не нужен: проверяем прямой путь
		DisableKeepAlives: true,
	}
	if resolved != nil {
		d := &net.Dialer{Timeout: 8 * time.Second}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return d.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
		}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: tr}
}

// CheckAll проверяет все сервисы параллельно.
func CheckAll(ctx context.Context) map[string]Result {
	out := make(map[string]Result, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			r := check(ctx, t)
			mu.Lock()
			out[t.key] = r
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return out
}

func check(ctx context.Context, t target) Result {
	r, err := fetch(ctx, client, t)
	if err == nil || !isNotFound(err) {
		return r
	}
	// Системный DNS не нашёл имя. Спрашиваем защищённый DNS: если он знает
	// адрес — виноват DNS провайдера, а заодно проверяем сам обход по этому
	// адресу, чтобы подбор стратегий не браковал рабочие из-за DNS.
	u, _ := url.Parse(t.url)
	ip, derr := ResolveDoH(ctx, u.Hostname())
	if derr != nil {
		return Result{Detail: "DNS не находит адрес — похоже, нет интернета или DNS не работает совсем"}
	}
	r2, err2 := fetch(ctx, newClient(ip), t)
	r2.DNSBlocked = true
	if err2 != nil {
		r2.Detail = "DNS провайдера не отдаёт адрес; по защищённому DNS — " + r2.Detail
	}
	return r2
}

func fetch(ctx context.Context, cl *http.Client, t target) (Result, error) {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", t.url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36")
	resp, err := cl.Do(req)
	if err != nil {
		return Result{Detail: shortErr(err)}, err
	}
	defer resp.Body.Close()
	// Если соединение «замедлено», чтение упрётся в таймаут клиента.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, t.minBytes))
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Result{Detail: shortErr(err), Latency: ms}, err
	}
	return Result{OK: true, Latency: ms}, nil
}

// isNotFound — имя не нашлось (NXDOMAIN или DNS не ответил вовсе).
func isNotFound(err error) bool {
	var de *net.DNSError
	if errors.As(err, &de) {
		return true
	}
	return strings.Contains(err.Error(), "no such host")
}

func shortErr(err error) string {
	s := err.Error()
	switch {
	case isNotFound(err):
		return "DNS не находит адрес"
	case strings.Contains(s, "Client.Timeout"), strings.Contains(s, "deadline exceeded"):
		return "нет ответа (таймаут)"
	case strings.Contains(s, "connection reset"), strings.Contains(s, "forcibly closed"):
		return "соединение сброшено"
	case strings.Contains(s, "EOF"):
		return "соединение оборвано"
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}
