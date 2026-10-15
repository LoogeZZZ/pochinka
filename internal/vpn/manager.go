package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/paths"
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusError    Status = "error"
)

const AutoServer = "auto"

type ServerView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Ping     int    `json:"ping"` // мс; -1 нет ответа; 0 не мерили
	UDP      bool   `json:"udp"`
}

type State struct {
	Status Status `json:"status"`
	Error  string `json:"error"`
	Busy   string `json:"busy"`
	Mode   Mode   `json:"mode"`

	Subs      []SubView `json:"subs"`
	ActiveSub string    `json:"activeSub"`
	// Ниже — про активную подписку.
	HasSub   bool         `json:"hasSub"`
	SubHost  string       `json:"subHost"` // только домен, без токена
	Info     SubInfo      `json:"info"`
	Servers  []ServerView `json:"servers"`
	Selected string       `json:"selected"`

	Active  string `json:"active"` // какой сервер реально используется
	Check   string `json:"check"`  // результат самопроверки
	CheckOK bool   `json:"checkOk"`
}

// cachedServer — сервер на диске (вместе с outbound).
type cachedServer struct {
	Server
	Outbound map[string]any   `json:"outbound"`
	Extra    []map[string]any `json:"extra"`
}

// cache — формат старой одиночной подписки (vpn-cache.json), для переноса.
type cache struct {
	Info    SubInfo        `json:"info"`
	Servers []cachedServer `json:"servers"`
}

type Manager struct {
	eng      engine
	onChange func()
	// zapretDomains — домены, которые сейчас чинит zapret (пусто, если он выключен).
	zapretDomains func() []string
	// onTun сообщает zapret индекс физического интерфейса (0 — TUN выключен).
	onTun func(ifIndex int)

	op sync.Mutex

	mu       sync.Mutex
	state    State
	subs     []*subscription
	active   string // ID активной подписки
	want     bool
	port     int  // порт HTTP-прокси, который мы выставили в системе
	tunUp    bool // поднят TUN (а не прокси) — режим из конфига мог уже смениться
	closed   bool // приложение закрывается — больше не стартуем
	tunIfIdx int  // сетевая карта, к которой привязан TUN

	startCancel context.CancelFunc // отмена идущего запуска
}

func NewManager(onChange func(), zapretDomains func() []string, onTun func(int)) *Manager {
	m := &Manager{onChange: onChange, zapretDomains: zapretDomains, onTun: onTun}
	mainEngine = &m.eng
	SetAssetDir(assetDir())
	cfg := ensurePorts()
	// Если в прошлый раз приложение упало с включённым прокси — убираем его.
	ClearSystemProxy(cfg.VPNHTTPPort)
	m.state = State{Status: StatusStopped, Mode: modeOf(cfg)}
	m.subs = loadSubs()
	m.active = config.Load().VPNActiveSub
	if m.subByID(m.active) == nil && len(m.subs) > 0 {
		m.active = m.subs[0].ID
	}
	return m
}

func assetDir() string { return filepath.Join(paths.DataDir(), "geo") }

// xrayLog — журнал ядра; переменная, чтобы тесты без прав админа писали в temp.
var xrayLog = func() string { return filepath.Join(paths.LogsDir(), "xray.log") }

// UserSitesFile — свои сайты, которые всегда идут через VPN.
func UserSitesFile() string { return filepath.Join(paths.UserDir(), "vpn-sites.txt") }

func modeOf(c config.Config) Mode {
	if c.VPNMode == string(ModeTUN) {
		return ModeTUN
	}
	return ModeProxy
}

// ensurePorts выбирает случайные порты один раз на установку: стандартные
// 10808/10809 некоторые программы проверяют как признак VPN.
func ensurePorts() config.Config {
	c := config.Load()
	if c.VPNHTTPPort == 0 {
		c, _ = config.Update(func(c *config.Config) {
			c.VPNHTTPPort = 20000 + rand.IntN(20000)
			c.VPNSocksPort = c.VPNHTTPPort + 1
		})
	}
	return c
}

func subHost(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return "ссылки на серверы"
}

