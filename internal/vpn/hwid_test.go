package vpn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"pochinka/internal/config"
)

// Та же проверка, что у Remnawave (extract-hwid-headers.util.ts).
var remnawaveHWID = regexp.MustCompile(`^[a-zA-Z0-9=-]{10,64}$`)

func TestDeviceInfo(t *testing.T) {
	d := Device()
	if !remnawaveHWID.MatchString(d.HWID) {
		t.Fatalf("HWID %q не пройдёт проверку Remnawave", d.HWID)
	}
	if hwid() != d.HWID {
		t.Fatal("HWID должен быть постоянным")
	}
	for _, v := range []string{d.OS, d.OSVersion, d.Model} {
		if v == "" || asciiClean(v) != v {
			t.Fatalf("заголовок %q пустой или не ASCII", v)
		}
	}
	t.Logf("HWID %s, %s %s, %s", d.HWID, d.OS, d.OSVersion, d.Model)
}

func TestFetchSendsHWID(t *testing.T) {
	isolate(t)
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.Header.Get("x-hwid") == "" {
			w.WriteHeader(404) // как Remnawave с лимитом устройств
			return
		}
		w.Write([]byte(testLinks["vless-ws"]))
	}))
	defer srv.Close()

	servers, _, err := FetchSubscription(context.Background(), srv.URL)
	if err != nil || len(servers) != 1 {
		t.Fatalf("с HWID подписка должна загрузиться: %v %d", err, len(servers))
	}
	d := Device()
	if got.Get("x-hwid") != d.HWID || got.Get("x-device-os") != "Windows" || got.Get("x-ver-os") != d.OSVersion || got.Get("x-device-model") != d.Model {
		t.Fatalf("заголовки: %v", got)
	}

	_, _ = config.Update(func(c *config.Config) { c.VPNNoHWID = true })
	_, _, err = FetchSubscription(context.Background(), srv.URL)
	if got.Get("x-hwid") != "" {
		t.Fatal("HWID ушёл, хотя отправка выключена")
	}
	if err == nil || !strings.Contains(err.Error(), "требует HWID") {
		t.Fatalf("без HWID нужна подсказка включить его, а не %v", err)
	}
}
