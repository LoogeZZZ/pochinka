package app

import (
	"context"
	neturl "net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/health"
	"pochinka/internal/install"
	"pochinka/internal/paths"
	"pochinka/internal/tgproxy"
	"pochinka/internal/userlists"
	"pochinka/internal/vpn"
	"pochinka/internal/winutil"
	"pochinka/internal/zapret"
)

// App — всё, что видит окно. Публичные методы вызываются из JS
// (window.go.app.App.*), изменения прилетают в окно событием "state".
type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	zapret *zapret.Manager
	vpn    *vpn.Manager
	tray   *tray
	tg     *telegram

	mu         sync.Mutex
	checks     map[string]health.Result
	checking   bool
	quitting   bool
	conflict   string    // чужой VPN, который перехватывает интернет (Happ, v2rayN…)
	autostart  bool      // есть задача автозапуска
	appUpdate  AppUpdate // новая версия Починки на GitHub
	pick       PickState // автоподбор стратегии
	pickCancel context.CancelFunc
	sdnsErr    string             // ошибка защищённого DNS
	onCancel   context.CancelFunc // отмена идущего включения обхода/VPN

	modeMu sync.Mutex // переключение режимов обход ⇄ VPN

	emitMu      sync.Mutex
	emitPending bool
}

// Version — версия программы (совпадает с productVersion в wails.json).
const Version = "1.1.1"

type UIState struct {
	Zapret    zapret.State             `json:"zapret"`
	VPN       vpn.State                `json:"vpn"`
	Checks    map[string]health.Result `json:"checks"`
	Checking  bool                     `json:"checking"`
	Admin     bool                     `json:"admin"`
	Config    config.Config            `json:"config"`
	Games     []zapret.GameGroup       `json:"games"`
	Conflict  string                   `json:"conflict"`
	TG        TGState                  `json:"tg"`
	Autostart bool                     `json:"autostart"`
	Version   string                   `json:"version"`
	Update    AppUpdate                `json:"update"`
	Pick      PickState                `json:"pick"`
	SecureDNS SecureDNSState           `json:"secureDns"`
}

func NewApp() *App {
	return &App{checks: map[string]health.Result{}}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	applog.OnLine(func(line string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "log", line)
		}
	})
	// Есть ли задача автозапуска (schtasks — ~100 мс, не держим старт).
	go func() {
		on := install.AutostartEnabled()
		a.mu.Lock()
		a.autostart = on
		a.mu.Unlock()
		a.emitState()
	}()
	// Модули поднимаются независимо: если один не смог — остальные работают.
	a.tg = &telegram{}
	if z, err := zapret.NewManager(a.onZapretChange); err != nil {
		applog.Printf("не удалось инициализировать zapret: %v", err)
	} else {
		a.zapret = z
		// Домены Cloudflare-фолбэка Telegram — в обход zapret (их подсети режут).
		z.SetExtraDomains(tgproxy.DefaultCFDomains())
		go z.AutoUpdateLoop(a.ctx)
	}
	if config.Load().TGEnabled {
		if err := a.tg.start(); err != nil {
			applog.Printf("tg: %v", err)
		}
	}
	a.vpn = vpn.NewManager(a.emitState, a.zapretDomains, func(idx int) {
		if a.zapret != nil {
			a.zapret.SetInterfaceFilter(a.ctx, idx)
		}
	})
	a.tray = startTray(a)
	go a.vpn.Loop(a.ctx)
	go a.watchConflicts()
	go a.watchAppUpdates()
	go a.ensureSecureDNS()

	if !winutil.IsAdmin() {
		applog.Printf("ВНИМАНИЕ: приложение запущено без прав администратора — zapret не сможет работать")
	}
	go a.restoreModes()
}

