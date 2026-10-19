package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"pochinka/internal/applog"
)

// SupportReport — текст для «Скопировать отчёт»: версия, состояние модулей
// и хвост журнала. Ссылки подписок, ключи и секреты сюда не попадают: в
// состоянии их нет, а журнал чистится при записи (applog.redact).
func (a *App) SupportReport() string {
	st := a.GetState()
	v := windows.RtlGetVersion()
	var b strings.Builder
	w := func(f string, args ...any) { fmt.Fprintf(&b, f+"\n", args...) }

	w("Починка %s · Windows %d.%d.%d · админ: %v · %s", appVersion, v.MajorVersion, v.MinorVersion, v.BuildNumber,
		st.Admin, time.Now().Format("2006-01-02 15:04"))
	if st.Conflict != "" {
		w("Другой VPN: %s", st.Conflict)
	}

	z := st.Zapret
	w("\n[Обход] %s · zapret %s · стратегия «%s» · порты игр: %s", z.Status, z.Version, prettyStrategy(z.Strategy), orDash(z.GameFilter))
	if z.Error != "" {
		w("  ошибка: %s", z.Error)
	}
	keys := make([]string, 0, len(st.Checks))
	for k := range st.Checks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := st.Checks[k]
		if r.OK {
			w("  %s: открывается (%d мс)", k, r.Latency)
		} else {
			w("  %s: НЕ открывается — %s", k, r.Detail)
		}
	}

	vp := st.VPN
	w("\n[VPN] %s · режим %s · маршрут: %s · подписок: %d · серверов: %d", vp.Status, vp.Mode,
		map[bool]string{true: "всё, кроме российского", false: "только заблокированное"}[st.Config.VPNRouteAll],
		len(vp.Subs), len(vp.Servers))
	if vp.Active != "" {
		w("  сервер: %s", vp.Active)
	}
	if vp.Error != "" {
		w("  ошибка: %s", vp.Error)
	}
	if vp.Check != "" {
		w("  самопроверка: %s", vp.Check)
	}

	t := st.TG
	w("\n[Telegram] работает: %v · Telegram Desktop: %v · соединений: %d (всего %d) · WS/CF/TCP: %d/%d/%d",
		t.Running, t.Installed, t.Stats.Active, t.Stats.Total, t.Stats.ViaWS, t.Stats.ViaCF, t.Stats.ViaTCP)
	if t.Error != "" {
		w("  ошибка: %s", t.Error)
	}

	w("\n[Журнал — последние строки]")
	lines := applog.Tail()
	if len(lines) > 150 {
		lines = lines[len(lines)-150:]
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
