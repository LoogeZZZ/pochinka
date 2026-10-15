package zapret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/paths"
	"pochinka/internal/winutil"
)

type Status string

const (
	StatusStopped    Status = "stopped"
	StatusInstalling Status = "installing"
	StatusStarting   Status = "starting"
	StatusRunning    Status = "running"
	StatusError      Status = "error"
)

type State struct {
	Status        Status   `json:"status"`
	Error         string   `json:"error"`
	Version       string   `json:"version"`
	LatestVersion string   `json:"latestVersion"`
	Strategy      string   `json:"strategy"`
	Strategies    []string `json:"strategies"`
	GameFilter    string   `json:"gameFilter"`
	Busy          string   `json:"busy"` // что делаем прямо сейчас, для UI
}

type Manager struct {
	onChange func()
	run      *runner

	op sync.Mutex // сериализует Start/Stop/Update — они не должны идти параллельно

	mu    sync.Mutex
	state State
	want  bool // пользователь хочет, чтобы обход был включён
	extra []string
	// iface — индекс физического интерфейса, если включён VPN в режиме TUN:
	// winws тогда перехватывает только его (--wf-iface), иначе он бы
	// портил пакеты ещё и внутри виртуального адаптера.
	iface int

	startCancel context.CancelFunc // отмена идущего запуска
	updateMu    sync.Mutex         // одна проверка обновлений за раз
}

func NewManager(onChange func()) (*Manager, error) {
	m := &Manager{onChange: onChange}
	r, err := newRunner(m.onRunnerState)
	if err != nil {
		return nil, err
	}
	m.run = r
	cfg := config.Load()
	m.state = State{Status: StatusStopped, Strategy: cfg.Strategy, GameFilter: cfg.GameFilter}
	if v := m.pickInstalledVersion(cfg.ZapretVersion); v != "" {
		m.state.Version = v
		m.refreshStrategiesLocked()
	}
	return m, nil
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.state
	s.Strategies = append([]string(nil), s.Strategies...)
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

func (m *Manager) onRunnerState(running bool, err error) {
	m.set(func(s *State) {
		switch {
		case running:
			s.Status, s.Error = StatusRunning, ""
		case err != nil:
			s.Status, s.Error = StatusError, err.Error()
		case s.Status != StatusStarting:
			s.Status = StatusStopped
		}
	})
}

// SetExtraDomains — домены, которые приложение само добавляет в обход
// (например, домены Cloudflare-фолбэка Telegram-прокси).
func (m *Manager) SetExtraDomains(d []string) {
	m.mu.Lock()
	m.extra = append([]string(nil), d...)
	m.mu.Unlock()
}

func (m *Manager) versionDir(v string) string { return filepath.Join(paths.ZapretVersionsDir(), v) }

// pickInstalledVersion: версия из конфига, если она на месте, иначе самая свежая из имеющихся.
func (m *Manager) pickInstalledVersion(preferred string) string {
	if ValidVersion(preferred) && IsInstalled(m.versionDir(preferred)) {
		return preferred
	}
	vs := InstalledVersions(paths.ZapretVersionsDir())
	if len(vs) == 0 {
		return ""
	}
	sort.Slice(vs, func(i, j int) bool { return CompareVersions(vs[i], vs[j]) > 0 })
	return vs[0]
}

func (m *Manager) refreshStrategiesLocked() {
	list, err := ListStrategies(m.versionDir(m.state.Version))
	if err != nil {
		return
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	m.state.Strategies = names
	found := false
	for _, n := range names {
		if n == m.state.Strategy {
			found = true
		}
	}
	if !found && len(names) > 0 {
		m.state.Strategy = names[0]
	}
}

// ensureInstalled ставит zapret, если ни одной версии ещё нет.
func (m *Manager) ensureInstalled(ctx context.Context) error {
	if v := m.State().Version; v != "" {
		if IsInstalled(m.versionDir(v)) {
			return nil
		}
		// Файлы пропали уже после установки — чаще всего их забрал антивирус
		// (winws.exe и WinDivert он любит). Ставим заново.
		applog.Printf("zapret: файлы версии %s пропали (антивирус?) — скачиваю заново", v)
		_ = os.RemoveAll(m.versionDir(v))
		m.mu.Lock()
		m.state.Version = ""
		m.mu.Unlock()
	}
	m.set(func(s *State) { s.Status, s.Busy = StatusInstalling, "Скачиваю zapret с GitHub…" })
	defer m.set(func(s *State) { s.Busy = "" })
	rel, err := LatestRelease(ctx)
	if err != nil {
		return err
	}
	applog.Printf("zapret: скачиваю версию %s", rel.Version)
	if _, err := Install(ctx, paths.ZapretVersionsDir(), rel); err != nil {
		return fmt.Errorf("не удалось скачать zapret: %w", err)
	}
	applog.Printf("zapret: версия %s установлена", rel.Version)
	m.mu.Lock()
	m.state.Version, m.state.LatestVersion = rel.Version, rel.Version
	m.refreshStrategiesLocked()
	m.mu.Unlock()
	_, _ = config.Update(func(c *config.Config) { c.ZapretVersion = rel.Version })
	return nil
}

// Start включает обход. Безопасно вызывать повторно — перезапустит winws.
func (m *Manager) Start(ctx context.Context) error { return m.start(ctx, false) }

// restartIfWanted — применить новые настройки, если обход включён. Проверка
// под той же блокировкой, что и запуск: иначе «Выключить» могло проскочить
// между проверкой и стартом, и обход поднялся бы сам.
func (m *Manager) restartIfWanted(ctx context.Context) error { return m.start(ctx, true) }

// CancelStart прерывает идущий запуск (например, первую загрузку zapret),
// не дожидаясь его: вызывается перед Stop, чтобы «Выключить» не висело.
func (m *Manager) CancelStart() {
	m.mu.Lock()
	if m.startCancel != nil {
		m.startCancel()
	}
	m.mu.Unlock()
}

func (m *Manager) start(parent context.Context, ifWanted bool) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if parent.Err() != nil || (ifWanted && !m.want) {
		m.mu.Unlock()
		return nil
	}
	m.want = true
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
		return ctx.Err() // отменили — Stop всё уберёт
	}
	if err != nil {
		m.set(func(s *State) { s.Status, s.Error = StatusError, err.Error() })
	}
	return err
}