// restoreModes включает то, что было включено при выходе. При автозапуске
// сеть после входа в Windows часто ещё не поднялась (Wi‑Fi, VPN-адаптеры,
// DNS), поэтому неудачная попытка повторяется — примерно 2 минуты. Если
// пользователь за это время сам что-то переключил, повторы прекращаются.
func (a *App) restoreModes() {
	cfg := config.Load()
	// Включено может быть только что-то одно; если в конфиге осталось оба
	// (старая версия) — приоритет у VPN.
	want := ""
	switch {
	case cfg.VPNConnected:
		want = "vpn"
	case cfg.Connected && a.zapret != nil:
		want = "zapret"
	default:
		return
	}
	applog.Printf("восстанавливаю подключение после перезапуска")
	delays := []time.Duration{0, 3 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}
	for i, d := range delays {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(d):
		}
		c := config.Load()
		if (want == "vpn" && !c.VPNConnected) || (want == "zapret" && !c.Connected) {
			return // выключили вручную или включили другой режим
		}
		var msg string
		if want == "vpn" {
			if a.vpn.State().Status == vpn.StatusRunning {
				return
			}
			msg = a.SetVPN(true)
		} else {
			if a.zapret.State().Status == zapret.StatusRunning {
				return
			}
			msg = a.SetZapret(true)
		}
		if msg == "" {
			return
		}
		if i < len(delays)-1 {
			applog.Printf("восстановление: не вышло (%s) — повторю через %v", msg, delays[i+1])
		}
	}
}

// onZapretChange: обход и VPN взаимоисключающие, перестраивать маршруты VPN
// при смене состояния обхода больше не нужно.
func (a *App) onZapretChange() { a.emitState() }

func (a *App) zapretDomains() []string {
	if a.zapret == nil {
		return nil
	}
	return vpn.DomainRules(a.zapret.HandledDomains())
}

// beforeClose: крестик прячет окно в трей, а не закрывает приложение.
func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	q := a.quitting
	a.mu.Unlock()
	if q {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

// shutdown: что было включено, уже записано в конфиг в момент переключения
// (SetVPN/SetZapret/SetTelegram) — при следующем запуске восстановится.
// Здесь только аккуратно гасим всё в правильном порядке.
func (a *App) Shutdown(ctx context.Context) {
	// 1. Отменить всё, что идёт (подбор сервера, загрузки, проверки) —
	//    иначе выход ждал бы их десятки секунд.
	if a.cancel != nil {
		a.cancel()
	}
	// 2. VPN — первым и навсегда: убрать системный прокси/TUN; после Close
	//    никакой отложенный вызов (трей, проверка сервера) его не поднимет.
	if a.vpn != nil {
		a.vpn.Close()
	}
	if a.zapret != nil {
		a.zapret.Stop()
	}
	if a.tg != nil {
		a.tg.stop()
	}
	if a.tray != nil {
		a.tray.stop()
	}
	applog.Close()
}

// emitState шлёт состояние в окно не чаще раза в 100 мс.
func (a *App) emitState() {
	a.emitMu.Lock()
	if a.emitPending {
		a.emitMu.Unlock()
		return
	}
	a.emitPending = true
	a.emitMu.Unlock()
	time.AfterFunc(100*time.Millisecond, func() {
		a.emitMu.Lock()
		a.emitPending = false
		a.emitMu.Unlock()
		st := a.GetState()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "state", st)
		}
		if a.tray != nil {
			a.tray.update(st)
		}
	})
}

// ---- состояние ----

func (a *App) GetState() UIState {
	a.mu.Lock()
	checks := make(map[string]health.Result, len(a.checks))
	for k, v := range a.checks {
		checks[k] = v
	}
	checking, conflict, autostart, upd, pick := a.checking, a.conflict, a.autostart, a.appUpdate, a.pick
	a.mu.Unlock()
	st := UIState{
		Conflict:  conflict,
		Autostart: autostart,
		Version:   Version,
		Update:    upd,
		Pick:      pick,
		SecureDNS: a.secureDNSState(),
		Checks:    checks,
		Checking:  checking,
		Admin:     winutil.IsAdmin(),
		Config:    config.Load(),
		Games:     zapret.GameGroups,
	}
	st.Config.VPNSubURL = "" // токен подписки в окно не отдаём
	st.Config.TGSecret = ""  // секрет прокси тоже
	if a.tg != nil {
		st.TG = a.tg.state()
	}
	if a.zapret != nil {
		st.Zapret = a.zapret.State()
	}
	if a.vpn != nil {
		st.VPN = a.vpn.State()
	}
	return st
}

func (a *App) GetLog() []string { return applog.Tail() }

// ---- главная кнопка ----

