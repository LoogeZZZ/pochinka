package vpn

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
)

var testLinks = map[string]string{
	"vless-reality-vision": "vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?type=tcp&security=reality&pbk=ZDBDzpSVbqcpY-PcbtKwF7vKhvCs7ZwGtL4kC3dGvXQ&sid=ab12&sni=www.max.ru&fp=chrome&flow=xtls-rprx-vision#%F0%9F%87%B3%F0%9F%87%B1%20Нидерланды",
	"vless-xhttp-tls":      "vless://11111111-2222-3333-4444-555555555555@nl.example.com:443?type=xhttp&security=tls&sni=nl.example.com&path=%2Fx&mode=packet-up&extra=%7B%22xPaddingBytes%22%3A%22100-1000%22%7D#XHTTP",
	"vless-ws":             "vless://11111111-2222-3333-4444-555555555555@ws.example.com:443?type=ws&security=tls&host=ws.example.com&path=%2Fws#WS",
	"vless-grpc":           "vless://11111111-2222-3333-4444-555555555555@g.example.com:443?type=grpc&security=tls&serviceName=grpc&mode=multi#gRPC",
	"trojan":               "trojan://secret@t.example.com:443?sni=t.example.com#Trojan",
	"ss-sip002":            "ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass")) + "@5.6.7.8:8388#SS",
	"hy2":                  "hysteria2://authpass@h.example.com:443?sni=h.example.com#Hysteria",
	"vmess": "vmess://" + base64.StdEncoding.EncodeToString([]byte(
		`{"v":"2","ps":"VMess","add":"v.example.com","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","host":"v.example.com","path":"/v","tls":"tls","sni":"v.example.com"}`)),
}

func TestParseLinks(t *testing.T) {
	for name, link := range testLinks {
		s, err := ParseLink(link)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if s.Address == "" || s.Port == 0 || s.Outbound == nil {
			t.Errorf("%s: пустой сервер %+v", name, s)
		}
	}
	s, _ := ParseLink(testLinks["vless-reality-vision"])
	if s.Name != "🇳🇱 Нидерланды" {
		t.Errorf("имя не раскодировано: %q", s.Name)
	}
}

func TestSubscriptionFormats(t *testing.T) {
	var lines []string
	for _, l := range testLinks {
		lines = append(lines, l)
	}
	plain := strings.Join(lines, "\n")
	for name, body := range map[string]string{
		"plain":  plain,
		"base64": base64.StdEncoding.EncodeToString([]byte(plain)),
	} {
		list, err := parseBody([]byte(body))
		if err != nil || len(list) != len(testLinks) {
			t.Errorf("%s: %d серверов, %v", name, len(list), err)
		}
	}

	js := `[{"remarks":"🇩🇪 Германия","outbounds":[
	  {"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"de.example.com","port":443,"users":[{"id":"11111111-2222-3333-4444-555555555555","encryption":"none"}]}]},
	   "streamSettings":{"network":"tcp","security":"tls","sockopt":{"dialerProxy":"fragment"}}},
	  {"tag":"fragment","protocol":"freedom","settings":{"fragment":{"packets":"tlshello","length":"100-200","interval":"10-20"}}},
	  {"tag":"direct","protocol":"freedom"}]}]`
	list, err := parseBody([]byte(js))
	if err != nil || len(list) != 1 || list[0].Address != "de.example.com" || len(list[0].Extra) != 1 {
		t.Fatalf("xray-json: %v %+v", err, list)
	}

	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=100; download=200; total=1000; expire=1893456000")
	h.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Мой VPN")))
	info := parseHeaders(h)
	if info.Title != "Мой VPN" || info.Total != 1000 || info.Download != 200 || info.Expire != 1893456000 {
		t.Errorf("заголовки: %+v", info)
	}
}

// GEO_TEST_DIR — папка с geosite.dat и geoip.dat (ru-only).
func TestConfigsLoadInXray(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	var all []*Server
	for name, link := range testLinks {
		s, err := ParseLink(link)
		if err != nil {
			t.Fatal(name, err)
		}
		all = append(all, s)
	}
	js, _ := parseBody([]byte(`[{"remarks":"x","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"a.b","port":443,"users":[{"id":"11111111-2222-3333-4444-555555555555","encryption":"none"}]}]}, "streamSettings":{"network":"tcp","security":"tls","sockopt":{"dialerProxy":"fragment"}}},{"tag":"fragment","protocol":"freedom","settings":{"fragment":{"packets":"tlshello","length":"100-200","interval":"10-20"}}}]}]`))
	all = append(all, finalize(js)...)

	cases := map[string]BuildOptions{
		"auto-proxy": {Servers: all, Auto: true, Mode: ModeProxy},
		"single":     {Servers: all[:1], Mode: ModeProxy, ZapretDomains: DomainRules("discord.com\n^dns.google\n# c\n"), UserDomains: []string{"domain:example.org"}},
		"tun":        {Servers: all, Auto: true, Mode: ModeTUN, PhysIface: "Ethernet"},
	}
	for name, o := range cases {
		o.HTTPPort, o.SocksPort, o.LogFile = 39001, 39002, filepath.Join(t.TempDir(), "x.log")
		b, err := BuildConfig(o)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cfg, err := serial.LoadJSONConfig(bytes.NewReader(b))
		if err != nil {
			t.Errorf("%s: xray не принял конфиг: %v", name, err)
			continue
		}
		if name == "tun" {
			continue // core.New поднял бы настоящий адаптер
		}
		inst, err := core.New(cfg)
		if err != nil {
			t.Errorf("%s: core.New: %v", name, err)
			continue
		}
		inst.Close()
	}
}

