// Package hostsfile — записи hosts для Discord и Telegram из
// zapret-discord-youtube («Update Hosts File» в service.bat). Оригинал
// открывает Блокнот и просит скопировать строки руками; мы пишем их сами,
// отдельным помеченным блоком, и только его же потом убираем.
package hostsfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const sourceURL = "https://raw.githubusercontent.com/Flowseal/zapret-discord-youtube/refs/heads/main/.service/hosts"

const (
	beginMark = "# >>> Pochinka: Discord и Telegram (zapret-discord-youtube). Блок ведёт программа, правки внутри пропадут."
	endMark   = "# <<< Pochinka"
)

// allowed — домены, которые разрешено прописывать. Файл приходит из сети, и
// программа с правами администратора не должна дать подменить через него
// адрес банка или Госуслуг: всё остальное отбрасывается.
var allowed = []string{
	"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media",
	"telegram.org", "telegram.me", "telegram.dog", "telegram.space", "t.me", "telesco.pe", "tg.dev",
	"github.com", "githubusercontent.com", "githubassets.com",
}

func allowedHost(h string) bool {
	for _, d := range allowed {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// Path — системный файл hosts.
func Path() string {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		dir = `C:\Windows\System32`
	}
	return filepath.Join(dir, "drivers", "etc", "hosts")
}

// Active — сколько записей сейчас в нашем блоке (0 — блока нет).
func Active() int {
	b, err := os.ReadFile(Path())
	if err != nil {
		return 0
	}
	_, block := split(string(b))
	return len(block)
}

// Enable скачивает свежие записи и ставит их в hosts вместо прежнего блока.
// Возвращает число записей.
func Enable(ctx context.Context) (int, error) {
	text, err := download(ctx)
	if err != nil {
		return 0, err
	}
	entries, err := Parse(text)
	if err != nil {
		return 0, err
	}
	return len(entries), write(entries)
}

// Disable убирает наш блок; чужие строки остаются как были.
func Disable() error {
	if _, err := os.Stat(Path()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return write(nil)
}

func download(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// Метка в адресе — чтобы не получить старую копию из кэша, как в service.bat.
	url := fmt.Sprintf("%s?t=%d", sourceURL, time.Now().Unix())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Pochinka")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("записи не скачались: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("записи не скачались: HTTP %d", resp.StatusCode)
	}
	const limit = 256 << 10
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", fmt.Errorf("записи не скачались: %w", err)
	}
	if len(b) > limit {
		return "", errors.New("файл записей слишком большой")
	}
	return string(b), nil
}

// Parse оставляет из скачанного только строки «адрес имя» для разрешённых
// доменов, в виде «адрес имя».
func Parse(text string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(strings.TrimPrefix(line, "\xef\xbb\xbf"))
		if len(f) < 2 {
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil || ip.IsUnspecified() || ip.IsLoopback() {
			continue
		}
		for _, h := range f[1:] {
			h = strings.ToLower(strings.TrimSuffix(h, "."))
			if !validName(h) || !allowedHost(h) {
				continue
			}
			e := ip.String() + " " + h
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("в скачанном файле нет подходящих записей")
	}
	return out, nil
}

func validName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// split делит hosts на «всё, кроме нашего блока» и записи блока.
func split(text string) (rest string, block []string) {
	var keep []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == beginMark:
			in = true
		case in && t == endMark:
			in = false
		case in:
			if t != "" && !strings.HasPrefix(t, "#") {
				block = append(block, t)
			}
		default:
			keep = append(keep, line)
		}
	}
	return strings.TrimRight(strings.Join(keep, "\r\n"), "\r\n \t"), block
}

// write ставит блок с entries (nil — убирает блок). Строки, которые уже есть
// в hosts вне блока (их могли скопировать руками), не дублируем.
func write(entries []string) error { return writeTo(Path(), entries) }

func writeTo(p string, entries []string) error {
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rest, _ := split(string(old))
	have := map[string]bool{}
	for _, line := range strings.Split(rest, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			have[strings.ToLower(f[0]+" "+f[1])] = true
		}
	}
	var sb strings.Builder
	sb.WriteString(rest)
	if len(entries) > 0 {
		sb.WriteString("\r\n\r\n" + beginMark + "\r\n")
		for _, e := range entries {
			if !have[e] {
				sb.WriteString(e + "\r\n")
			}
		}
		sb.WriteString(endMark)
	}
	sb.WriteString("\r\n")
	out := []byte(sb.String())
	if bytes.Equal(out, old) {
		return nil
	}
	// Пишем поверх, а не через переименование: так у hosts остаются его права.
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return fmt.Errorf("не удалось записать hosts: %w", err)
	}
	flush()
	return nil
}

var flushProc = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")

// flush сбрасывает кэш DNS, чтобы записи заработали сразу.
func flush() { _, _, _ = flushProc.Call() }