// anyOn — включено ли хоть что-то (или включается).
func (a *App) anyOn() bool {
	if a.zapret != nil {
		switch a.zapret.State().Status {
		case zapret.StatusRunning, zapret.StatusStarting, zapret.StatusInstalling:
			return true
		}
	}
	if a.vpn != nil {
		switch a.vpn.State().Status {
		case vpn.StatusRunning, vpn.StatusStarting:
			return true
		}
	}
	return false
}

// Переключение режимов (обход ⇄ VPN) идёт под одной блокировкой modeMu —
// окно, трей и восстановление при запуске могут нажимать одновременно, а
// включёнными обход и VPN не должны оказаться никогда.
//
// Включение держит modeMu всё время запуска. Выключение сначала отменяет
// идущее включение (cancelOn — без блокировки, мгновенно), потом берёт
// modeMu: так «Выключить» не ждёт десятки секунд подбора сервера.

func (a *App) beginOn() (context.Context, func()) {
	a.modeMu.Lock()
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.onCancel = cancel
	a.mu.Unlock()
	return ctx, func() {
		a.mu.Lock()
		a.onCancel = nil
		a.mu.Unlock()
		cancel()
		a.modeMu.Unlock()
	}
}

func (a *App) cancelOn() {
	a.mu.Lock()
	if a.onCancel != nil {
		a.onCancel()
	}
	a.mu.Unlock()
	if a.vpn != nil {
		a.vpn.CancelStart()
	}
	if a.zapret != nil {
		a.zapret.CancelStart()
	}
}

// Toggle — главная кнопка: всё включить или всё выключить.
func (a *App) Toggle() string {
	if a.anyOn() {
		a.cancelOn()
		a.modeMu.Lock()
		defer a.modeMu.Unlock()
		a.vpnOff()
		a.zapretOff()
		return ""
	}
	// Включаем то, чем пользовались последним; если ещё ничем — VPN, когда
	// есть подписка, иначе обход.
	last := config.Load().LastMode
	hasSub := a.vpn != nil && a.vpn.State().HasSub
	if last == "" && hasSub {
		last = "vpn"
	}
	if last == "vpn" && hasSub {
		return a.SetVPN(true)
	}
	return a.SetZapret(true)
}

// SetZapret включает/выключает обход (Discord, YouTube, игры).
// Включение обхода выключает VPN: вместе они мешают друг другу.
func (a *App) SetZapret(on bool) string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	if !on {
		a.cancelOn()
		a.modeMu.Lock()
		defer a.modeMu.Unlock()
		a.zapretOff()
		return ""
	}
	ctx, done := a.beginOn()
	defer done()
	_, _ = config.Update(func(c *config.Config) { c.Connected, c.LastMode = true, "zapret" })
	a.vpnOff()
	if err := a.zapret.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return "" // выключили во время запуска
		}
		applog.Printf("ошибка запуска: %v", err)
		return err.Error()
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // даём WinDivert встать
		a.RunChecks()
	}()
	return ""
}

// SetVPN включает/выключает VPN. Включение VPN выключает обход.
func (a *App) SetVPN(on bool) string {
	if a.vpn == nil {
		return "VPN недоступен — см. журнал"
	}
	if !on {
		a.cancelOn()
		a.modeMu.Lock()
		defer a.modeMu.Unlock()
		a.vpnOff()
		return ""
	}
	ctx, done := a.beginOn()
	defer done()
	_, _ = config.Update(func(c *config.Config) { c.VPNConnected, c.LastMode = true, "vpn" })
	a.zapretOff()
	if err := a.vpn.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return ""
		}
		return err.Error()
	}
	return ""
}

// zapretOff / vpnOff — под modeMu.
func (a *App) zapretOff() {
	_, _ = config.Update(func(c *config.Config) { c.Connected = false })
	if a.zapret != nil && a.zapret.State().Status != zapret.StatusStopped {
		a.zapret.Stop()
	}
	a.mu.Lock()
	a.checks = map[string]health.Result{}
	a.mu.Unlock()
	a.emitState()
}

func (a *App) vpnOff() {
	_, _ = config.Update(func(c *config.Config) { c.VPNConnected = false })
	if a.vpn != nil && a.vpn.State().Status != vpn.StatusStopped {
		a.vpn.Stop()
	}
}