// subByID и cur вызываются под m.mu.
func (m *Manager) subByID(id string) *subscription {
	for _, s := range m.subs {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *Manager) cur() *subscription { return m.subByID(m.active) }

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.state
	s.ActiveSub = m.active
	s.Subs = make([]SubView, len(m.subs))
	for i, sub := range m.subs {
		s.Subs[i] = SubView{ID: sub.ID, Title: sub.title(), Host: subHost(sub.URL), Info: sub.Info,
			Servers: len(sub.servers), Active: sub.ID == m.active}
	}
	c := m.cur()
	s.HasSub = c != nil
	s.Servers = nil
	s.Selected = AutoServer
	if c != nil {
		s.SubHost, s.Info, s.Selected = subHost(c.URL), c.Info, c.Selected
		s.Servers = make([]ServerView, len(c.servers))
		for i, sv := range c.servers {
			s.Servers[i] = ServerView{ID: sv.ID, Name: sv.Name, Protocol: sv.Protocol, Ping: c.Pings[sv.ID], UDP: sv.UDP}
		}
	}
	return s
}

func (m *Manager) set(fn func(s *State)) {
	m.mu.Lock()
	fn(&m.state)
	m.mu.Unlock()
	if m.onChange != nil {
		m.onChange()
	}
}

// ---- подписки ----

// AddSubscription добавляет подписку (или обновляет уже добавленную с той же
// ссылкой) и делает её активной.
func (m *Manager) AddSubscription(ctx context.Context, url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("вставьте ссылку на подписку")
	}
	m.set(func(s *State) { s.Busy = "Загружаю подписку…" })
	defer m.set(func(s *State) { s.Busy = "" })
	servers, info, err := FetchSubscription(ctx, url)
	if err != nil {
		return err
	}
	logRejected()

	m.mu.Lock()
	var sub *subscription
	for _, s := range m.subs {
		if s.URL == url {
			sub = s
		}
	}
	if sub == nil {
		sub = &subscription{ID: newSubID(), URL: url, Added: time.Now().Unix(), Selected: AutoServer, Pings: map[string]int{}}
		m.subs = append(m.subs, sub)
	}
	sub.Info = info
	sub.setServers(servers)
	if sub.Selected != AutoServer && sub.find(sub.Selected) == nil {
		sub.Selected = AutoServer
	}
	m.active = sub.ID
	err = sub.save()
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("не удалось сохранить подписку: %w", err)
	}
	_, _ = config.Update(func(c *config.Config) { c.VPNActiveSub = sub.ID })
	applog.Printf("vpn: подписка «%s» добавлена — %d серверов", sub.title(), len(servers))
	m.onChange()
	go m.Ping(context.Background())
	return m.restartIfWanted(ctx)
}

// RefreshSubscription перезагружает подписку id (пусто — активную).
func (m *Manager) RefreshSubscription(ctx context.Context, id string) error {
	m.mu.Lock()
	if id == "" {
		id = m.active
	}
	sub := m.subByID(id)
	m.mu.Unlock()
	if sub == nil {
		return errors.New("подписка не добавлена")
	}
	m.set(func(s *State) { s.Busy = "Обновляю подписку…" })
	defer m.set(func(s *State) { s.Busy = "" })
	servers, info, err := FetchSubscription(ctx, sub.URL)
	if err != nil {
		return err
	}
	logRejected()

	m.mu.Lock()
	old := serverIDs(sub.servers)
	sub.Info = info
	sub.setServers(servers)
	if sub.Selected != AutoServer && sub.find(sub.Selected) == nil {
		sub.Selected = AutoServer
	}
	_ = sub.save()
	isActive := sub.ID == m.active
	m.mu.Unlock()
	m.onChange()
	applog.Printf("vpn: подписка «%s» обновлена — %d серверов", sub.title(), len(servers))
	// Перезапуск только если у АКТИВНОЙ подписки поменялся сам набор
	// серверов. Ротация хостов у провайдера (Dream VPN отдаёт на каждый
	// запрос другой хост) соединение не рвёт — подхватится при следующем
	// подключении.
	if isActive && old != serverIDs(servers) {
		go m.Ping(context.Background())
		return m.restartIfWanted(ctx)
	}
	return nil
}

