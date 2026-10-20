package vpn

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseUserRules(t *testing.T) {
	r := ParseUserRules("# c\nexample.com\n^exact.org\n1.2.3.4\n10.0.0.0/8\nDiscord.exe\nsteam.exe\n")
	if strings.Join(r.IPs, ",") != "1.2.3.4,10.0.0.0/8" {
		t.Fatalf("ips %v", r.IPs)
	}
	if strings.Join(r.Procs, ",") != "Discord,discord,steam,Steam" {
		t.Fatalf("procs %v", r.Procs)
	}
	if len(r.Domains) != 2 {
		t.Fatalf("domains %v", r.Domains)
	}
}

// Свои правила важнее встроенных: «напрямую» перебивает базу блокировок,
// «через VPN» — правило «Госуслуги всегда напрямую».
func TestUserRulesRouting(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	const uuid = "11111111-2222-3333-4444-555555555555"
	seen := startVlessSniffer(t, "127.0.0.1:39445")
	s, _ := ParseLink("vless://" + uuid + "@127.0.0.1:39445?type=tcp&security=none#local")
	client, err := BuildConfig(BuildOptions{
		Servers: []*Server{s}, Mode: ModeProxy, HTTPPort: 39020, SocksPort: 39021,
		LogFile:    filepath.Join(t.TempDir(), "c1.log"),
		UserDirect: ParseUserRules("instagram.com\n"),
		UserProxy:  ParseUserRules("gosuslugi.ru\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, client)
	for _, h := range []string{"instagram.com", "gosuslugi.ru", "example.com"} {
		connect(t, "127.0.0.1:39020", h+":443")
	}
	time.Sleep(500 * time.Millisecond)
	log := seen()
	if strings.Contains(log, "instagram.com:443") {
		t.Error("instagram.com в «напрямую», а ушёл в VPN")
	}
	if !strings.Contains(log, "gosuslugi.ru:443") {
		t.Error("gosuslugi.ru в «через VPN», а ушёл напрямую")
	}
	if strings.Contains(log, "example.com:443") {
		t.Error("example.com ни в каком списке — должен идти напрямую")
	}
}

// Программа ловится по процессу (здесь — сам тестовый процесс).
func TestUserRulesProcess(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	const uuid = "11111111-2222-3333-4444-555555555555"
	seen := startVlessSniffer(t, "127.0.0.1:39446")
	s, _ := ParseLink("vless://" + uuid + "@127.0.0.1:39446?type=tcp&security=none#local")
	exe, _ := os.Executable()
	client, err := BuildConfig(BuildOptions{
		Servers: []*Server{s}, Mode: ModeProxy, HTTPPort: 39022, SocksPort: 39023,
		LogFile:   filepath.Join(t.TempDir(), "c2.log"),
		UserProxy: ParseUserRules(filepath.Base(exe) + "\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, client)
	// Соединение держим открытым, как настоящая программа: xray ищет процесс
	// по живому соединению, а маршрут выбирает после первых данных.
	c, err := net.DialTimeout("tcp", "127.0.0.1:39022", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	req := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	_, _ = c.Write([]byte(req))
	_, _ = c.Write([]byte("\x16\x03\x01\x00\x05hello"))
	time.Sleep(1500 * time.Millisecond)
	c.Close()
	time.Sleep(300 * time.Millisecond)
	if !strings.Contains(seen(), "example.com:443") {
		t.Errorf("программа %s в «через VPN», а её соединение ушло напрямую", filepath.Base(exe))
	}
}

func TestPatternRule(t *testing.T) {
	cases := map[string]string{
		"yandex.*":             `regexp:^yandex\..*$`,
		"*.yandex.*":           `regexp:^(.+\.)?yandex\..*$`,
		"img*.example.com":     `regexp:^img.*\.example\.com$`,
		`regexp:^a\d+\.b\.ru$`: `regexp:^a\d+\.b\.ru$`,
		`/yandex\.(ru|com)$/`:  `regexp:yandex\.(ru|com)$`,
		"keyword:Yandex":       "keyword:yandex",
		"*.yandex.ru":          "", // обычный домен, не шаблон
		"regexp:(":             "", // неверный — пропуск
		"keyword:":             "",
		"a b*.com":             "",
	}
	for in, want := range cases {
		got, ok := patternRule(in)
		if want == "" {
			if ok {
				t.Errorf("%q: ожидали «не шаблон», получили %q", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q → %q (%v), want %q", in, got, ok, want)
		}
	}
	// В общем разборе шаблоны и домены живут вместе.
	r := ParseUserRules("*.yandex.ru\nyandex.*\nkeyword:vk\n")
	if strings.Join(r.Domains, ",") != `domain:yandex.ru,keyword:vk,regexp:^yandex\..*$` {
		t.Fatalf("domains %v", r.Domains)
	}
}

// Шаблон и регулярка реально уводят трафик в VPN.
func TestUserPatternsRouting(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	const uuid = "11111111-2222-3333-4444-555555555555"
	seen := startVlessSniffer(t, "127.0.0.1:39447")
	s, _ := ParseLink("vless://" + uuid + "@127.0.0.1:39447?type=tcp&security=none#local")
	client, err := BuildConfig(BuildOptions{
		Servers: []*Server{s}, Mode: ModeProxy, HTTPPort: 39024, SocksPort: 39025,
		LogFile:   filepath.Join(t.TempDir(), "c3.log"),
		UserProxy: ParseUserRules("examp*.org\n" + `regexp:^api\d+\.test\.net$` + "\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, client)
	for _, h := range []string{"example.org", "api42.test.net", "api.test.net", "example.com"} {
		connect(t, "127.0.0.1:39024", h+":443")
	}
	time.Sleep(500 * time.Millisecond)
	log := seen()
	for _, h := range []string{"example.org", "api42.test.net"} {
		if !strings.Contains(log, h+":443") {
			t.Errorf("%s подходит под шаблон, но ушёл напрямую", h)
		}
	}
	for _, h := range []string{"api.test.net", "example.com"} {
		if strings.Contains(log, h+":443") {
			t.Errorf("%s не подходит под шаблоны, но ушёл в VPN", h)
		}
	}
}
