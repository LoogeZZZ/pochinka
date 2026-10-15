package vpn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Браузеры на Chromium: расширение с правом "proxy" (Touch VPN, Browsec,
// Hola…) подменяет прокси браузера своим, и тот перестаёт смотреть на
// системный прокси Windows — VPN «работает», а браузер идёт мимо него.
var chromiumBrowsers = []struct{ name, dir string }{
	{"Chrome", `Google\Chrome\User Data`},
	{"Edge", `Microsoft\Edge\User Data`},
	{"Яндекс.Браузер", `Yandex\YandexBrowser\User Data`},
	{"Brave", `BraveSoftware\Brave-Browser\User Data`},
	{"Opera", `Opera Software\Opera Stable`},
}

// BrowserProxyHijackers — включённые расширения, которые прямо сейчас
// управляют прокси браузера. Формат: «Touch VPN (Chrome)».
func BrowserProxyHijackers() []string {
	local := os.Getenv("LOCALAPPDATA")
	var out []string
	for _, b := range chromiumBrowsers {
		root := filepath.Join(local, b.dir)
		profiles, _ := filepath.Glob(filepath.Join(root, "*"))
		profiles = append(profiles, root) // у Opera профиль = сама папка
		for _, p := range profiles {
			for _, f := range []string{"Secure Preferences", "Preferences"} {
				for _, id := range proxyExtensions(filepath.Join(p, f)) {
					out = append(out, extensionName(p, id)+" ("+b.name+")")
				}
			}
		}
	}
	return dedupe(out)
}

// proxyExtensions читает настройки профиля и возвращает ID включённых
// расширений, у которых выставлен прокси.
func proxyExtensions(prefsFile string) []string {
	b, err := os.ReadFile(prefsFile)
	if err != nil {
		return nil
	}
	var prefs struct {
		Extensions struct {
			Settings map[string]struct {
				State          *int           `json:"state"`
				DisableReasons []int          `json:"disable_reasons"`
				Preferences    map[string]any `json:"preferences"`
			} `json:"settings"`
		} `json:"extensions"`
	}
	if json.Unmarshal(b, &prefs) != nil {
		return nil
	}
	var ids []string
	for id, s := range prefs.Extensions.Settings {
		if _, ok := s.Preferences["proxy"]; !ok {
			continue
		}
		if len(s.DisableReasons) > 0 || (s.State != nil && *s.State == 0) {
			continue // расширение выключено
		}
		ids = append(ids, id)
	}
	return ids
}

// extensionName достаёт человеческое имя расширения из его manifest.json.
func extensionName(profile, id string) string {
	dirs, _ := filepath.Glob(filepath.Join(profile, "Extensions", id, "*"))
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, "manifest.json"))
		if err != nil {
			continue
		}
		var m struct {
			Name          string `json:"name"`
			DefaultLocale string `json:"default_locale"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		name := m.Name
		if key, ok := strings.CutPrefix(name, "__MSG_"); ok {
			key = strings.TrimSuffix(key, "__")
			name = localized(d, []string{"ru", m.DefaultLocale, "en", "en_US"}, key)
		}
		if name != "" {
			// «Touch VPN - Secure and unlimited VPN proxy» → «Touch VPN»
			if i := strings.Index(name, " - "); i > 0 {
				name = name[:i]
			}
			return name
		}
	}
	return "расширение " + id
}

func localized(dir string, locales []string, key string) string {
	for _, loc := range locales {
		if loc == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "_locales", loc, "messages.json"))
		if err != nil {
			continue
		}
		var msgs map[string]struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &msgs) != nil {
			continue
		}
		for k, v := range msgs {
			if strings.EqualFold(k, key) && v.Message != "" {
				return v.Message
			}
		}
	}
	return ""
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