// SetActiveSub делает подписку активной и переподключается на неё.
func (m *Manager) SetActiveSub(ctx context.Context, id string) error {
	m.mu.Lock()
	if m.subByID(id) == nil {
		m.mu.Unlock()
		return errors.New("подписка не найдена")
	}
	changed := m.active != id
	m.active = id
	m.mu.Unlock()
	_, _ = config.Update(func(c *config.Config) { c.VPNActiveSub = id })
	m.onChange()
	if !changed {
		return nil
	}
	go m.Ping(context.Background())
	return m.restartIfWanted(ctx)
}

// RemoveSubscription удаляет подписку. Если она была активной — VPN
// переключается на следующую (или выключается, если подписок не осталось).
func (m *Manager) RemoveSubscription(ctx context.Context, id string) {
	m.mu.Lock()
	sub := m.subByID(id)
	if sub == nil {
		m.mu.Unlock()
		return
	}
	sub.removed = true // под mu: все save() идут под mu, после этого файл не появится
	wasActive := id == m.active
	var rest []*subscription
	for _, s := range m.subs {
		if s.ID != id {
			rest = append(rest, s)
		}
	}
	m.subs = rest
	if wasActive {
		m.active = ""
		if len(rest) > 0 {
			m.active = rest[0].ID
		}
	}
	active := m.active
	m.mu.Unlock()
	sub.remove()
	_, _ = config.Update(func(c *config.Config) { c.VPNActiveSub = active })
	applog.Printf("vpn: подписка «%s» удалена", sub.title())
	switch {
	case !wasActive:
		m.onChange()
	case active == "":
		_, _ = config.Update(func(c *config.Config) { c.VPNConnected = false })
		m.Stop()
	default:
		m.onChange()
		_ = m.restartIfWanted(ctx)
	}
}

func serverIDs(list []*Server) string {
	ids := make([]string, len(list))
	for i, s := range list {
		ids[i] = s.ID
	}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}

func cacheOf(list []*Server) []cachedServer {
	var out []cachedServer
	for _, s := range list {
		out = append(out, cachedServer{Server: *s, Outbound: s.Outbound, Extra: s.Extra})
	}
	return out
}

// Select выбирает сервер активной подписки (или AutoServer) и переподключается.
func (m *Manager) Select(ctx context.Context, id string) error {
	m.mu.Lock()
	c := m.cur()
	if c == nil || (id != AutoServer && c.find(id) == nil) {
		m.mu.Unlock()
		return errors.New("сервер не найден")
	}
	c.Selected = id
	_ = c.save()
	m.mu.Unlock()
	m.onChange()
	return m.restartIfWanted(ctx)
}

func (m *Manager) SetMode(ctx context.Context, mode Mode) error {
	_, _ = config.Update(func(c *config.Config) { c.VPNMode = string(mode) })
	m.set(func(s *State) { s.Mode = mode })
	return m.restartIfWanted(ctx)
}

// rankServers возвращает живые серверы подписки, самый быстрый первым.
// Свежий замер (младше 5 минут) берём из памяти, иначе меряем заново.
func (m *Manager) rankServers(ctx context.Context, sub *subscription) []*Server {
	m.mu.Lock()
	servers := append([]*Server(nil), sub.servers...)
	pings := maps.Clone(sub.Pings) // копия: storePings пишет в оригинал из других горутин
	fresh := time.Since(time.Unix(sub.PingedAt, 0)) < 5*time.Minute && len(pings) > 0
	m.mu.Unlock()
	if !fresh {
		m.set(func(s *State) { s.Busy = "Выбираю самый быстрый сервер…" })
		pings = pingAll(ctx, servers, m.probeIface())
		m.storePings(sub, pings)
		m.set(func(s *State) { s.Busy = "" })
	}
	var alive []*Server
	for _, s := range servers {
		if pings[s.ID] > 0 {
			alive = append(alive, s)
		}
	}
	sort.SliceStable(alive, func(i, j int) bool { return pings[alive[i].ID] < pings[alive[j].ID] })
	return alive
}

