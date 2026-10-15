package vpn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"pochinka/internal/paths"
)

const inetKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// bypassHosts — российские сервисы, которые браузер открывает сам, вообще
// минуя прокси. Даже если с VPN что-то случится, они продолжат работать,
// а сервисы, проверяющие «нет ли VPN», не увидят ничего необычного.
// Широкое *.ru не ставим: среди .ru есть и заблокированные сайты.
var bypassHosts = []string{
	"yandex.ru", "yandex.net", "yandex.com", "ya.ru", "yastatic.net", "kinopoisk.ru", "dzen.ru",
	"vk.com", "vk.ru", "vk.me", "vkvideo.ru", "userapi.com", "vkuser.net", "mycdn.me", "ok.ru", "okcdn.ru", "mail.ru",
	"max.ru", "oneme.ru", "rutube.ru", "avito.ru", "ozon.ru", "wildberries.ru", "wb.ru",
	"gosuslugi.ru", "nalog.ru", "nalog.gov.ru", "mos.ru", "gov.ru",
	"sberbank.ru", "sber.ru", "sberbank.com", "tbank.ru", "tbank-online.com", "cdn-tinkoff.ru",
	"vtb.ru", "alfabank.ru", "gazprombank.ru", "raiffeisen.ru", "pochtabank.ru", "mts.ru", "megafon.ru", "beeline.ru",
}

var proxyBypass = func() []string {
	out := []string{"<local>", "localhost", "127.*", "10.*", "192.168.*", "169.254.*",
		"172.16.*", "172.17.*", "172.18.*", "172.19.*", "172.2*", "172.30.*", "172.31.*"}
	for _, h := range bypassHosts {
		out = append(out, h, "*."+h)
	}
	return out
}()

var (
	wininet           = windows.NewLazySystemDLL("wininet.dll")
	internetSetOption = wininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

// prevProxy — настройки прокси пользователя до нас: при выключении VPN
// возвращаем их как были, а не просто стираем.
type prevProxy struct {
	Enable   uint32 `json:"enable"`
	Server   string `json:"server"`
	Override string `json:"override"`
}

func prevProxyFile() string { return filepath.Join(paths.DataDir(), "prev-proxy.json") }

const (
	runOnceKey   = `Software\Microsoft\Windows\CurrentVersion\RunOnce`
	runOnceValue = "PochinkaProxyCleanup"
)

// SetSystemProxy включает системный HTTP-прокси на 127.0.0.1:port.
func SetSystemProxy(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("неверный порт %d", port)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, inetKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	ours := fmt.Sprintf("127.0.0.1:%d", port)
	// Запоминаем прежние настройки (если там уже не наши после краша).
	if srv, _, _ := k.GetStringValue("ProxyServer"); srv != ours {
		var p prevProxy
		if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
			p.Enable = uint32(v)
		}
		p.Server = srv
		p.Override, _, _ = k.GetStringValue("ProxyOverride")
		if b, err := json.Marshal(p); err == nil {
			_ = writeAtomic(prevProxyFile(), b)
		}
		armCrashCleanup(p)
	}
	if err := k.SetStringValue("ProxyServer", ours); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", strings.Join(proxyBypass, ";")); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	notifyProxyChanged()
	return nil
}

// armCrashCleanup: если прилку убьют, выключат питание или перезагрузят
// без нормального выхода — при следующем входе в Windows системный прокси
// выключится сам (RunOnce), даже если Починку не запускать. Иначе браузер
// смотрел бы на мёртвый порт. Ставим только когда у пользователя прокси
// был выключен (возвращать чужой адрес через reg не берёмся).
func armCrashCleanup(p prevProxy) {
	if p.Enable != 0 {
		return
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	cmd := `reg.exe add "HKCU\` + inetKey + `" /v ProxyEnable /t REG_DWORD /d 0 /f`
	_ = k.SetStringValue(runOnceValue, cmd)
}

func disarmCrashCleanup() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(runOnceValue)
}

// ClearSystemProxy выключает системный прокси, если он наш (указывает на
// 127.0.0.1:port), и возвращает прежние настройки. Чужие — не трогаем.
func ClearSystemProxy(port int) {
	disarmCrashCleanup()
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	srv, _, _ := k.GetStringValue("ProxyServer")
	if srv != fmt.Sprintf("127.0.0.1:%d", port) {
		_ = os.Remove(prevProxyFile()) // пользователь уже поставил своё
		return
	}
	var p prevProxy
	if b, err := os.ReadFile(prevProxyFile()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	_ = k.SetDWordValue("ProxyEnable", p.Enable)
	if p.Server != "" {
		_ = k.SetStringValue("ProxyServer", p.Server)
	} else {
		_ = k.DeleteValue("ProxyServer")
	}
	if p.Override != "" {
		_ = k.SetStringValue("ProxyOverride", p.Override)
	} else {
		_ = k.DeleteValue("ProxyOverride")
	}
	_ = os.Remove(prevProxyFile())
	notifyProxyChanged()
}

func notifyProxyChanged() {
	internetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	internetSetOption.Call(0, internetOptionRefresh, 0, 0)
}