func TestBundledGeoValid(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGeo(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range geoFiles {
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := validGeo(f, b); err != nil {
			t.Errorf("%s: %v", f.name, err)
		}
	}
	// Мусор вместо базы (например, HTML-страница ошибки) не проходит проверку.
	if validGeo(geoFiles[0], bytes.Repeat([]byte("<html>"), 500)) == nil {
		t.Error("HTML принят за базу")
	}
}

func TestRejectsBrokenServers(t *testing.T) {
	good := testLinks["vless-reality-vision"]
	bad := "vless://11111111-2222-3333-4444-555555555555@8.8.8.8:443?type=tcp&security=none#plain"
	list, err := parseBody([]byte(good + "\n" + bad))
	if err != nil || len(list) != 1 || len(lastRejected()) != 1 {
		t.Fatalf("ожидался 1 годный и 1 отброшенный: %d, %v, %v", len(list), lastRejected(), err)
	}
	if _, err := parseBody([]byte(bad)); err == nil {
		t.Fatal("подписка только из негодных серверов должна давать ошибку")
	}
}

// Провайдер на каждый запрос отдаёт другой хост того же сервера — выбор
// пользователя и соединение не должны от этого сбрасываться.
func TestServerIDStableAcrossHostRotation(t *testing.T) {
	a, _ := parseBody([]byte(strings.Replace(testLinks["vless-reality-vision"], "1.2.3.4", "5.6.7.8", 1)))
	b, _ := parseBody([]byte(testLinks["vless-reality-vision"]))
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID {
		t.Fatalf("ID зависит от адреса: %v / %v", a, b)
	}
	if serverIDs(a) != serverIDs(b) {
		t.Fatal("набор серверов считается изменившимся")
	}
}

// Злая подписка: поля, которые заставили бы xray (с правами админа) писать
// или читать файлы, должны быть вычищены, а не-прокси outbound — отброшен.
func TestSanitizeHostileSubscription(t *testing.T) {
	js := `[
	 {"remarks":"evil-main","outbounds":[
	  {"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"a.example","port":443,"users":[{"id":"11111111-2222-3333-4444-555555555555","encryption":"none"}]}]},
	   "streamSettings":{"network":"xhttp","security":"tls",
	     "tlsSettings":{"serverName":"a.example","masterKeyLog":"//evil/share/x","certificates":[{"certificateFile":"C:/Windows/win.ini"}]},
	     "xhttpSettings":{"path":"/","extra":{"downloadSettings":{"address":"b","port":443,"network":"xhttp","security":"tls","tlsSettings":{"masterKeyLog":"C:/pwn.txt"}}}},
	     "sockopt":{"dialerProxy":"frag","interface":"Ethernet","mark":5}}},
	  {"tag":"frag","protocol":"freedom","settings":{"fragment":{"packets":"tlshello","length":"100-200","interval":"1-2"},"redirect":"6.6.6.6:25"}}]},
	 {"remarks":"freedom-as-proxy","outbounds":[{"tag":"proxy","protocol":"freedom","settings":{"redirect":"6.6.6.6:25"}}]}
	]`
	list, err := parseBody([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("freedom под видом сервера должен быть отброшен: %d серверов", len(list))
	}
	b, _ := json.Marshal(append([]any{list[0].Outbound}, func() []any {
		var x []any
		for _, e := range list[0].Extra {
			x = append(x, e)
		}
		return x
	}()...))
	for _, bad := range []string{"masterKeyLog", "certificateFile", "certificates", `"interface"`, `"mark"`, "redirect", "win.ini", "pwn"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("осталось опасное поле %s: %s", bad, b)
		}
	}
	if !strings.Contains(string(b), "dialerProxy") || !strings.Contains(string(b), "fragment") {
		t.Errorf("цепочка фрагментации должна сохраниться: %s", b)
	}
}