func (m *Manager) storePings(sub *subscription, pings map[string]int) {
	m.mu.Lock()
	if sub.Pings == nil {
		sub.Pings = map[string]int{}
	}
	for k, v := range pings {
		sub.Pings[k] = v
	}
	sub.PingedAt = time.Now().Unix()
	_ = sub.save()
	m.mu.Unlock()
	m.onChange()
}

// verifySelected: вручную выбранный сервер не пропускает трафик —
// переключаемся на «Авто», чтобы у пользователя не было «подключено, но
// ничего не грузит».
func (m *Manager) verifySelected(sub *subscription, s *Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	res := pingAll(ctx, []*Server{s}, m.probeIface())
	m.storePings(sub, res)
	m.mu.Lock()
	still := m.active == sub.ID && sub.Selected == s.ID && m.state.Status == StatusRunning
	m.mu.Unlock()
	if res[s.ID] > 0 || !still {
		return
	}
	applog.Printf("vpn: сервер «%s» не пропускает трафик — переключаюсь на «Авто»", s.Name)
	_ = m.Select(context.Background(), AutoServer)
}

// Ping замеряет серверы активной подписки.
func (m *Manager) Ping(ctx context.Context) {
	m.mu.Lock()
	sub := m.cur()
	var list []*Server
	if sub != nil {
		list = append(list, sub.servers...)
	}
	m.mu.Unlock()
	if sub == nil {
		return
	}
	m.storePings(sub, pingAll(ctx, list, m.probeIface()))
}

// ---- запуск ----

func (m *Manager) wanted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.want
}

// restartIfWanted — применить новые настройки, если VPN включён. Проверка
// «включён» — под той же блокировкой, что и запуск: иначе нажатие
// «Выключить» могло проскочить между проверкой и стартом, и VPN поднимался
// бы сам.
func (m *Manager) restartIfWanted(ctx context.Context) error { return m.start(ctx, ifWanted) }

// Start включает VPN (повторный вызов — перезапуск с новыми настройками).
func (m *Manager) Start(ctx context.Context) error { return m.start(ctx, always) }

type startPolicy int

const (
	always    startPolicy = iota
	ifWanted              // только если пользователь держит VPN включённым
	ifRunning             // только если VPN реально работает
)

var errClosed = errors.New("приложение закрывается")

// Close — при выходе из приложения: выключить и больше не включаться.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.Stop()
}

// start: onlyIfRunning — перезапуск только уже работающего VPN; проверка
// внутри блокировки, чтобы «Выключить» не проскочило между проверкой и стартом.
func (m *Manager) start(parent context.Context, policy startPolicy) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	switch {
	case m.closed:
		m.mu.Unlock()
		return errClosed
	case parent.Err() != nil:
		m.mu.Unlock()
		return parent.Err() // включение уже отменили — даже не помечаем «хочу VPN»
	case policy == ifWanted && !m.want,
		policy == ifRunning && (!m.want || m.state.Status != StatusRunning):
		m.mu.Unlock()
		return nil
	}
	m.want = true
	// Свой контекст на каждый запуск: Stop() отменяет его, не дожидаясь,
	// пока закончится подбор сервера.
	ctx, cancel := context.WithCancel(parent)
	m.startCancel = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.startCancel = nil
		m.mu.Unlock()
	}()
	err := m.startLocked(ctx)
	if ctx.Err() != nil {
		// Запуск отменили (Выключить/выход) — убираем недоподнятое молча:
		// состояние выставит Stop.
		m.stopLocked(false)
		return ctx.Err()
	}
	if err != nil {
		// Не запустились — не держим «хочу VPN», иначе любое событие (смена
		// стратегии zapret, обновление базы) будет снова пытаться стартовать.
		m.mu.Lock()
		m.want = false
		m.mu.Unlock()
		m.stopLocked(false) // убрать недоподнятый прокси/TUN
		// TUN не поднялся — zapret должен снова видеть все интерфейсы.
		if m.onTun != nil {
			m.onTun(0)
		}
		applog.Printf("vpn: %v", err)
		m.set(func(s *State) { s.Status, s.Error, s.Busy = StatusError, err.Error(), "" })
	}
	return err
}

