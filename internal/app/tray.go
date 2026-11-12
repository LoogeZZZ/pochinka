package app

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

// trayText - подписи трея на двух языках.
var trayText = map[string]map[string]string{
	"ru": {"open": "Открыть окно", "connect": "Подключить", "disconnect": "Отключить всё", "quit": "Выход",
		"off": "выключено", "err": "ошибка", "starting": "подключаюсь…", "zapret": "обход",
		"servers": "Серверы", "strategies": "Стратегии"},
	"en": {"open": "Open window", "connect": "Connect", "disconnect": "Disconnect all", "quit": "Exit",
		"off": "off", "err": "error", "starting": "connecting…", "zapret": "unblock",
		"servers": "Servers", "strategies": "Strategies"},
}

func trayT(key string) string {
	if m, ok := trayText[config.Load().Lang]; ok {
		return m[key]
	}
	return trayText["ru"][key]
}

// tray - иконка в области уведомлений. Работает в своём потоке ОС со своим
// циклом сообщений, независимо от окна Wails.
type tray struct {
	app                *App
	open, toggle, quit *systray.MenuItem

	// Подменю серверов и стратегий: пункты создаются один раз, содержимое
	// обновляется из состояния (у systray нет удаления пунктов - лишние
	// просто прячутся).
	servers, strategies *systray.MenuItem
	srvSlots            []*srvSlot
	stratSlots          []*stratSlot

	mu      sync.Mutex
	last    string
	listSig string
	state   UIState
}

// srvSlot - пункт подменю «Серверы» и его текущий сервер.
type srvSlot struct {
	item *systray.MenuItem
	id   string
}

// stratSlot - пункт подменю «Стратегии» и его текущая стратегия.
type stratSlot struct {
	item *systray.MenuItem
	name string
}

const (
	maxTrayServers    = 15 // остальные - в окне
	maxTrayStrategies = 12
)

func startTray(app *App) *tray {
	t := &tray{app: app}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		systray.Run(func() {
			systray.SetIcon(assets.TrayOff)
			systray.SetTooltip("Починка - " + trayT("off"))
			systray.SetOnClick(func(systray.IMenu) { app.ShowWindow() })
			systray.SetOnRClick(func(m systray.IMenu) { m.ShowMenu() })

			t.open = systray.AddMenuItem(trayT("open"), "")
			t.open.Click(app.ShowWindow)
			t.toggle = systray.AddMenuItem(trayT("connect"), "")
			t.toggle.Click(func() { go app.Toggle() })
			systray.AddSeparator()

			// Подменю серверов: клик выбирает сервер, галочка - текущий.
			t.servers = systray.AddMenuItem(trayT("servers"), "")
			for i := 0; i < maxTrayServers; i++ {
				slot := &srvSlot{item: t.servers.AddSubMenuItemCheckbox("", "", false)}
				slot.item.Click(func() { go t.pickServer(slot) })
				t.srvSlots = append(t.srvSlots, slot)
			}
			// Подменю стратегий обхода.
			t.strategies = systray.AddMenuItem(trayT("strategies"), "")
			for i := 0; i < maxTrayStrategies; i++ {
				slot := &stratSlot{item: t.strategies.AddSubMenuItemCheckbox("", "", false)}
				slot.item.Click(func() { go t.pickStrategy(slot) })
				t.stratSlots = append(t.stratSlots, slot)
			}
			systray.AddSeparator()

			t.quit = systray.AddMenuItem(trayT("quit"), "")
			t.quit.Click(func() { go app.Quit() })
			close(ready)
		}, nil)
	}()
	<-ready
	return t
}

// pickServer / pickStrategy - клики по пунктам подменю.
func (t *tray) pickServer(s *srvSlot) {
	t.mu.Lock()
	id := s.id
	t.mu.Unlock()
	if id != "" {
		t.app.SelectServer(id)
	}
}

func (t *tray) pickStrategy(s *stratSlot) {
	t.mu.Lock()
	name := s.name
	t.mu.Unlock()
	if name != "" {
		t.app.SetStrategy(name)
	}
}

