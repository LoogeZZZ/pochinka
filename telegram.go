package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/tgproxy"
)

// TGState — состояние Telegram-прокси для окна.
type TGState struct {
	Running   bool          `json:"running"`
	Error     string        `json:"error"`
	Port      int           `json:"port"`
	Installed bool          `json:"installed"` // зарегистрирован ли обработчик tg://
	Stats     tgproxy.Stats `json:"stats"`
}

type telegram struct {
	mu    sync.Mutex
	proxy *tgproxy.Proxy
	err   string
}

// ensureTGConfig — секрет и порт создаются один раз: иначе Telegram
// пришлось бы перенастраивать после каждого перезапуска.
// tgMemo — секрет и порт в памяти: если конфиг не сохраняется (диск полон,
// нет прав), без этого каждый вызов придумывал бы новый секрет, и ссылка
// не совпала бы с запущенным прокси.
var tgMemo struct {
	sync.Mutex
	secret string
	port   int
}

func ensureTGConfig() config.Config {
	tgMemo.Lock()
	defer tgMemo.Unlock()
	c := config.Load()
	if len(c.TGSecret) == 32 && c.TGPort > 0 && c.TGPort <= 65535 {
		if _, err := hex.DecodeString(c.TGSecret); err == nil {
			tgMemo.secret, tgMemo.port = c.TGSecret, c.TGPort
			return c
		}
	}
	if tgMemo.secret != "" {
		c.TGSecret, c.TGPort = tgMemo.secret, tgMemo.port
		return c
	}
	c, _ = config.Update(func(c *config.Config) {
		if _, err := hex.DecodeString(c.TGSecret); err != nil || len(c.TGSecret) != 32 {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			c.TGSecret = hex.EncodeToString(b)
		}
		if c.TGPort <= 0 || c.TGPort > 65535 {
			c.TGPort = 1443
		}
	})
	tgMemo.secret, tgMemo.port = c.TGSecret, c.TGPort
	return c
}

func memoTGPort(n int) {
	tgMemo.Lock()
	tgMemo.port = n
	tgMemo.Unlock()
}

// start поднимает прокси. Если порт занят чужой программой — выбираем
// свободный и запоминаем (Telegram получит новую ссылку при «Подключить»).
func (t *telegram) start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.proxy != nil {
		return nil
	}
	c := ensureTGConfig()
	secret, _ := hex.DecodeString(c.TGSecret)
	opts := tgproxy.Options{
		Listen: fmt.Sprintf("127.0.0.1:%d", c.TGPort), Secret: secret,
		Logf: func(f string, a ...any) { applog.Printf(f, a...) },
	}
	p, err := tgproxy.Start(opts)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			opts.Listen = "127.0.0.1:0"
			p, err = tgproxy.Start(opts)
			if err == nil {
				_, port, _ := net.SplitHostPort(p.Addr())
				var n int
				fmt.Sscanf(port, "%d", &n)
				_, _ = config.Update(func(c *config.Config) { c.TGPort = n })
				memoTGPort(n)
				applog.Printf("tg: порт %d занят, прокси на %d", c.TGPort, n)
			}
		}
	}
	if err != nil {
		t.err = err.Error()
		return err
	}
	t.proxy, t.err = p, ""
	applog.Printf("tg: прокси для Telegram запущен на %s", p.Addr())
	return nil
}

func (t *telegram) stop() {
	t.mu.Lock()
	p := t.proxy
	t.proxy = nil
	t.mu.Unlock()
	if p != nil {
		p.Close()
		applog.Printf("tg: прокси остановлен")
	}
}

func (t *telegram) state() TGState {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := TGState{Running: t.proxy != nil, Error: t.err, Port: ensureTGConfig().TGPort, Installed: tgHandlerInstalled()}
	if t.proxy != nil {
		st.Stats = t.proxy.Stats()
	}
	return st
}

// link — ссылка, по которой Telegram Desktop сам предложит включить прокси.
func (t *telegram) link() string {
	c := ensureTGConfig()
	return fmt.Sprintf("tg://proxy?server=127.0.0.1&port=%d&secret=dd%s", c.TGPort, c.TGSecret)
}

// tgHandlerInstalled — установлен ли Telegram Desktop (зарегистрирована
// схема tg://). Без него ссылку открыть нечем.
func tgHandlerInstalled() bool {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.CLASSES_ROOT} {
		path := `tg\shell\open\command`
		if root == registry.CURRENT_USER {
			path = `Software\Classes\` + path
		}
		k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, _ := k.GetStringValue("")
		k.Close()
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// ---- методы для окна ----

// SetTelegram включает/выключает прокси для Telegram.
func (a *App) SetTelegram(on bool) string {
	_, _ = config.Update(func(c *config.Config) { c.TGEnabled = on })
	defer a.emitState()
	if !on {
		a.tg.stop()
		return ""
	}
	if err := a.tg.start(); err != nil {
		return "Прокси не запустился: " + err.Error()
	}
	return ""
}

// ConnectTelegram открывает в Telegram Desktop ссылку прокси (Telegram
// спросит «Подключить прокси?» — один тап). Прокси включается, если был выключен.
func (a *App) ConnectTelegram() string {
	if !tgHandlerInstalled() {
		return "Не найден Telegram Desktop — установите его с desktop.telegram.org"
	}
	if msg := a.SetTelegram(true); msg != "" {
		return msg
	}
	openAsUser(a.tg.link())
	applog.Printf("tg: ссылка прокси отправлена в Telegram")
	return ""
}

// TelegramLink — для «Скопировать ссылку» (Telegram на другом устройстве не
// подойдёт: прокси слушает только этот компьютер).
func (a *App) TelegramLink() string { return a.tg.link() }