func (m *Manager) startLocked(ctx context.Context) error {
	cfg := config.Load()
	m.mu.Lock()
	sub := m.cur()
	var servers []*Server
	selected := AutoServer
	if sub != nil {
		servers, selected = append(servers, sub.servers...), sub.Selected
	}
	m.mu.Unlock()
	if len(servers) == 0 {
		return errors.New("добавьте подписку VPN")
	}
	m.set(func(s *State) { s.Status, s.Error = StatusStarting, "" })

	// Базы берём с диска или из вшитой копии — сеть для старта не нужна,
	// свежие подтянет фоновое обновление.
	if err := EnsureGeo(assetDir()); err != nil {
		return fmt.Errorf("база блокировок: %w", err)
	}

	auto := selected == AutoServer || len(servers) == 1
	use := servers
	active := "Авто"
	if !auto {
		m.mu.Lock()
		s := sub.find(selected)
		m.mu.Unlock()
		if s != nil {
			use, active = []*Server{s}, s.Name
		}
	} else if len(servers) == 1 {
		active = servers[0].Name
	} else {
		// «Авто»: сначала замеряем, мёртвые выкидываем, самый быстрый —
		// первым (он же запасной у балансировщика, пока тот не сделал свой
		// первый замер).
		alive := m.rankServers(ctx, sub)
		if len(alive) == 0 {
			return errors.New("ни один сервер подписки не отвечает — обновите подписку или проверьте интернет")
		}
		use, active = alive, "Авто · "+alive[0].Name
	}

	mode := modeOf(cfg)
	m.stopLocked(mode == ModeTUN) // при перезапуске в TUN не дёргаем zapret дважды

	// Порты прокси могли занять (другая программа, резерв Hyper-V/WSL) —
	// тогда берём свободные от системы и запоминаем.
	if !portFree(cfg.VPNHTTPPort) || !portFree(cfg.VPNSocksPort) {
		ps, err := freePorts(2)
		if err != nil {
			return fmt.Errorf("нет свободных портов: %w", err)
		}
		applog.Printf("vpn: порты %d/%d заняты — беру %d/%d", cfg.VPNHTTPPort, cfg.VPNSocksPort, ps[0], ps[1])
		cfg.VPNHTTPPort, cfg.VPNSocksPort = ps[0], ps[1]
		if _, err := config.Update(func(c *config.Config) { c.VPNHTTPPort, c.VPNSocksPort = ps[0], ps[1] }); err != nil {
			applog.Printf("vpn: не удалось сохранить порты: %v", err)
		}
	}

	opts := BuildOptions{
		Servers: use, Auto: auto, Mode: mode,
		HTTPPort: cfg.VPNHTTPPort, SocksPort: cfg.VPNSocksPort,
		LogFile: xrayLog(), ZapretDomains: m.zapretDomains(), UserDomains: readUserSites(),
		RouteAll: cfg.VPNRouteAll,
	}
	var physIdx int
	if mode == ModeTUN {
		phys, err := PhysicalInterface()
		if err != nil {
			return err
		}
		opts.PhysIface, physIdx = phys.Name, phys.Index
		m.mu.Lock()
		m.tunIfIdx = phys.Index
		m.mu.Unlock()
		if err := ensureWintun(); err != nil {
			return fmt.Errorf("wintun.dll: %w", err)
		}
	}
	js, err := BuildConfig(opts)
	if err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(paths.LogsDir(), "xray-config.json"), redactConfig(js), 0o600)
	if err := m.eng.start(js); err != nil {
		return err
	}
	// Пока поднимали ядро, могли нажать «Выключить» или закрыть приложение —
	// тогда системный прокси и маршруты не трогаем.
	if ctx.Err() != nil {
		m.eng.stop()
		return ctx.Err()
	}

	if mode == ModeTUN {
		if err := setupTunRoutes(); err != nil {
			m.eng.stop()
			return err
		}
		m.mu.Lock()
		m.tunUp = true
		m.mu.Unlock()
		if m.onTun != nil {
			m.onTun(physIdx)
		}
	} else {
		// port — до записи: если запись упадёт посередине, stopLocked всё
		// равно уберёт то, что успело записаться.
		m.mu.Lock()
		m.port = cfg.VPNHTTPPort
		m.mu.Unlock()
		if err := SetSystemProxy(cfg.VPNHTTPPort); err != nil {
			m.eng.stop()
			return fmt.Errorf("системный прокси: %w", err)
		}
	}
	applog.Printf("vpn: подключено (%s, %s)", map[Mode]string{ModeProxy: "системный прокси", ModeTUN: "TUN"}[mode], active)
	m.set(func(s *State) { s.Status, s.Active, s.Error, s.Check, s.CheckOK = StatusRunning, active, "", "", false })
	go func() {
		time.Sleep(1500 * time.Millisecond) // дать системе подхватить прокси/маршруты
		m.SelfCheck(context.Background())
	}()
	if !auto && len(servers) > 1 {
		go m.verifySelected(sub, use[0])
	}
	return nil
}

