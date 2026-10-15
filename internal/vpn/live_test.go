package vpn

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// LIVE_CACHE=C:\ProgramData\Pochinka\vpn-cache.json — проверка настоящих
// серверов подписки: поднимаем xray на тестовых портах и ходим через него.
func TestLiveServers(t *testing.T) {
	path := os.Getenv("LIVE_CACHE")
	geo := os.Getenv("GEO_TEST_DIR")
	if path == "" || geo == "" {
		t.Skip("LIVE_CACHE/GEO_TEST_DIR не заданы")
	}
	SetAssetDir(geo)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c cache
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	for i, cs := range c.Servers {
		s := cs.Server
		s.Outbound, s.Extra = cs.Outbound, cs.Extra
		port := 39100 + i*2
		js, err := BuildConfig(BuildOptions{
			Servers: []*Server{&s}, Mode: ModeProxy, HTTPPort: port, SocksPort: port + 1,
			LogFile:     os.DevNull,
			UserDomains: []string{"domain:ifconfig.me", "domain:cloudflare.com"},
		})
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		e := &engine{}
		if err := e.start(js); err != nil {
			t.Errorf("%s: старт: %v", s.Name, err)
			continue
		}
		pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		cl := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
		var res []string
		for _, target := range []string{
			"https://ifconfig.me/ip",                   // через VPN — IP выхода
			"https://www.cloudflare.com/cdn-cgi/trace", // через VPN — страна выхода
			"https://www.instagram.com/",               // через VPN (ru-blocked)
			"https://ya.ru/",                           // напрямую
		} {
			start := time.Now()
			resp, err := cl.Get(target)
			if err != nil {
				msg := err.Error()
				if len(msg) > 90 {
					msg = msg[len(msg)-90:]
				}
				res = append(res, fmt.Sprintf("  %-42s ОШИБКА %s", target, msg))
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			extra := ""
			if strings.Contains(target, "ifconfig") {
				extra = " ip=" + strings.TrimSpace(string(body))
			}
			if strings.Contains(target, "trace") {
				for _, l := range strings.Split(string(body), "\n") {
					if strings.HasPrefix(l, "loc=") {
						extra += " " + l
					}
				}
			}
			res = append(res, fmt.Sprintf("  %-42s %d за %v%s", target, resp.StatusCode, time.Since(start).Round(time.Millisecond), extra))
		}
		e.stop()
		t.Logf("%s [%s %s:%d]\n%s", s.Name, s.Protocol, s.Address, s.Port, strings.Join(res, "\n"))
	}
}
