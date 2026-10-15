package applog

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	in := `подписка недоступна: Get "https://sub.example.com/api/sub/AbCdEf123token": EOF; tg://proxy?server=127.0.0.1&port=1443&secret=dd0123456789abcdef0123456789abcdef id 11111111-2222-3333-4444-555555555555 vless://uuid@host:443?x=1`
	out := redact(in)
	for _, bad := range []string{"AbCdEf123token", "0123456789abcdef0123456789abcdef", "11111111-2222", "uuid@host"} {
		if strings.Contains(out, bad) {
			t.Errorf("секрет в журнале: %s", out)
		}
	}
	if !strings.Contains(out, "sub.example.com") {
		t.Errorf("хост должен остаться для диагностики: %s", out)
	}
}
