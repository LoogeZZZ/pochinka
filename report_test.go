package main

import (
	"strings"
	"testing"

	"pochinka/internal/applog"
)

// В отчёт не должны попадать ссылки подписок, ключи и секреты прокси.
func TestSupportReportNoSecrets(t *testing.T) {
	applog.Printf("подписка недоступна: Get \"https://sub.example.com/api/sub/SECRETtoken123\": EOF")
	applog.Printf("tg: tg://proxy?server=127.0.0.1&port=1443&secret=dd0123456789abcdef0123456789abcdef")
	applog.Printf("vpn: vless://11111111-2222-3333-4444-555555555555@host:443")
	r := (&App{}).SupportReport()
	for _, bad := range []string{"SECRETtoken123", "0123456789abcdef0123456789abcdef", "11111111-2222-3333-4444-555555555555"} {
		if strings.Contains(r, bad) {
			t.Errorf("в отчёте утечка: %s", bad)
		}
	}
	if !strings.Contains(r, "Починка "+appVersion) || !strings.Contains(r, "[Журнал") {
		t.Fatalf("отчёт неполный:\n%s", r)
	}
}