func (m *Manager) startLocked(ctx context.Context) error {
	if err := m.ensureInstalled(ctx); err != nil {
		return err
	}
	st := m.State()
	dir := m.versionDir(st.Version)
	m.set(func(s *State) { s.Status, s.Error = StatusStarting, "" })

	if err := m.prepareLists(dir); err != nil {
		return fmt.Errorf("списки: %w", err)
	}
	strategies, err := ListStrategies(dir)
	if err != nil {
		return err
	}
	var bat string
	for _, s := range strategies {
		if s.Name == st.Strategy {
			bat = s.File
		}
	}
	if bat == "" {
		return fmt.Errorf("стратегия %q не найдена", st.Strategy)
	}
	tcp, udp := GameFilterPorts(st.GameFilter)
	args, err := ParseBat(bat, Vars{BatDir: dir, GameFilterTCP: tcp, GameFilterUDP: udp})
	if err != nil {
		return fmt.Errorf("%s: %w", st.Strategy, err)
	}

	m.mu.Lock()
	iface := m.iface
	m.mu.Unlock()
	if iface > 0 {
		args = append([]string{fmt.Sprintf("--wf-iface=%d", iface)}, args...)
	}

	m.run.stop()
	stopForeignZapret(dir)
	enableTCPTimestamps()

	applog.Printf("zapret: старт %s, стратегия «%s»", st.Version, st.Strategy)
	return m.run.start(dir, args)
}

// Stop выключает обход и выгружает драйвер WinDivert.
func (m *Manager) Stop() {
	m.CancelStart()
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	m.want = false
	m.mu.Unlock()
	m.run.stop()
	if pids, _ := winutil.FindProcesses("winws.exe"); len(pids) == 0 {
		_ = winutil.StopService("WinDivert", 5*time.Second)
	}
	m.set(func(s *State) { s.Status, s.Error = StatusStopped, "" })
	applog.Printf("zapret: остановлен")
}

func (m *Manager) wanted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.want
}

// SetStrategy меняет стратегию и, если обход включён, сразу перезапускает winws.
func (m *Manager) SetStrategy(ctx context.Context, name string) error {
	m.set(func(s *State) { s.Strategy = name })
	if _, err := config.Update(func(c *config.Config) { c.Strategy = name }); err != nil {
		return err
	}
	return m.restartIfWanted(ctx)
}

func (m *Manager) SetGameFilter(ctx context.Context, mode string) error {
	m.set(func(s *State) { s.GameFilter = mode })
	if _, err := config.Update(func(c *config.Config) { c.GameFilter = mode }); err != nil {
		return err
	}
	return m.restartIfWanted(ctx)
}