// relabel - после смены языка.
func (t *tray) relabel() {
	t.open.SetTitle(trayT("open"))
	t.quit.SetTitle(trayT("quit"))
	t.servers.SetTitle(trayT("servers"))
	t.strategies.SetTitle(trayT("strategies"))
	t.mu.Lock()
	t.last = ""
	st := t.state
	t.mu.Unlock()
	t.update(st)
}

// updateLists обновляет подменю из состояния - только когда набор серверов
// или стратегий поменялся, чтобы не дёргать меню на каждое событие.
func (t *tray) updateLists(st UIState) {
	sig := strings.Join(serverNames(st.VPN.Servers), "|") + "||" + strings.Join(st.Zapret.Strategies, "|") +
		"|" + st.VPN.Selected + "|" + st.Zapret.Strategy + "|" + config.Load().Lang
	if sig == t.listSig {
		return
	}
	t.listSig = sig

	t.mu.Lock()
	servers := st.VPN.Servers
	strats := st.Zapret.Strategies
	for i, slot := range t.srvSlots {
		if i < len(servers) && i < maxTrayServers {
			slot.id = servers[i].ID
			slot.item.SetTitle(servers[i].Name)
			slot.item.SetTooltip(servers[i].Protocol)
			slot.item.Show()
			if servers[i].ID == st.VPN.Selected {
				slot.item.Check()
			} else {
				slot.item.Uncheck()
			}
		} else {
			slot.id = ""
			slot.item.Hide()
		}
	}
	for i, slot := range t.stratSlots {
		if i < len(strats) && i < maxTrayStrategies {
			slot.name = strats[i]
			slot.item.SetTitle(strats[i])
			slot.item.Show()
			if strats[i] == st.Zapret.Strategy {
				slot.item.Check()
			} else {
				slot.item.Uncheck()
			}
		} else {
			slot.name = ""
			slot.item.Hide()
		}
	}
	t.mu.Unlock()

	t.servers.Show()
	t.strategies.Show()
	if len(servers) == 0 {
		t.servers.Hide()
	}
	if len(strats) == 0 {
		t.strategies.Hide()
	}
}

func serverNames(list []vpn.ServerView) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

func (t *tray) update(st UIState) {
	t.updateLists(st)
	z, v := st.Zapret.Status, st.VPN.Status
	key := string(z) + "|" + string(v) + "|" + st.VPN.Active + "|" + st.Zapret.Strategy
	t.mu.Lock()
	t.state = st
	if key == t.last {
		t.mu.Unlock()
		return
	}
	t.last = key
	t.mu.Unlock()

	// Тултип: что включено и чем именно (сервер/стратегия).
	var parts []string
	if z == zapret.StatusRunning {
		p := trayT("zapret")
		if st.Zapret.Strategy != "" {
			p += " · " + st.Zapret.Strategy
		}
		parts = append(parts, p)
	}
	if v == vpn.StatusRunning {
		p := "VPN"
		if st.VPN.Active != "" && st.VPN.Active != "Авто" {
			p += " · " + st.VPN.Active
		}
		parts = append(parts, p)
	}
	switch {
	case z == zapret.StatusError || v == vpn.StatusError:
		systray.SetIcon(assets.TrayErr)
		systray.SetTooltip("Починка - " + trayT("err"))
		t.toggle.SetTitle(trayT("disconnect"))
	case len(parts) > 0:
		systray.SetIcon(assets.TrayOn)
		systray.SetTooltip("Починка - " + strings.Join(parts, " + "))
		t.toggle.SetTitle(trayT("disconnect"))
	case z == zapret.StatusStarting || z == zapret.StatusInstalling || v == vpn.StatusStarting:
		systray.SetTooltip("Починка - " + trayT("starting"))
		t.toggle.SetTitle(trayT("disconnect"))
	default:
		systray.SetIcon(assets.TrayOff)
		systray.SetTooltip("Починка - " + trayT("off"))
		t.toggle.SetTitle(trayT("connect"))
	}
}

func (t *tray) stop() { systray.Quit() }