// Stop выключает VPN и возвращает системные настройки как были.
func (m *Manager) Stop() {
	// Сначала отменяем идущий запуск — он держит op, и без отмены «Выключить»
	// ждало бы окончания подбора сервера (десятки секунд).
	m.mu.Lock()
	m.want = false
	if m.startCancel != nil {
		m.startCancel()
	}
	m.mu.Unlock()
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	m.want = false
	m.mu.Unlock()
	m.stopLocked(false)
	m.set(func(s *State) { s.Status, s.Error, s.Active, s.Check = StatusStopped, "", "", "" })
}

func (m *Manager) stopLocked(stayTun bool) {
	m.mu.Lock()
	port := m.port
	m.port = 0
	m.mu.Unlock()
	if port != 0 {
		ClearSystemProxy(port)
	}
	m.mu.Lock()
	wasTun := m.tunUp
	m.tunUp = false
	m.mu.Unlock()
	m.eng.stop()
	if wasTun {
		// Адаптер удаляется асинхронно; новый старт до этого получил бы
		// «initialization already completed».
		waitTunGone(3 * time.Second)
	}
	if wasTun && !stayTun && m.onTun != nil {
		m.onTun(0)
	}
}

// Running — VPN поднят.
func (m *Manager) Running() bool { return m.State().Status == StatusRunning }

// Loop — фоновое обслуживание: обновление подписки и баз блокировок.
func (m *Manager) Loop(ctx context.Context) {
	go m.watchNetwork(ctx)
	t := time.NewTimer(20 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		m.mu.Lock()
		subs := append([]*subscription(nil), m.subs...)
		m.mu.Unlock()
		if len(subs) > 0 {
			// Обновляем все подписки: у неактивных тоже должны быть свежие
			// трафик/срок/серверы, чтобы переключение было мгновенным.
			for _, sub := range subs {
				m.mu.Lock()
				info := sub.Info
				m.mu.Unlock()
				every := time.Duration(max(info.UpdateHrs, 1)) * time.Hour
				if every > 12*time.Hour {
					every = 12 * time.Hour
				}
				if time.Since(time.Unix(info.FetchedAt, 0)) > every {
					if err := m.RefreshSubscription(ctx, sub.ID); err != nil {
						applog.Printf("vpn: обновление подписки «%s»: %v", sub.title(), err)
					}
				}
			}
			if changed, err := UpdateGeo(ctx, assetDir(), false); err != nil {
				applog.Printf("vpn: %v", err)
			} else if changed {
				applog.Printf("vpn: базы блокировок обновлены")
				m.RestartIfRunning(ctx)
			}
		}
		t.Reset(30 * time.Minute)
	}
}

