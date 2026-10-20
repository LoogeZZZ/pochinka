package vpn

import (
	"net/netip"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"pochinka/internal/userlists"
)

// UserRules — свои правила VPN: домены, адреса и программы.
type UserRules struct {
	Domains []string // в формате правил xray (см. DomainRules)
	IPs     []string // адреса и подсети
	Procs   []string // имена программ без .exe, в нескольких написаниях
}

func (r UserRules) empty() bool { return len(r.Domains)+len(r.IPs)+len(r.Procs) == 0 }

// ParseUserRules разбирает список «через VPN» или «напрямую».
func ParseUserRules(text string) UserRules {
	var r UserRules
	var domains []string
	seenProc := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case userlists.IsProgram(line):
			// xray сравнивает имя процесса точно (без .exe, с учётом
			// регистра), а пользователь пишет как помнит — добавляем
			// написания «как есть», строчными и с заглавной.
			name := line[:len(line)-4]
			for _, v := range []string{name, strings.ToLower(name), capitalize(strings.ToLower(name))} {
				if !seenProc[v] {
					seenProc[v] = true
					r.Procs = append(r.Procs, v)
				}
			}
		case isIPOrPrefix(line):
			r.IPs = append(r.IPs, line)
		default:
			domains = append(domains, line)
		}
	}
	r.Domains = DomainRules(strings.Join(domains, "\n"))
	return r
}

func isIPOrPrefix(s string) bool {
	if _, err := netip.ParsePrefix(s); err == nil {
		return true
	}
	_, err := netip.ParseAddr(s)
	return err == nil
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// readUserRules — список по ID (vpn-sites, vpn-direct).
func readUserRules(id string) UserRules {
	l, ok := userlists.Get(id)
	if !ok {
		return UserRules{}
	}
	text, err := l.Read()
	if err != nil {
		return UserRules{}
	}
	return ParseUserRules(text)
}

// patternRule — шаблон домена в правило xray. ok=false — это не шаблон
// (или шаблон неверный — тогда строку пропускаем: из-за одной кривой строки
// VPN не должен перестать запускаться).
//
//	regexp:<re>, /<re>/ → regexp:<re> (синтаксис Go, как в xray)
//	keyword:<слово>     → keyword:<слово>
//	yandex.*, a*b.com   → regexp:^…$ (звёздочка — любые символы);
//	                      «*.» в начале — «сам домен или любой поддомен»
func patternRule(line string) (string, bool) {
	l := strings.ToLower(line)
	switch {
	case strings.HasPrefix(l, "regexp:"):
		return regexpRule(line[len("regexp:"):])
	case len(line) > 2 && strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/"):
		return regexpRule(line[1 : len(line)-1])
	case strings.HasPrefix(l, "keyword:"):
		w := strings.TrimSpace(l[len("keyword:"):])
		if !looksLikeDomain(w) {
			return "", false
		}
		return "keyword:" + w, true
	}
	rest := strings.TrimPrefix(l, "*.")
	if !strings.Contains(rest, "*") {
		return "", false
	}
	if !looksLikeDomain(strings.ReplaceAll(rest, "*", "x")) {
		return "", false
	}
	var b strings.Builder
	b.WriteString("^")
	if strings.HasPrefix(l, "*.") {
		b.WriteString(`(.+\.)?`)
	}
	for i, part := range strings.Split(rest, "*") {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	b.WriteString("$")
	return regexpRule(b.String())
}

func regexpRule(re string) (string, bool) {
	if strings.TrimSpace(re) == "" || len(re) > 512 {
		return "", false
	}
	if _, err := regexp.Compile(re); err != nil {
		return "", false
	}
	return "regexp:" + re, true
}