// ---- обход ----

func (a *App) SetStrategy(name string) string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	if err := a.zapret.SetStrategy(a.ctx, name); err != nil {
		return err.Error()
	}
	if a.zapret.State().Status == zapret.StatusRunning {
		go a.RunChecks()
	}
	return ""
}

func (a *App) SetGameFilter(mode string) string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	if err := a.zapret.SetGameFilter(a.ctx, mode); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) SetGameEnabled(id string, on bool) string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	if err := a.zapret.SetGameEnabled(a.ctx, id, on); err != nil {
		return err.Error()
	}
	return ""
}

// SetLanguage: ru | en. Журнал и сообщения бэкенда остаются на русском.
func (a *App) SetLanguage(lang string) {
	if lang != "ru" && lang != "en" {
		return
	}
	_, _ = config.Update(func(c *config.Config) { c.Lang = lang })
	if a.tray != nil {
		a.tray.relabel()
	}
	a.emitState()
}

func (a *App) SetAutoUpdate(on bool) {
	_, _ = config.Update(func(c *config.Config) { c.AutoUpdate = on })
	a.emitState()
}

// CheckUpdates — ручная проверка обновлений zapret.
func (a *App) CheckUpdates() string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	ok, err := a.zapret.CheckUpdate(a.ctx)
	switch {
	case err != nil:
		applog.Printf("обновление: %v", err)
		return "Ошибка"
	case ok:
		return "Обновлено до " + a.zapret.State().Version
	}
	return "Последняя версия"
}

// RunChecks проверяет доступность Discord и YouTube.
func (a *App) RunChecks() {
	if a.zapret == nil {
		return
	}
	a.mu.Lock()
	if a.checking {
		a.mu.Unlock()
		return
	}
	a.checking = true
	a.mu.Unlock()
	a.emitState()

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	res := health.CheckAll(ctx)
	cancel()

	// Обход выключили, пока шла проверка — результат уже не про него.
	if a.zapret.State().Status != zapret.StatusRunning {
		res = map[string]health.Result{}
	}
	a.mu.Lock()
	a.checks, a.checking = res, false
	a.mu.Unlock()
	for k, r := range res {
		if r.OK {
			applog.Printf("проверка %s: открывается (%d мс)", k, r.Latency)
		} else {
			applog.Printf("проверка %s: НЕ открывается — %s", k, r.Detail)
		}
	}
	a.emitState()
}

// ---- VPN ----

func (a *App) AddSubscription(url string) string {
	if err := a.vpn.AddSubscription(a.ctx, url); err != nil {
		return err.Error()
	}
	return ""
}

// RefreshSubscription обновляет активную подписку.
func (a *App) RefreshSubscription() string {
	if err := a.vpn.RefreshSubscription(a.ctx, ""); err != nil {
		return err.Error()
	}
	return ""
}

// SetSendHWID — отправлять ли HWID устройства панели. Подписку сразу
// перечитываем: после включения панель с лимитом устройств её отдаст.
func (a *App) SetSendHWID(on bool) string {
	_, _ = config.Update(func(c *config.Config) { c.VPNNoHWID = !on })
	a.emitState()
	if !a.vpn.State().HasSub {
		return ""
	}
	return a.RefreshSubscription()
}

func (a *App) SetActiveSub(id string) string {
	if err := a.vpn.SetActiveSub(a.ctx, id); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) RemoveSubscription(id string) { a.vpn.RemoveSubscription(a.ctx, id) }

func (a *App) SelectServer(id string) string {
	if err := a.vpn.Select(a.ctx, id); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) SetVPNMode(mode string) string {
	if err := a.vpn.SetMode(a.ctx, vpn.Mode(mode)); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) PingServers() { a.vpn.Ping(a.ctx) }

// ---- свои списки (редактор в окне) ----

// ListResult — ответ окну при сохранении списка.
type ListResult struct {
	Error string `json:"error"`
	Count int    `json:"count"` // сколько записей (без комментариев)
}

// GetUserList — текст списка: zapret-include, zapret-exclude,
// zapret-ipexclude, vpn-sites.
func (a *App) GetUserList(id string) string {
	l, ok := userlists.Get(id)
	if !ok {
		return ""
	}
	text, err := l.Read()
	if err != nil {
		applog.Printf("список %s: %v", id, err)
	}
	return text
}

