// Package userlists — свои списки пользователя: домены для обхода,
// исключения, сайты через VPN. Лежат в папке данных, куда пишет только
// администратор, поэтому редактируются из окна программы, а не Блокнотом:
// Блокнот с правами пользователя не может их сохранить, а с правами
// администратора через «Открыть» дал бы доступ к любому файлу системы.
package userlists

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"pochinka/internal/paths"
)

// Kind — что за строки в списке.
type Kind int

const (
	Domains Kind = iota // домены (формат zapret: «^» в начале — без поддоменов)
	IPs                 // адреса и подсети
	Mixed               // домены, адреса и программы (*.exe) — правила VPN
)

// List — один список.
type List struct {
	ID      string
	File    string
	Kind    Kind
	Default string
}

var lists = []List{
	{ID: "zapret-include", File: "list-general-user.txt", Kind: Domains,
		Default: "# Свои домены для обхода, по одному в строке\ndomain.example.abc\n"},
	{ID: "zapret-exclude", File: "list-exclude-user.txt", Kind: Domains,
		Default: "# Домены, которые обход не трогает\ndomain.example.abc\n"},
	{ID: "zapret-ipexclude", File: "ipset-exclude-user.txt", Kind: IPs,
		Default: "# Адреса и подсети, которые обход не трогает\n203.0.113.113/32\n"},
	{ID: "vpn-sites", File: "vpn-sites.txt", Kind: Mixed,
		Default: "# Всегда через VPN: домены, IP/подсети и программы (*.exe), по одному в строке.\n# Например: example.com, 1.2.3.0/24, Discord.exe\n"},
	{ID: "vpn-direct", File: "vpn-direct.txt", Kind: Mixed,
		Default: "# Всегда напрямую, мимо VPN: домены, IP/подсети и программы (*.exe), по одному в строке.\n# Например: mybank.ru, 10.0.0.0/8, steam.exe\n"},
}

// Get — список по ID.
func Get(id string) (List, bool) {
	for _, l := range lists {
		if l.ID == id {
			return l, true
		}
	}
	return List{}, false
}

// ByFile — список по имени файла (для zapret).
func ByFile(name string) (List, bool) {
	for _, l := range lists {
		if l.File == name {
			return l, true
		}
	}
	return List{}, false
}

// Path — где лежит файл списка.
func (l List) Path() string { return filepath.Join(paths.UserDir(), l.File) }

// Ensure создаёт файл со стартовым содержимым, если его нет.
func (l List) Ensure() error {
	if _, err := os.Stat(l.Path()); errors.Is(err, os.ErrNotExist) {
		return writeAtomic(l.Path(), []byte(l.Default))
	}
	return nil
}

