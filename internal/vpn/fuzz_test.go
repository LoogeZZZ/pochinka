package vpn

import (
	"encoding/base64"
	"testing"
)

// Фаззинг разбора подписок: любые байты от сервера не должны ронять прилку.
func FuzzParseBody(f *testing.F) {
	for _, l := range testLinks {
		f.Add([]byte(l))
		f.Add([]byte(base64.StdEncoding.EncodeToString([]byte(l))))
	}
	f.Add([]byte(`[{"remarks":"x","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"a","port":1}]},"streamSettings":{"sockopt":{"dialerProxy":"proxy"}}}]}]`))
	f.Add([]byte(`{"outbounds":[{"protocol":"freedom"}]}`))
	f.Add([]byte("vless://@:0?type=xhttp&extra=%7B%7D"))
	f.Fuzz(func(t *testing.T, b []byte) {
		list, _ := parseBody(b)
		for _, s := range list {
			if s.Outbound == nil || s.ID == "" {
				t.Fatalf("сервер без outbound/ID: %+v", s)
			}
		}
	})
}

func FuzzParseLink(f *testing.F) {
	for _, l := range testLinks {
		f.Add(l)
	}
	f.Add("ss://")
	f.Add("vmess://e30=")
	f.Add("hysteria2://a@[::1]:443?mport=1-65535")
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseLink(s) })
}

func FuzzDomainRules(f *testing.F) {
	f.Add("discord.com\n^dns.google\n# x\n\xef\xbb\xbfya.ru\n")
	f.Fuzz(func(t *testing.T, s string) {
		for _, r := range DomainRules(s) {
			if len(r) < 6 {
				t.Fatalf("странное правило %q", r)
			}
		}
	})
}
