package vpn

import (
	"strings"
	"testing"
	"time"
)

func TestSubWarn(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		info SubInfo
		want string // "" - предупреждений нет; иначе подстрока
	}{
		{"всё в порядке", SubInfo{Total: 100 << 30, Upload: 10 << 30, Expire: now + 30*86400}, ""},
		{"безлимит и бессрочно", SubInfo{}, ""},
		{"истекла", SubInfo{Expire: now - 86400}, "истекла"},
		{"истекает завтра", SubInfo{Expire: now + 86400}, "истекает"},
		{"истекает через 3 дня", SubInfo{Expire: now + 3*86400 - 3600}, "истекает"},
		{"истекает через неделю", SubInfo{Expire: now + 7*86400}, ""},
		{"трафик закончился", SubInfo{Total: 100, Upload: 60, Download: 40}, "закончился"},
		{"трафик на исходе", SubInfo{Total: 100, Upload: 91, Download: 0}, "исходе"},
		{"трафика ещё много", SubInfo{Total: 100, Upload: 60, Download: 20}, ""},
	}
	for _, c := range cases {
		got := subWarn(c.info)
		if c.want == "" && got != "" {
			t.Errorf("%s: ожидали пустое предупреждение, получили %q", c.name, got)
		}
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: ожидали %q в предупреждении, получили %q", c.name, c.want, got)
		}
	}
}

// TestSubWarnLanguage - предупреждение должно быть на языке интерфейса
// (config.Load читает настоящий конфиг, поэтому допускаем оба языка).
func TestSubWarnLanguage(t *testing.T) {
	got := subWarn(SubInfo{Expire: time.Now().Unix() - 100})
	if !strings.Contains(got, "истекла") && !strings.Contains(got, "expired") {
		t.Errorf("предупреждение не на известном языке: %q", got)
	}
}
