package vpn

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// wgLink собирает ссылку wg:// из JSON (как v2rayN и Hiddify).
func wgLink(t *testing.T, w map[string]any) string {
	t.Helper()
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return "wg://" + base64.StdEncoding.EncodeToString(b)
}

func TestParseWireGuard(t *testing.T) {
	link := wgLink(t, map[string]any{
		"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
		"address":    []string{"10.64.0.2/32", "fd01::2/128"},
		"mtu":        1420,
		"reserved":   []int{1, 2, 3},
		"peers": []any{map[string]any{
			"publicKey":    "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
			"preSharedKey": "a9M6dCmXmGGw0SBFWx3W5t0GC1pSdQrQjJX9Gf3uLmw=",
			"endpoint":     "engage.cloudflare.com:2408",
			"keepAlive":    25,
			"allowedIPs":   []string{"0.0.0.0/0", "::/0"},
		}},
	})
	s, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if s.Protocol != "wireguard" || !s.UDP || s.Address != "engage.cloudflare.com" || s.Port != 2408 {
		t.Fatalf("протокол/адрес не распознались: %+v", s)
	}
	st, ok := s.Outbound["settings"].(map[string]any)
	if !ok {
		t.Fatalf("нет settings в outbound: %v", s.Outbound)
	}
	if st["secretKey"] != "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=" {
		t.Errorf("secretKey не сохранился: %v", st["secretKey"])
	}
	if st["mtu"] != 1420 {
		t.Errorf("mtu не сохранился: %v", st["mtu"])
	}
	if _, ok := st["reserved"]; !ok {
		t.Errorf("reserved не сохранился: %v", st)
	}
	peers, _ := st["peers"].([]any)
	if len(peers) != 1 {
		t.Fatalf("peer'ов должно быть 1, их %d", len(peers))
	}
	peer, _ := peers[0].(map[string]any)
	if peer["endpoint"] != "engage.cloudflare.com:2408" || peer["keepAlive"] != 25 {
		t.Errorf("peer не разобрался: %v", peer)
	}
	// Ядро должно принимать наш outbound.
	if ok, _ := validateServers([]*Server{s}); len(ok) != 1 {
		t.Fatalf("ядро xray не приняло outbound: %v", lastRejected())
	}
}

func TestParseWireGuardAddressString(t *testing.T) {
	// address строкой (не массивом) - тоже бывает у Hiddify.
	link := wgLink(t, map[string]any{
		"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
		"address":    "10.64.0.2/32",
		"peers": []any{map[string]any{
			"publicKey": "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
			"endpoint":  "1.2.3.4:51820",
		}},
	})
	s, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if s.Address != "1.2.3.4" || s.Port != 51820 {
		t.Errorf("адрес не разобрался: %s:%d", s.Address, s.Port)
	}
	if ok, _ := validateServers([]*Server{s}); len(ok) != 1 {
		t.Fatalf("ядро xray не приняло outbound: %v", lastRejected())
	}
}

func TestParseWireGuardBad(t *testing.T) {
	cases := map[string]string{
		"не base64": "wg://not-base64!",
		"не JSON":   "wg://" + base64.StdEncoding.EncodeToString([]byte("hello")),
		"нет ключа": wgLink(t, map[string]any{
			"address": "10.64.0.2/32",
			"peers":   []any{map[string]any{"publicKey": "x", "endpoint": "1.2.3.4:51820"}},
		}),
		"нет address": wgLink(t, map[string]any{
			"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
			"peers":      []any{map[string]any{"publicKey": "x", "endpoint": "1.2.3.4:51820"}},
		}),
		"нет peer'ов": wgLink(t, map[string]any{
			"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
			"address":    "10.64.0.2/32",
		}),
		"peer без endpoint": wgLink(t, map[string]any{
			"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
			"address":    "10.64.0.2/32",
			"peers":      []any{map[string]any{"publicKey": "x"}},
		}),
	}
	for name, link := range cases {
		if _, err := ParseLink(link); err == nil {
			t.Errorf("%s: ожидали ошибку, её нет", name)
		}
	}
}

func TestParseWireGuardFragmentName(t *testing.T) {
	link := wgLink(t, map[string]any{
		"privateKey": "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=",
		"address":    "10.64.0.2/32",
		"peers": []any{map[string]any{
			"publicKey": "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
			"endpoint":  "1.2.3.4:51820",
		}},
	}) + "#Мой%20сервер"
	s, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if s.Name != "Мой сервер" {
		t.Errorf("имя из фрагмента не разобралось: %q", s.Name)
	}
}