// Read — текст списка.
func (l List) Read() (string, error) {
	if err := l.Ensure(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\xef\xbb\xbf"), nil
}

const (
	maxBytes = 512 << 10
	maxLines = 20000
)

// Save проверяет каждую строку и записывает список. Ошибка называет первую
// неверную строку — так пользователь сразу видит, что поправить.
func (l List) Save(text string) (int, error) {
	if len(text) > maxBytes {
		return 0, fmt.Errorf("список слишком большой (больше %d КБ)", maxBytes>>10)
	}
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\xef\xbb\xbf")
	raw := strings.Split(text, "\n")
	if len(raw) > maxLines {
		return 0, fmt.Errorf("слишком много строк (больше %d)", maxLines)
	}
	var out []string
	n := 0
	for i, line := range raw {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			out = append(out, "")
			continue
		case strings.HasPrefix(line, "#"):
			out = append(out, clean(line))
			continue
		}
		if l.Kind == Domains && IsPattern(line) {
			return 0, fmt.Errorf("строка %d: «%s» — в списках обхода шаблоны не работают: zapret понимает только домены (поддомены учитываются сами, «*.» в начале можно)", i+1, clip(line, 60))
		}
		v, ok := normalize(l.Kind, line)
		if !ok {
			if l.Kind == Mixed && IsPattern(line) {
				return 0, fmt.Errorf("строка %d: «%s» — неверный шаблон (примеры: yandex.*, regexp:^img\\d+\\.example\\.com$, keyword:yandex)", i+1, clip(line, 60))
			}
			what := "домен"
			switch l.Kind {
			case IPs:
				what = "адрес или подсеть (например 1.2.3.4 или 1.2.3.0/24)"
			case Mixed:
				what = "домен, адрес/подсеть или программа .exe"
			}
			return 0, fmt.Errorf("строка %d: «%s» — не %s", i+1, clip(line, 60), what)
		}
		out = append(out, v)
		n++
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if err := writeAtomic(l.Path(), []byte(strings.Join(out, "\n")+"\n")); err != nil {
		return 0, err
	}
	return n, nil
}

// normalize приводит строку к виду, понятному zapret и xray, или отказывает.
func normalize(k Kind, s string) (string, bool) {
	if k == Mixed {
		if IsProgram(s) {
			if !validProgram(s) {
				return "", false
			}
			return s, true
		}
		if v, ok := normalize(IPs, s); ok {
			return v, true
		}
		if IsPattern(s) {
			return normalizePattern(s)
		}
		return normalize(Domains, s)
	}
	if k == IPs {
		if p, err := netip.ParsePrefix(s); err == nil {
			return p.Masked().String(), true
		}
		if a, err := netip.ParseAddr(s); err == nil {
			return a.String(), true
		}
		return "", false
	}
	// Вставили ссылку — берём из неё домен.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimPrefix(s, "*.")
	if i := strings.IndexAny(s, "/?#:"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	exact := strings.HasPrefix(s, "^")
	d := strings.TrimPrefix(s, "^")
	if !isDomain(d) {
		return "", false
	}
	if exact {
		return "^" + d, true
	}
	return d, true
}

// IsPattern — шаблон домена, а не сам домен: regexp:…, /…/, keyword:… или
// звёздочка не только в начале («*.yandex.ru» — обычный домен с
// поддоменами). Шаблоны понимает xray (списки VPN), но не zapret.
func IsPattern(s string) bool {
	l := strings.ToLower(s)
	if strings.HasPrefix(l, "regexp:") || strings.HasPrefix(l, "keyword:") ||
		(len(s) > 2 && strings.HasPrefix(s, "/") && strings.HasSuffix(s, "/")) {
		return true
	}
	return strings.Contains(strings.TrimPrefix(s, "*."), "*")
}

const maxPattern = 512

// normalizePattern проверяет шаблон и приводит к виду, который потом
// понимает vpn.DomainRules: regexp:… (проверено компиляцией), keyword:…,
// или домен со звёздочками.
func normalizePattern(s string) (string, bool) {
	if len(s) > maxPattern {
		return "", false
	}
	l := strings.ToLower(s)
	switch {
	case strings.HasPrefix(l, "regexp:") || strings.HasPrefix(s, "/"):
		re := s
		if strings.HasPrefix(l, "regexp:") {
			re = s[len("regexp:"):]
		} else {
			re = s[1 : len(s)-1]
		}
		if strings.TrimSpace(re) == "" {
			return "", false
		}
		if _, err := regexp.Compile(re); err != nil {
			return "", false
		}
		return "regexp:" + re, true
	case strings.HasPrefix(l, "keyword:"):
		w := strings.ToLower(strings.TrimSpace(s[len("keyword:"):]))
		if w == "" || !isDomain(strings.Trim(w, ".-")) {
			return "", false
		}
		return "keyword:" + w, true
	}
	g := strings.ToLower(strings.TrimSuffix(s, "."))
	if strings.Contains(g, "**") || strings.Trim(g, "*.") == "" {
		return "", false
	}
	if !isDomain(strings.ReplaceAll(g, "*", "x")) {
		return "", false
	}
	return g, true
}

// IsProgram — строка про программу: имя файла .exe (как в Диспетчере задач).
func IsProgram(s string) bool { return strings.HasSuffix(strings.ToLower(s), ".exe") }

// validProgram — только имя файла, без пути и спецсимволов.
func validProgram(s string) bool {
	name := s[:len(s)-4]
	if name == "" || len(s) > 128 {
		return false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._-+()", r)) {
			return false
		}
	}
	return true
}

func isDomain(s string) bool {
	if s == "" || len(s) > 253 || strings.Contains(s, "..") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// clean убирает управляющие символы из комментария.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func writeAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), p)
}
