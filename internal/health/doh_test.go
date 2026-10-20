package health

import (
	"context"
	"os"
	"testing"
)

// LIVE_DOH=1 — настоящие запросы к защищённому DNS.
func TestResolveDoHLive(t *testing.T) {
	if os.Getenv("LIVE_DOH") == "" {
		t.Skip("LIVE_DOH=1")
	}
	for _, srv := range dohServers {
		ip, err := dohQuery(context.Background(), srv, "www.youtube.com")
		t.Logf("%s → %v %v", srv, ip, err)
	}
	ip, err := ResolveDoH(context.Background(), "www.youtube.com")
	if err != nil || ip == nil {
		t.Fatal(err)
	}
	if _, err := ResolveDoH(context.Background(), "no-such-host-pochinka.invalid"); err == nil {
		t.Fatal("несуществующее имя «нашлось»")
	}
}

// Сломанный системный DNS: проверка должна сама найти адрес и проверить
// сайт по нему, пометив проблему DNS.
func TestCheckFallsBackToDoHLive(t *testing.T) {
	if os.Getenv("LIVE_DOH") == "" {
		t.Skip("LIVE_DOH=1")
	}
	r := check(context.Background(), target{key: "x", url: "https://www.youtube.com.", minBytes: 1 << 10})
	t.Logf("обычный: %+v", r)
	// Имитируем «DNS провайдера не отдаёт адрес»: заведомо несуществующее
	// для системы имя не подставить, поэтому проверяем вторую половину —
	// запрос по адресу из защищённого DNS.
	ip, err := ResolveDoH(context.Background(), "www.youtube.com")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := fetch(context.Background(), newClient(ip), target{url: "https://www.youtube.com/", minBytes: 1 << 10})
	t.Logf("по %s: %+v %v", ip, r2, err)
}
