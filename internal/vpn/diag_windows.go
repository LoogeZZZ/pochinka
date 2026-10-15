package vpn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pochinka/internal/config"
	"pochinka/internal/paths"
)

// DiagTUN поднимает TUN на живом сервере из кэша подписки, прогоняет
// проверки и пишет подробный отчёт. Запуск: Pochinka.exe --diag-tun.
func DiagTUN() string {
	var r strings.Builder
	say := func(f string, a ...any) { fmt.Fprintf(&r, f+"\n", a...) }
	report := filepath.Join(paths.LogsDir(), "diag-tun.txt")
	defer func() { _ = os.WriteFile(report, []byte(r.String()), 0o644) }()

	say("Диагностика TUN %s", time.Now().Format("2006-01-02 15:04:05"))
	SetAssetDir(assetDir())
	if err := EnsureGeo(assetDir()); err != nil {
		say("geo: %v", err)
		return report
	}
	subs := loadSubs()
	if len(subs) == 0 {
		say("нет подписок")
		return report
	}
	sub := subs[0]
	for _, s := range subs {
		if s.ID == config.Load().VPNActiveSub {
			sub = s
		}
	}
	say("подписка: %s", sub.title())
	servers := sub.servers
	pings := PingAll(context.Background(), servers)
	var alive []*Server
	for _, s := range servers {
		say("пинг %5d мс  %s", pings[s.ID], s.Name)
		if pings[s.ID] > 0 {
			alive = append(alive, s)
		}
	}
	sort.SliceStable(alive, func(i, j int) bool { return pings[alive[i].ID] < pings[alive[j].ID] })
	if len(alive) == 0 {
		say("нет живых серверов")
		return report
	}

	phys, err := PhysicalInterface()
	if err != nil {
		say("физ. интерфейс: %v", err)
		return report
	}
	say("физ. интерфейс: %q index=%d", phys.Name, phys.Index)
	if err := ensureWintun(); err != nil {
		say("wintun: %v", err)
		return report
	}
	cfg := config.Load()
	xlog := filepath.Join(paths.LogsDir(), "diag-xray.log")
	_ = os.Remove(xlog)
	js, err := BuildConfig(BuildOptions{
		Servers: alive[:1], Mode: ModeTUN, PhysIface: phys.Name,
		HTTPPort: cfg.VPNHTTPPort + 10, SocksPort: cfg.VPNHTTPPort + 11, LogFile: xlog,
	})
	if err != nil {
		say("конфиг: %v", err)
		return report
	}
	var m map[string]any
	_ = json.Unmarshal(js, &m)
	m["log"] = map[string]any{"loglevel": "debug", "access": xlog, "error": xlog}
	js, _ = json.Marshal(m)

	e := &engine{}
	if err := e.start(js); err != nil {
		say("xray: %v", err)
		return report
	}
	defer e.stop()
	if err := setupTunRoutes(); err != nil {
		say("маршруты: %v", err)
	}
	time.Sleep(3 * time.Second)
	say("\n--- адрес TUN ---\n%s", run("netsh", "interface", "ipv4", "show", "addresses", TunName))
	say("--- маршруты ---\n%s", run("route", "print", "-4"))

	if addrs, err := net.LookupHost("ya.ru"); err != nil {
		say("DNS ya.ru: %v", err)
	} else {
		say("DNS ya.ru: %v", addrs)
	}
	cl := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	for _, u := range []string{"https://ya.ru/", "http://1.1.1.1/", "https://www.gstatic.com/generate_204", "https://www.instagram.com/", "https://ifconfig.me/ip"} {
		start := time.Now()
		resp, err := cl.Get(u)
		if err != nil {
			say("%-40s ОШИБКА %v", u, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		resp.Body.Close()
		extra := ""
		if strings.Contains(u, "ifconfig") {
			extra = " ip=" + strings.TrimSpace(string(body))
		}
		say("%-40s %d за %v%s", u, resp.StatusCode, time.Since(start).Round(time.Millisecond), extra)
	}

	// Перезапуск TUN внутри одного процесса — то, что раньше падало с
	// «initialization already completed».
	say("\n--- перезапуск TUN ---")
	e.stop()
	say("адаптер исчез после остановки: %v", waitTunGone(3*time.Second))
	if err := e.start(js); err != nil {
		say("повторный старт: ОШИБКА %v", err)
		return report
	}
	if err := setupTunRoutes(); err != nil {
		say("маршруты после перезапуска: %v", err)
	}
	time.Sleep(2 * time.Second)
	for _, u := range []string{"https://ya.ru/", "https://www.instagram.com/"} {
		start := time.Now()
		resp, err := cl.Get(u)
		if err != nil {
			say("%-40s ОШИБКА %v", u, err)
			continue
		}
		resp.Body.Close()
		say("%-40s %d за %v", u, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	}
	e.stop()
	say("адаптер исчез после выключения: %v", waitTunGone(3*time.Second))
	left := strings.Contains(run("route", "print", "-4"), "198.18.0.1")
	say("маршруты TUN остались после выключения: %v", left)
	return report
}

func run(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%s (ошибка: %v)", out, err)
	}
	return string(out)
}
