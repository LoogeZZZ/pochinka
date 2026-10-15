package vpn

import (
	"strings"
	"unicode/utf8"
)

// Подписку присылает чужой сервер, а xray у нас работает с правами
// администратора. Поэтому из outbound'ов подписки берём только то, что нужно
// для подключения, и вычищаем поля, которые заставляют xray трогать файлы
// или систему: masterKeyLog (запись в любой путь, в т.ч. \\сетевой — утечка
// NTLM), certificateFile/keyFile (чтение любых файлов), привязку к
// интерфейсу/метке и т.п.

// Протоколы основного outbound'а сервера.
var allowedProxyProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true, "hysteria": true,
}

// Ключи, которые удаляются на любой глубине (сравнение без учёта регистра).
var forbiddenKeys = map[string]bool{
	"masterkeylog": true, "certificatefile": true, "keyfile": true,
	"certificate": true, "certificates": true, "key": true,
	"customsockopt": true, "interface": true, "mark": true, "tproxy": true,
	"redirect": true, "proxysettings": true, "acceptproxyprotocol": true,
}

const (
	maxServers  = 300
	maxNameLen  = 120
	maxInfoText = 1024
)

// sanitizeServer: false — сервер отбрасывается целиком.
func sanitizeServer(s *Server) bool {
	if s == nil || s.Outbound == nil {
		return false
	}
	p, _ := s.Outbound["protocol"].(string)
	if !allowedProxyProtocols[p] {
		return false
	}
	scrub(s.Outbound)
	var extras []map[string]any
	for _, e := range s.Extra {
		ep, _ := e["protocol"].(string)
		switch {
		case ep == "freedom":
			// Цепочки фрагментации: оставляем только настройки фрагментов.
			st, _ := e["settings"].(map[string]any)
			keep := map[string]any{}
			for _, k := range []string{"fragment", "noises", "domainStrategy"} {
				if v, ok := st[k]; ok {
					keep[k] = v
				}
			}
			e["settings"] = keep
		case allowedProxyProtocols[ep]:
		default:
			continue // прочие протоколы в цепочке не нужны
		}
		scrub(e)
		extras = append(extras, e)
	}
	s.Extra = extras
	s.Name = clipText(s.Name, maxNameLen)
	return true
}

// scrub рекурсивно удаляет запрещённые ключи. dialerProxy (ссылка на
// цепочку внутри той же подписки) оставляем — её переименовывает collectDialers.
func scrub(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if forbiddenKeys[strings.ToLower(k)] {
				delete(t, k)
				continue
			}
			scrub(val)
		}
	case []any:
		for _, e := range t {
			scrub(e)
		}
	}
}

// clipText обрезает строку до n символов (по рунам, без порчи UTF-8) и
// убирает управляющие символы.
func clipText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
