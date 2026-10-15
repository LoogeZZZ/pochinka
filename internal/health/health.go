// Package health проверяет, открываются ли сервисы с этого компьютера.
//
// Проверка идёт обычным HTTPS-запросом — так же, как ходит браузер, поэтому
// winws обрабатывает её точно так же, как настоящий трафик.
package health

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Result struct {
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Latency int64  `json:"latencyMs"`
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

var client = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		Proxy:             nil, // системный прокси не нужен: проверяем прямой путь
		DisableKeepAlives: true,
	},
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
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", t.url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return Result{Detail: shortErr(err)}
	}
	defer resp.Body.Close()
	// Если соединение «замедлено», чтение упрётся в таймаут клиента.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, t.minBytes))
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return Result{Detail: shortErr(err), Latency: ms}
	}
	return Result{OK: true, Latency: ms}
}

func shortErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "Client.Timeout"), strings.Contains(s, "deadline exceeded"):
		return "нет ответа (таймаут)"
	case strings.Contains(s, "connection reset"), strings.Contains(s, "forcibly closed"):
		return "соединение сброшено"
	case strings.Contains(s, "no such host"):
		return "DNS не находит адрес"
	case strings.Contains(s, "EOF"):
		return "соединение оборвано"
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}
