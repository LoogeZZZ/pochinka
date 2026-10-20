package config

import (
	"encoding/json"
	"os"
	"sync"

	"pochinka/internal/paths"
)

type Config struct {
	// Strategy — имя .bat-файла стратегии без расширения, например "general (ALT2)".
	Strategy string `json:"strategy"`
	// ZapretVersion — какая распакованная версия zapret сейчас активна.
	ZapretVersion string `json:"zapretVersion"`
	// GameFilter: disabled | all | tcp | udp — как game_filter в service.bat.
	GameFilter string `json:"gameFilter"`
	// AutoUpdate — проверять и ставить обновления zapret в фоне.
	AutoUpdate bool `json:"autoUpdate"`
	// DisabledGames — ID игровых групп, которые пользователь выключил.
	// Хранятся выключенные, а не включённые: новые группы из обновлений
	// приложения включаются сами.
	DisabledGames []string `json:"disabledGames"`
	// EnabledGames — ID групп, выключенных по умолчанию, которые включил пользователь.
	EnabledGames []string `json:"enabledGames"`
	// VPN
	VPNActiveSub string `json:"vpnActiveSub"` // ID активной подписки (сами подписки — в vpn-subs\)
	// Устарело: одиночная подписка до версии с мульти-подписками, переносится автоматически.
	VPNSubURL   string `json:"vpnSubUrl,omitempty"`
	VPNSelected string `json:"vpnSelected,omitempty"`
	VPNMode     string `json:"vpnMode"`     // proxy | tun
	VPNRouteAll bool   `json:"vpnRouteAll"` // через VPN всё, кроме российского
	// VPNNoHWID — не отправлять HWID устройства при загрузке подписки (по
	// умолчанию отправляем: панели с лимитом устройств без него отдают 404).
	VPNNoHWID    bool `json:"vpnNoHwid,omitempty"`
	VPNHTTPPort  int  `json:"vpnHttpPort"`
	VPNSocksPort int  `json:"vpnSocksPort"`
	VPNConnected bool `json:"vpnConnected"`

	// Telegram-прокси (tg-ws-proxy): включён ли, секрет (32 hex) и порт.
	TGEnabled bool   `json:"tgEnabled"`
	TGSecret  string `json:"tgSecret"`
	TGPort    int    `json:"tgPort"`

	// Theme — тема окна: system | light | dark.
	Theme string `json:"theme"`
	// CustomTheme — своя палитра (scheme + 8 цветов #rrggbb), см. themes.go.
	CustomTheme map[string]string `json:"customTheme,omitempty"`
	// SecureDNS — защищённый DNS на сетевых картах: "" | google | cloudflare | quad9.
	SecureDNS string `json:"secureDns,omitempty"`

	// Lang — язык интерфейса: ru | en (пусто — русский).
	Lang string `json:"lang"`

	// LastMode — чем пользовались последним: "zapret" или "vpn" (они
	// взаимоисключающие, главная кнопка включает последний).
	LastMode string `json:"lastMode"`

	// Connected — было ли включено при выходе; при запуске восстанавливаем.
	Connected bool `json:"connected"`
}

func defaults() Config {
	return Config{
		Strategy:   "general",
		GameFilter: "disabled",
		AutoUpdate: true,
	}
}

var mu sync.Mutex

// LastError — последняя ошибка записи конфига (диск полон, нет прав…), для окна.
var LastError error

func Load() Config {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked()
}

func loadLocked() Config {
	c := defaults()
	b, err := os.ReadFile(paths.ConfigFile())
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Strategy == "" {
		c.Strategy = "general"
	}
	if c.GameFilter == "" {
		c.GameFilter = "disabled"
	}
	return c
}

func Save(c Config) error {
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(c)
}

// saveLocked пишет атомарно: во временный файл со случайным именем (его
// нельзя подложить заранее) и переименованием поверх.
func saveLocked(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(paths.DataDir(), "config-*.tmp")
	if err != nil {
		LastError = err
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, paths.ConfigFile())
	}
	if err != nil {
		os.Remove(tmp)
		LastError = err
		return err
	}
	LastError = nil
	return nil
}

// Update загружает конфиг, применяет fn и сохраняет — под одной блокировкой,
// чтобы одновременные изменения не затирали друг друга.
func Update(fn func(c *Config)) (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	c := loadLocked()
	fn(&c)
	return c, saveLocked(c)
}
