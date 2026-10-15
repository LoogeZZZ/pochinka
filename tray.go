package main

import (
	"runtime"
	"strings"
	"sync"

	"github.com/energye/systray"

	"pochinka/internal/assets"
	"pochinka/internal/config"
	"pochinka/internal/vpn"
	"pochinka/internal/zapret"
)

// trayText — подписи трея на двух языках.
var trayText = map[string]map[string]string{
	"ru": {"open": "Открыть окно", "connect": "Подключить", "disconnect": "Отключить всё", "quit": "Выход",
		"off": "выключено", "err": "ошибка", "starting": "подключаюсь…", "zapret": "обход"},
	"en": {"open": "Open window", "connect": "Connect", "disconnect": "Disconnect all", "quit": "Exit",
		"off": "off", "err": "error", "starting": "connecting…", "zapret": "unblock"},
}

func trayT(key string) string {
	if m, ok := trayText[config.Load().Lang]; ok {
		return m[key]
	}
	return trayText["ru"][key]
}

// tray — иконка в области уведомлений. Работает в своём потоке ОС со своим
// циклом сообщений, независимо от окна Wails.
type tray struct {
	app                *App
	open, toggle, quit *systray.MenuItem

	mu    sync.Mutex
	last  string
	state UIState
}

func startTray(app *App) *tray {
	t := &tray{app: app}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		systray.Run(func() {
			systray.SetIcon(assets.TrayOff)
			systray.SetTooltip("Починка — " + trayT("off"))
			systray.SetOnClick(func(systray.IMenu) { app.showWindow() })
			systray.SetOnRClick(func(m systray.IMenu) { m.ShowMenu() })

			t.open = systray.AddMenuItem(trayT("open"), "")
			t.open.Click(app.showWindow)
			t.toggle = systray.AddMenuItem(trayT("connect"), "")
			t.toggle.Click(func() { go app.Toggle() })
			systray.AddSeparator()
			t.quit = systray.AddMenuItem(trayT("quit"), "")
			t.quit.Click(func() { go app.Quit() })
			close(ready)
		}, nil)
	}()
	<-ready
	return t
}

// relabel — после смены языка.
func (t *tray) relabel() {
	t.open.SetTitle(trayT("open"))
	t.quit.SetTitle(trayT("quit"))
	t.mu.Lock()
	t.last = ""
	st := t.state
	t.mu.Unlock()
	t.update(st)
}

func (t *tray) update(st UIState) {
	z, v := st.Zapret.Status, st.VPN.Status
	key := string(z) + "|" + string(v) + "|" + st.VPN.Active
	t.mu.Lock()
	t.state = st
	if key == t.last {
		t.mu.Unlock()
		return
	}
	t.last = key
	t.mu.Unlock()

	var parts []string
	if z == zapret.StatusRunning {
		parts = append(parts, trayT("zapret"))
	}
	if v == vpn.StatusRunning {
		parts = append(parts, "VPN")
	}
	switch {
	case z == zapret.StatusError || v == vpn.StatusError:
		systray.SetIcon(assets.TrayErr)
		systray.SetTooltip("Починка — " + trayT("err"))
		t.toggle.SetTitle(trayT("disconnect"))
	case len(parts) > 0:
		systray.SetIcon(assets.TrayOn)
		systray.SetTooltip("Починка — " + strings.Join(parts, " + "))
		t.toggle.SetTitle(trayT("disconnect"))
	case z == zapret.StatusStarting || z == zapret.StatusInstalling || v == vpn.StatusStarting:
		systray.SetTooltip("Починка — " + trayT("starting"))
		t.toggle.SetTitle(trayT("disconnect"))
	default:
		systray.SetIcon(assets.TrayOff)
		systray.SetTooltip("Починка — " + trayT("off"))
		t.toggle.SetTitle(trayT("connect"))
	}
}

func (t *tray) stop() { systray.Quit() }
