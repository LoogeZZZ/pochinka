//go:build windows

// Package hotkey - глобальные горячие клавиши: отдельные сочетания для
// главной кнопки, обхода блокировок и VPN. Работают из любого места, даже
// когда окно скрыто в трей.
//
// Реализация: невидимое окно только для сообщений (HWND_MESSAGE) в своём
// потоке со своим циклом GetMessage. RegisterHotKey вызывается только из
// этого потока - WM_HOTKEY приходит в его очередь сообщений, колбэки
// выполняются в отдельных горутинах. Команды из других потоков передаются
// через канал и будят цикл сообщений.
package hotkey

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
	modWin     = 0x0008

	wmHotkey = 0x0312
	wmQuit   = 0x0012
	wmApp    = 0x8000 // своё сообщение: «в очереди есть команды»

	// hwndMessage - HWND_MESSAGE: невидимое окно только для сообщений.
	hwndMessage = ^uintptr(3)
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
)

// msg - структура MSG для GetMessageW (макет под amd64/arm64).
type msg struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      pt
}

type pt struct{ x, y int32 }

// entry - одна зарегистрированная клавиша.
type entry struct {
	name  string
	combo string
	id    int32
	cb    func()
}

type cmd struct {
	name  string
	combo string
	cb    func()
	err   chan error
}

// Manager - поток сообщений и набор горячих клавиш на нём.
type Manager struct {
	mu      sync.Mutex
	cmds    chan cmd
	done    chan struct{}
	closed  bool
	hwnd    uintptr
	pending []cmd
	nextID  int32
	entries map[string]*entry
}

// New запускает поток сообщений. Close останавливает его.
func New() *Manager {
	m := &Manager{
		cmds:    make(chan cmd),
		done:    make(chan struct{}),
		entries: map[string]*entry{},
	}
	go m.fanIn()
	go m.loop()
	return m
}

// fanIn принимает команды из любых потоков и будит цикл сообщений.
func (m *Manager) fanIn() {
	for {
		select {
		case c := <-m.cmds:
			m.mu.Lock()
			m.pending = append(m.pending, c)
			h := m.hwnd
			m.mu.Unlock()
			if h != 0 {
				procPostMessageW.Call(h, wmApp, 0, 0)
			}
		case <-m.done:
			return
		}
	}
}

// Set регистрирует (или меняет) клавишу name. combo == "" - снять клавишу.
// Пустой combo не валидируется; непустой должен быть вида "Ctrl+Alt+P".
func (m *Manager) Set(name, combo string, cb func()) error {
	combo = strings.TrimSpace(combo)
	if combo != "" {
		if _, _, err := ParseCombo(combo); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errClosed
	}
	m.mu.Unlock()
	c := cmd{name: name, combo: combo, cb: cb, err: make(chan error, 1)}
	select {
	case m.cmds <- c:
	case <-m.done:
		return errClosed
	}
	select {
	case err := <-c.err:
		return err
	case <-m.done:
		return errClosed
	}
}

// Combo - зарегистрированная комбинация ("" - клавиша не назначена).
func (m *Manager) Combo(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil {
		return e.combo
	}
	return ""
}

var errClosed = errors.New("менеджер горячих клавиш закрыт")

// Close снимает все клавиши и останавливает поток сообщений.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	h := m.hwnd
	m.mu.Unlock()
	if h != 0 {
		procPostMessageW.Call(h, wmQuit, 0, 0)
	}
	<-m.done
}

// loop живёт на одном потоке ОС: окно, регистрация клавиш и цикл сообщений
// должны быть в одном потоке.
func (m *Manager) loop() {
	runtime.LockOSThread()
	defer close(m.done)

	className, _ := windows.UTF16PtrFromString("STATIC")
	h, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0,
		hwndMessage, 0, 0, 0)
	m.mu.Lock()
	m.hwnd = h
	m.mu.Unlock()
	if h == 0 {
		m.drain(errClosed)
		return
	}

	var mm msg
	for {
		m.drain(nil)
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&mm)), 0, 0, 0)
		if int32(r) <= 0 {
			break // WM_QUIT или ошибка
		}
		switch mm.message {
		case wmHotkey:
			m.mu.Lock()
			e := m.byID(int32(mm.wParam))
			m.mu.Unlock()
			if e != nil && e.cb != nil {
				go e.cb()
			}
			continue
		case wmApp:
			continue // drain на следующей итерации
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&mm)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&mm)))
	}

	// Выход: снять все клавиши, доставить ошибки неразобранным командам.
	m.mu.Lock()
	for _, e := range m.entries {
		procUnregisterHotKey.Call(0, uintptr(e.id))
	}
	m.entries = map[string]*entry{}
	m.mu.Unlock()
	m.drain(errClosed)
	procDestroyWindow.Call(h)
	m.mu.Lock()
	m.hwnd = 0
	m.mu.Unlock()
}

// drain применяет накопившиеся команды на потоке цикла.
func (m *Manager) drain(errForLeftovers error) {
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()
	for _, c := range pending {
		if errForLeftovers != nil {
			c.err <- errForLeftovers
			continue
		}
		c.err <- m.apply(c)
	}
}

// apply - только из потока цикла.
func (m *Manager) apply(c cmd) error {
	if old := m.entries[c.name]; old != nil {
		procUnregisterHotKey.Call(0, uintptr(old.id))
		delete(m.entries, c.name)
	}
	if c.combo == "" {
		return nil
	}
	mods, vk, err := ParseCombo(c.combo)
	if err != nil {
		return err
	}
	m.nextID++
	id := m.nextID
	if r, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk)); r == 0 {
		if err := windows.GetLastError(); err != nil {
			return fmt.Errorf("сочетание %s занято другой программой", c.combo)
		}
		return fmt.Errorf("сочетание %s не получилось зарегистрировать", c.combo)
	}
	m.entries[c.name] = &entry{name: c.name, combo: c.combo, id: id, cb: c.cb}
	return nil
}

// byID - клавиша по идентификатору WM_HOTKEY (под m.mu).
func (m *Manager) byID(id int32) *entry {
	for _, e := range m.entries {
		if e.id == id {
			return e
		}
	}
	return nil
}

// ParseCombo разбирает "Ctrl+Alt+P": модификаторы ctrl/alt/shift/win и
// клавиша - буква, цифра или F1–F24. Возвращает флаги MOD_* и VK-код.
func ParseCombo(combo string) (mods uint32, vk uint32, err error) {
	parts := strings.Split(combo, "+")
	if len(parts) < 2 {
		return 0, 0, errors.New("нужны модификатор и клавиша, например Ctrl+Alt+P")
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			mods |= modControl
		case "alt":
			mods |= modAlt
		case "shift":
			mods |= modShift
		case "win", "meta", "windows":
			mods |= modWin
		default:
			return 0, 0, fmt.Errorf("неизвестный модификатор «%s»", strings.TrimSpace(p))
		}
	}
	if mods == 0 {
		return 0, 0, errors.New("нужен хотя бы один модификатор: Ctrl, Alt, Shift или Win")
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	switch {
	case len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z':
		vk = uint32(key[0]) // VK A–Z совпадают с ASCII
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
		vk = uint32(key[0])
	case strings.HasPrefix(key, "F"):
		n, e := strconv.Atoi(key[1:])
		if e != nil || n < 1 || n > 24 {
			return 0, 0, fmt.Errorf("неизвестная клавиша «%s»", key)
		}
		vk = 0x70 + uint32(n-1)
	default:
		return 0, 0, fmt.Errorf("неизвестная клавиша «%s»", key)
	}
	return mods, vk, nil
}
