package zapret

import (
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

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/paths"
)

// Фильтр по IP — как «IPSet Filter» в service.bat. Стратегии обходят и по
// списку доменов, и по списку адресов ipset-all.txt; режим решает, что в нём:
//
//	""     — список адресов (свежий, если обновляли, иначе из релиза);
//	"none" — заглушка: по адресам ничего не обходится, только по доменам;
//	"any"  — пустой файл: для winws это «любой адрес».
const (
	IPSetList = ""
	IPSetNone = "none"
	IPSetAny  = "any"
)

// Заглушка из service.bat: адрес из документационной сети, его нет в интернете.
// Пустой список для winws значит «все адреса», поэтому «ничего» — это он.
const ipsetPlaceholder = "203.0.113.113/32"

// ipsetURL — тот же файл, что качает «Update IPSet List» в service.bat.
const ipsetURL = "https://raw.githubusercontent.com/" + repo + "/refs/heads/main/.service/ipset-service.txt"

// Свежий список храним рядом с настройками, а не в папке версии: иначе
// обновление zapret молча вернуло бы старый список из релиза.
func ipsetFile() string { return filepath.Join(paths.DataDir(), "ipset-all.txt") }

func ValidIPSetMode(m string) bool { return m == IPSetList || m == IPSetNone || m == IPSetAny }

// IPSetUpdated — когда список адресов обновляли (нулевое время — ни разу).
func IPSetUpdated() time.Time {
	st, err := os.Stat(ipsetFile())
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// SetIPSetMode меняет режим фильтра по IP и перезапускает обход.
func (m *Manager) SetIPSetMode(ctx context.Context, mode string) error {
	if !ValidIPSetMode(mode) {
		return errors.New("неизвестный режим фильтра по IP")
	}
	m.set(func(s *State) { s.IPSet = mode })
	if _, err := config.Update(func(c *config.Config) { c.IPSetMode = mode }); err != nil {
		return err
	}
	return m.restartIfWanted(ctx)
}

// UpdateIPSet скачивает свежий список адресов и, если обход включён,
// перезапускает его. Возвращает число записей.
func (m *Manager) UpdateIPSet(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ipsetURL, nil)
	req.Header.Set("User-Agent", "Pochinka")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("список не скачался: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("список не скачался: HTTP %d", resp.StatusCode)
	}
	const limit = 16 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, fmt.Errorf("список не скачался: %w", err)
	}
	if len(b) > limit {
		return 0, errors.New("список слишком большой")
	}
	clean, n, err := parseIPSet(string(b))
	if err != nil {
		return 0, err
	}
	tmp := ipsetFile() + ".tmp"
	if err := os.WriteFile(tmp, []byte(clean), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, ipsetFile()); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	applog.Printf("zapret: список адресов обновлён, записей: %d", n)
	m.set(func(s *State) { s.IPSetUpdated = IPSetUpdated().Format(time.RFC3339) })
	return n, m.restartIfWanted(ctx)
}

// parseIPSet проверяет скачанный список: только адреса и подсети, по одному в
// строке. Файл пришёл из сети — что-то другое winws не отдаём.
func parseIPSet(text string) (string, int, error) {
	var sb strings.Builder
	n := 0
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\xef\xbb\xbf"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := netip.ParsePrefix(line); err != nil {
			if _, err := netip.ParseAddr(line); err != nil {
				return "", 0, fmt.Errorf("в скачанном списке не адрес в строке %d", i+1)
			}
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		n++
	}
	if n == 0 {
		return "", 0, errors.New("скачанный список пуст")
	}
	return sb.String(), n, nil
}

// prepareIPSet собирает ipset-all.txt версии под выбранный режим. Список из
// релиза хранится рядом как .orig — к нему возвращаемся, если свежего нет.
func prepareIPSet(listsDir, mode string) error {
	cur := filepath.Join(listsDir, "ipset-all.txt")
	orig := cur + ".orig"
	if _, err := os.Stat(orig); errors.Is(err, os.ErrNotExist) {
		b, err := os.ReadFile(cur)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.WriteFile(orig, b, 0o644); err != nil {
			return err
		}
	}
	var data []byte
	switch mode {
	case IPSetNone:
		data = []byte(ipsetPlaceholder + "\n")
	case IPSetAny:
		data = nil
	default:
		b, err := os.ReadFile(ipsetFile())
		if err != nil {
			if b, err = os.ReadFile(orig); err != nil {
				return err
			}
		}
		data = b
	}
	return os.WriteFile(cur, data, 0o644)
}

// hasEntries — есть ли в списке хоть одна запись, кроме комментариев.
// Пустой список winws понимает как «всё»: пустые исключения отключили бы
// обход целиком, поэтому в них кладём заглушку, как и service.bat.
func hasEntries(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "#") {
			return true
		}
	}
	return false
}