// SaveUserList проверяет и записывает список, затем применяет его: обход или
// VPN перечитывают списки, если включены.
func (a *App) SaveUserList(id, text string) ListResult {
	l, ok := userlists.Get(id)
	if !ok {
		return ListResult{Error: "неизвестный список"}
	}
	n, err := l.Save(text)
	if err != nil {
		return ListResult{Error: err.Error()}
	}
	applog.Printf("список %s сохранён: %d записей", id, n)
	go func() {
		if strings.HasPrefix(id, "vpn-") {
			a.vpn.RestartIfRunning(a.ctx)
			return
		}
		if a.zapret != nil {
			if err := a.zapret.ApplyLists(a.ctx); err != nil {
				applog.Printf("обход: не удалось применить списки: %v", err)
			}
		}
		// Домены обхода идут в VPN напрямую — VPN тоже перечитывает.
		a.vpn.RestartIfRunning(a.ctx)
	}()
	return ListResult{Count: n}
}

// ---- прочее ----

// OpenLogsFolder — папка журналов (единственная, которую можно открыть с
// правами пользователя: остальная папка данных — только для администраторов).
func (a *App) OpenLogsFolder() {
	_ = exec.Command("explorer.exe", paths.LogsDir()).Start()
}

// OpenURL — ссылки из подписки (поддержка, личный кабинет).
func (a *App) OpenURL(u string) {
	pu, err := neturl.Parse(strings.TrimSpace(u))
	if err != nil || len(u) > 1024 {
		return
	}
	switch pu.Scheme {
	case "https", "http", "tg":
		openAsUser(pu.String())
	}
}

// openAsUser открывает ссылку через explorer.exe: он передаёт её уже
// запущенной оболочке пользователя, и браузер/Telegram стартуют с обычными
// правами, а не с правами администратора, как Починка.
//
// Саму ссылку explorer.exe в аргументе понимает только для http(s):
// «tg://proxy?…» он принимает за путь и открывает «Документы». Поэтому
// ссылка кладётся в ярлык .url — его оболочка открывает обработчиком схемы.
func openAsUser(link string) {
	if strings.ContainsAny(link, "\r\n") {
		return
	}
	f, err := os.CreateTemp("", "pochinka-*.url")
	if err != nil {
		applog.Printf("не удалось открыть ссылку: %v", err)
		return
	}
	_, err = f.WriteString("[InternetShortcut]\r\nURL=" + link + "\r\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		applog.Printf("не удалось открыть ссылку: %v", err)
		return
	}
	_ = exec.Command("explorer.exe", f.Name()).Start()
	// В ярлыке секрет прокси — убираем, когда оболочка его уже прочитала.
	time.AfterFunc(30*time.Second, func() { os.Remove(f.Name()) })
}

// Quit — полный выход (из трея или из окна).
func (a *App) Quit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	runtime.Quit(a.ctx)
}

func (a *App) ShowWindow() {
	// Второй запуск или клик в трее до окончания старта — окна ещё нет.
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}

// watchConflicts раз в 5 секунд проверяет, не перехватывает ли интернет
// другой VPN: два VPN одновременно дерутся за трафик, и ничего не грузится.
func (a *App) watchConflicts() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		name := vpn.ForeignVPN()
		a.mu.Lock()
		changed := name != a.conflict
		a.conflict = name
		a.mu.Unlock()
		// Счётчики Telegram-прокси меняются постоянно — обновляем окно.
		if changed || (a.tg != nil && a.tg.state().Running) {
			if changed && name != "" {
				applog.Printf("ВНИМАНИЕ: работает другой VPN (%s) — он перехватывает трафик, выключите его", name)
			}
			a.emitState()
		}
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CheckVPN — повторить самопроверку VPN.
func (a *App) CheckVPN() { a.vpn.SelfCheck(a.ctx) }

// SetVPNRoute: "all" — всё, кроме российского; иначе только заблокированное.
func (a *App) SetVPNRoute(route string) string {
	if err := a.vpn.SetRouteAll(a.ctx, route == "all"); err != nil {
		return err.Error()
	}
	return ""
}