// SetInterfaceFilter ограничивает winws одним интерфейсом (0 — все).
func (m *Manager) SetInterfaceFilter(ctx context.Context, idx int) {
	m.mu.Lock()
	changed := m.iface != idx
	m.iface = idx
	m.mu.Unlock()
	if changed {
		if err := m.restartIfWanted(ctx); err != nil {
			applog.Printf("zapret: перезапуск под TUN: %v", err)
		}
	}

}

// HandledDomains — списки доменов, которые сейчас чинит zapret (пусто, если
// он выключен). VPN пускает их напрямую: зачем гнать через сервер то, что и
// так открывается.
func (m *Manager) HandledDomains() string {
	st := m.State()
	if st.Status != StatusRunning || st.Version == "" {
		return ""
	}
	var sb strings.Builder
	for _, f := range []string{"list-general.txt", "list-google.txt", "list-general-user.txt"} {
		if b, err := os.ReadFile(filepath.Join(m.versionDir(st.Version), "lists", f)); err == nil {
			sb.Write(b)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// SetGameEnabled включает/выключает игровую группу доменов.
func (m *Manager) SetGameEnabled(ctx context.Context, id string, on bool) error {
	_, err := config.Update(func(c *config.Config) {
		c.DisabledGames = without(c.DisabledGames, id)
		c.EnabledGames = without(c.EnabledGames, id)
		if on {
			c.EnabledGames = append(c.EnabledGames, id)
		} else {
			c.DisabledGames = append(c.DisabledGames, id)
		}
	})
	if err != nil {
		return err
	}
	m.onChange()
	return m.restartIfWanted(ctx)
}

// CheckUpdate проверяет новую версию zapret; если она есть — скачивает рядом
// и переключается на неё. winws перезапускается только после того, как новая
// версия полностью распакована, так что разрыв — доли секунды.
func (m *Manager) CheckUpdate(ctx context.Context) (updated bool, err error) {
	// Ручная проверка и автообновление не должны качать одновременно: у них
	// общая временная папка распаковки.
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	rel, err := LatestRelease(ctx)
	if err != nil {
		return false, err
	}
	cur := m.State().Version
	m.set(func(s *State) { s.LatestVersion = rel.Version })
	if cur != "" && CompareVersions(rel.Version, cur) <= 0 {
		return false, nil
	}

	m.set(func(s *State) { s.Busy = "Скачиваю zapret " + rel.Version + "…" })
	defer m.set(func(s *State) { s.Busy = "" })
	applog.Printf("zapret: найдено обновление %s → %s", cur, rel.Version)
	if _, err := Install(ctx, paths.ZapretVersionsDir(), rel); err != nil {
		return false, err
	}

	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	prev := m.state.Version
	m.state.Version = rel.Version
	m.refreshStrategiesLocked()
	m.mu.Unlock()
	_, _ = config.Update(func(c *config.Config) { c.ZapretVersion = rel.Version })

	if m.wanted() {
		if err := m.startLocked(ctx); err != nil {
			// Новая версия не взлетела — откатываемся на старую.
			applog.Printf("zapret: %s не запустилась (%v), откат на %s", rel.Version, err, prev)
			m.mu.Lock()
			m.state.Version = prev
			m.refreshStrategiesLocked()
			m.mu.Unlock()
			_, _ = config.Update(func(c *config.Config) { c.ZapretVersion = prev })
			if err2 := m.startLocked(ctx); err2 != nil {
				m.set(func(s *State) { s.Status, s.Error = StatusError, err2.Error() })
			}
			return false, err
		}
	}
	m.onChange()
	m.cleanupOldVersions(rel.Version, prev)
	return true, nil
}

// cleanupOldVersions оставляет текущую и предыдущую версии (для отката).
func (m *Manager) cleanupOldVersions(keep ...string) {
	entries, _ := os.ReadDir(paths.ZapretVersionsDir())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skip := false
		for _, k := range keep {
			if e.Name() == k {
				skip = true
			}
		}
		if !skip {
			_ = os.RemoveAll(filepath.Join(paths.ZapretVersionsDir(), e.Name()))
		}
	}
}

// AutoUpdateLoop раз в 6 часов проверяет обновления (если включено в конфиге).
func (m *Manager) AutoUpdateLoop(ctx context.Context) {
	t := time.NewTimer(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if config.Load().AutoUpdate && m.State().Version != "" {
			if ok, err := m.CheckUpdate(ctx); err != nil {
				applog.Printf("zapret: проверка обновлений: %v", err)
			} else if ok {
				applog.Printf("zapret: обновлено до %s", m.State().Version)
			}
		}
		t.Reset(6 * time.Hour)
	}
}

// prepareLists кладёт пользовательские списки в папку версии. Оригинальные
// списки релиза не трогаем — свои домены добавляются через *-user.txt, как
// и задумано в zapret-discord-youtube.
func (m *Manager) prepareLists(dir string) error {
	ud := paths.UserDir()
	defaults := map[string]string{
		"list-general-user.txt":  "# Свои домены для обхода, по одному в строке\ndomain.example.abc\n",
		"list-exclude-user.txt":  "domain.example.abc\n",
		"ipset-exclude-user.txt": "203.0.113.113/32\n",
	}
	cfg := config.Load()
	rules := CollectGameRules(cfg.DisabledGames, cfg.EnabledGames)
	m.mu.Lock()
	include := append(append([]string(nil), m.extra...), rules.Include...)
	m.mu.Unlock()
	added := map[string][]string{
		"list-general-user.txt":  include,
		"list-exclude-user.txt":  rules.Exclude,
		"ipset-exclude-user.txt": rules.IPExclude,
	}
	if err := patchReleaseExclude(filepath.Join(dir, "lists"), rules.Unexclude); err != nil {
		return err
	}

	for name, def := range defaults {
		src := filepath.Join(ud, name)
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(src, []byte(def), 0o644); err != nil {
				return err
			}
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if extra := added[name]; len(extra) > 0 {
			data = append(data, []byte("\n# добавлено приложением\n"+strings.Join(extra, "\n")+"\n")...)
		}
		if err := os.WriteFile(filepath.Join(dir, "lists", name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// patchReleaseExclude убирает домены из исключений релиза (list-exclude.txt).
// Оригинал хранится рядом как .orig, и файл каждый раз собирается заново из
// него — так выключение группы честно возвращает исключения на место.
func patchReleaseExclude(listsDir string, remove []string) error {
	cur := filepath.Join(listsDir, "list-exclude.txt")
	orig := cur + ".orig"
	if _, err := os.Stat(orig); errors.Is(err, os.ErrNotExist) {
		b, err := os.ReadFile(cur)
		if err != nil {
			return err
		}
		if err := os.WriteFile(orig, b, 0o644); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(orig)
	if err != nil {
		return err
	}
	if len(remove) == 0 {
		return os.WriteFile(cur, b, 0o644)
	}
	drop := map[string]bool{}
	for _, d := range remove {
		drop[strings.ToLower(d)] = true
	}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if !drop[strings.ToLower(strings.TrimSpace(line))] {
			out = append(out, line)
		}
	}
	return os.WriteFile(cur, []byte(strings.Join(out, "\n")), 0o644)
}

func without(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// stopForeignZapret гасит чужие копии zapret: установленную через service.bat
// службу "zapret" и любые winws.exe не из нашей папки. Два winws одновременно
// мешают друг другу (оба перехватывают одни и те же пакеты).
func stopForeignZapret(ourDir string) {
	// Службы других обходчиков на WinDivert: zapret (service.bat) и GoodbyeDPI.
	for _, svcName := range []string{"zapret", "GoodbyeDPI"} {
		if !winutil.ServiceExists(svcName) {
			continue
		}
		applog.Printf("zapret: найдена служба «%s» — останавливаю и перевожу в ручной запуск (два обхода мешают друг другу)", svcName)
		if err := winutil.StopService(svcName, 10*time.Second); err != nil {
			applog.Printf("zapret: %v", err)
		}
		_ = winutil.SetServiceManual(svcName)
	}
	for _, exe := range []string{"goodbyedpi.exe"} {
		pids, _ := winutil.FindProcesses(exe)
		for _, pid := range pids {
			applog.Printf("zapret: завершаю %s (pid %d) — он тоже перехватывает пакеты", exe, pid)
			_ = winutil.KillPID(pid)
		}
	}
	pids, _ := winutil.FindProcesses("winws.exe")
	for _, pid := range pids {
		p := winutil.ProcessPath(pid)
		if p != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(paths.ZapretVersionsDir())) {
			continue
		}
		applog.Printf("zapret: завершаю посторонний winws.exe (pid %d, %s)", pid, p)
		_ = winutil.KillPID(pid)
	}
}

// enableTCPTimestamps — как :tcp_enable в service.bat: часть стратегий
// использует fooling=ts, без таймстемпов он не работает.
func enableTCPTimestamps() {
	cmd := exec.Command("netsh", "interface", "tcp", "set", "global", "timestamps=enabled")
	winutil.HideWindow(cmd)
	_ = cmd.Run()
}