// RestartIfRunning — применить изменившиеся внешние условия (например,
// включили/выключили zapret, поменялся список своих сайтов).
// Трогает только реально работающий VPN: выключенный или упавший с ошибкой
// не поднимается сам по себе.
func (m *Manager) RestartIfRunning(ctx context.Context) {
	_ = m.start(ctx, ifRunning)
}

// EnsureUserSites создаёт файл своих сайтов с подсказкой, если его нет.
func EnsureUserSites() {
	if _, err := os.Stat(UserSitesFile()); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(UserSitesFile(), []byte("# Сайты, которые всегда открываются через VPN — по одному в строке.\r\n# Например: example.com\r\n"), 0o644)
	}
}

func readUserSites() []string {
	EnsureUserSites()
	b, err := os.ReadFile(UserSitesFile())
	if err != nil {
		return nil
	}
	return DomainRules(string(b))
}

// DomainRules превращает список доменов (формат zapret: "^x" — без поддоменов)
// в правила xray.
func DomainRules(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, value := "domain:", strings.ToLower(line)
		if rest, ok := strings.CutPrefix(value, "^"); ok {
			kind, value = "full:", rest
		}
		value = strings.Trim(value, ".")
		// Только похожее на домен: буквы (в т.ч. кириллица), цифры, точка,
		// дефис, подчёркивание. Пустое ("^") или мусор xray отверг бы, и VPN
		// не запустился бы из-за одной кривой строки.
		if !looksLikeDomain(value) {
			continue
		}
		rule := kind + value
		if seen[rule] {
			continue
		}
		seen[rule] = true
		out = append(out, rule)
	}
	sort.Strings(out)
	return out
}

// redactConfig — копия конфига для диагностики без ключей и паролей.
func redactConfig(js []byte) []byte {
	var v any
	if json.Unmarshal(js, &v) != nil {
		return nil
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				switch k {
				case "id", "password", "auth", "publicKey", "shortId", "mldsa65Verify":
					t[k] = "***"
				default:
					walk(val)
				}
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

func logRejected() {
	for _, r := range lastRejected() {
		applog.Printf("vpn: сервер пропущен — ядро его не принимает: %s", r)
	}
}

func cfgHTTPPort() int { return config.Load().VPNHTTPPort }

// SetRouteAll: true — через VPN всё, кроме российского; false — только
// заблокированное.
func (m *Manager) SetRouteAll(ctx context.Context, all bool) error {
	_, _ = config.Update(func(c *config.Config) { c.VPNRouteAll = all })
	m.onChange()
	return m.restartIfWanted(ctx)
}

func looksLikeDomain(s string) bool {
	if s == "" || len(s) > 253 || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// probeIface — в режиме TUN замеры должны идти мимо нашего же туннеля
// (через сетевую карту), иначе мёртвый текущий сервер «убивал» бы все замеры.
func (m *Manager) probeIface() string {
	m.mu.Lock()
	up := m.tunUp
	m.mu.Unlock()
	if !up {
		return ""
	}
	if ifc, err := PhysicalInterface(); err == nil {
		return ifc.Name
	}
	return ""
}

// CancelStart прерывает идущий запуск (подбор сервера и т.п.), не дожидаясь его.
func (m *Manager) CancelStart() {
	m.mu.Lock()
	if m.startCancel != nil {
		m.startCancel()
	}
	m.mu.Unlock()
}

// watchNetwork: в режиме TUN все соединения привязаны к конкретной сетевой
// карте. Сменилась карта (Wi‑Fi ⇄ Ethernet, переподключение) — без
// перезапуска весь интернет встал бы. Проверяем раз в 5 секунд.
func (m *Manager) watchNetwork(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		m.mu.Lock()
		up, idx := m.tunUp, m.tunIfIdx
		m.mu.Unlock()
		if !up {
			continue
		}
		phys, err := PhysicalInterface()
		if err != nil || phys.Index == idx {
			continue // нет сети вовсе — ждём, перезапуск ничего не даст
		}
		applog.Printf("vpn: сменилась сетевая карта (%s) — переподключаю TUN", phys.Name)
		m.RestartIfRunning(ctx)
	}
}
