package health

import (
	"context"
	"testing"
)

// TestCheckAllExtra проверяет сборку списка целей: встроенные сервисы,
// свои сайты (без дублей и мусора). Контекст отменён заранее, поэтому
// сетевых запросов не будет - важны только ключи.
func TestCheckAllExtra(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := CheckAll(ctx, []string{"example.com", "bad url", "", "EXAMPLE.COM", "discord"})
	for _, key := range []string{"discord", "youtube", "twitch", "example.com"} {
		if _, ok := res[key]; !ok {
			t.Errorf("нет результата для %q", key)
		}
	}
	if _, ok := res["bad url"]; ok {
		t.Error("мусорная строка не должна стать проверкой")
	}
	for k, r := range res {
		if r.OK {
			t.Errorf("%s: открывается при отменённом контексте", k)
		}
	}
}
