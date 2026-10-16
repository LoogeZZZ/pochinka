package vpn

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Mode string

const (
	ModeProxy Mode = "proxy" // системный прокси Windows
	ModeTUN   Mode = "tun"   // виртуальный адаптер Wintun
)

const TunName = "Pochinka"

// tunNames — имена адаптера по порядку. Запасное нужно, когда прошлый
// адаптер ещё удаляется Windows (бывает минутами после перезапуска): новый с
// тем же именем и GUID в это время не создаётся («cannot find the file»).
var tunNames = []string{TunName, TunName + " 2"}

var (
	tunMu  sync.Mutex
	tunCur = TunName
)

// curTun — имя адаптера текущего (или последнего) подключения.
func curTun() string {
	tunMu.Lock()
	defer tunMu.Unlock()
	return tunCur
}

func setCurTun(n string) {
	tunMu.Lock()
	tunCur = n
	tunMu.Unlock()
}

// BuildOptions — всё, что нужно для конфига xray.
type BuildOptions struct {
	Servers   []*Server // один выбранный сервер или все (Auto)
	Auto      bool      // балансировщик leastPing по всем серверам
	Mode      Mode
	HTTPPort  int
	SocksPort int
	LogFile   string
	// PhysIface — физический интерфейс (только TUN): исходящие соединения
	// xray привязываются к нему, иначе они вернутся в TUN и зациклятся.
	PhysIface string
	// ZapretDomains — то, что чинит zapret: пока он работает, эти сайты
	// идут напрямую, а не через VPN (VPN нужен только там, где zapret бессилен).
	ZapretDomains []string
	// UserDomains — сайты, которые пользователь сам отправил через VPN.
	UserDomains []string
	// RouteAll — через VPN всё, кроме российского (а не только базу
	// блокировок). ТСПУ «замедляет» зарубежные хостинги (DigitalOcean,
	// Hetzner…) без всяких списков — в этом режиме они идут через VPN.
	RouteAll bool
	// TunName — имя адаптера (пусто — основное).
	TunName string
}

func (o BuildOptions) tunName() string {
	if o.TunName != "" {
		return o.TunName
	}
	return TunName
}

// ruDirect — что в режиме «всё, кроме российского» всё равно идёт напрямую:
// российские зоны и крупные российские сервисы на зарубежных доменах.
func ruDirect() []string {
	out := []string{"domain:ru", "domain:su", "domain:xn--p1ai", "domain:xn--d1acj3b", "domain:moscow", "domain:xn--80adxhks"}
	for _, h := range bypassHosts {
		out = append(out, "domain:"+h)
	}
	return out
}

// Подсети Telegram (официальный список core.telegram.org/resources/cidr.txt):
// в TUN-режиме медиа Telegram идёт через VPN и не тормозит.
var telegramCIDRs = []string{
	"91.105.192.0/23", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22",
	"91.108.20.0/22", "91.108.56.0/22", "95.161.64.0/20", "149.154.160.0/20", "185.76.151.0/24",
	"2001:67c:4e8::/48", "2001:b28:f23c::/47", "2001:b28:f23f::/48", "2a0a:f280::/32",
}

// rewireForProbe переделывает обычный конфиг в «измерительный»: вместо
// маршрутов по базе блокировок — по отдельному HTTP-входу на каждый сервер.
func rewireForProbe(cfgJSON []byte, ports []int) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		return nil, err
	}
	var inbounds, rules []any
	for i, port := range ports {
		tag := fmt.Sprintf("probe-%d", i)
		inbounds = append(inbounds, map[string]any{"tag": tag, "protocol": "http", "listen": "127.0.0.1", "port": port})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": fmt.Sprintf("proxy-%d", i)})
	}
	cfg["inbounds"] = inbounds
	cfg["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	delete(cfg, "observatory")
	// "log" не трогаем: логгер у xray общий на процесс, и «none» здесь
	// заглушил бы журнал основного подключения.
	return json.Marshal(cfg)
}

// GeoBlockedServices — блокируют пользователей из РФ сами (а не РКН).
var GeoBlockedServices = []string{
	// Anthropic: Claude, Claude Code, API
	"domain:anthropic.com", "domain:claude.ai", "domain:claude.com", "domain:clau.de", "domain:claudeusercontent.com",
	// OpenAI
	"domain:openai.com", "domain:chatgpt.com", "domain:oaistatic.com", "domain:oaiusercontent.com", "domain:sora.com",
	// Google Gemini / AI Studio / NotebookLM (остальной Google — напрямую)
	"domain:gemini.google.com", "domain:aistudio.google.com", "domain:notebooklm.google.com",
	"domain:generativelanguage.googleapis.com", "domain:alkalimakersuite-pa.clients6.google.com", "domain:labs.google",
	// xAI, Perplexity, Mistral, Cursor, Copilot
	"domain:x.ai", "domain:grok.com", "domain:perplexity.ai", "domain:mistral.ai",
	"domain:cursor.com", "domain:cursor.sh", "domain:cursorapi.com", "domain:copilot.microsoft.com",
}

func BuildConfig(o BuildOptions) ([]byte, error) {
	if len(o.Servers) == 0 {
		return nil, fmt.Errorf("нет серверов")
	}
	sockopt := func() map[string]any {
		if o.Mode == ModeTUN && o.PhysIface != "" {
			return map[string]any{"interface": o.PhysIface}
		}
		return nil
	}

	// Первый outbound — маршрут по умолчанию. У нас это «напрямую».
	direct := map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{}}
	if so := sockopt(); so != nil {
		direct["streamSettings"] = map[string]any{"sockopt": so}
	}
	outbounds := []any{direct}

	var proxyTags []string
	for i, s := range o.Servers {
		ob := cloneMap(s.Outbound)
		tag := fmt.Sprintf("proxy-%d", i)
		ob["tag"] = tag
		proxyTags = append(proxyTags, tag)
		if so := sockopt(); so != nil {
			ss, _ := ob["streamSettings"].(map[string]any)
			if ss == nil {
				ss = map[string]any{}
				ob["streamSettings"] = ss
			}
			cur, _ := ss["sockopt"].(map[string]any)
			if cur == nil {
				cur = map[string]any{}
			}
			// dialerProxy-цепочки сами выходят через свой outbound, их не трогаем.
			if _, chained := cur["dialerProxy"]; !chained {
				cur["interface"] = o.PhysIface
			}
			ss["sockopt"] = cur
		}
		outbounds = append(outbounds, ob)
		for _, e := range s.Extra {
			ex := cloneMap(e)
			// Не "proxy-…": иначе балансировщик примет цепочку за сервер.
			ex["tag"] = fmt.Sprintf("chain-%d-%v", i, e["tag"])
			// Ссылку основного outbound на этот dialer тоже переименовываем.
			if ss, ok := ob["streamSettings"].(map[string]any); ok {
				if so, ok := ss["sockopt"].(map[string]any); ok && so["dialerProxy"] == e["tag"] {
					so["dialerProxy"] = ex["tag"]
				}
			}
			if so := sockopt(); so != nil {
				ss, _ := ex["streamSettings"].(map[string]any)
				if ss == nil {
					ss = map[string]any{}
					ex["streamSettings"] = ss
				}
				cur, _ := ss["sockopt"].(map[string]any)
				if cur == nil {
					cur = map[string]any{}
				}
				if _, chained := cur["dialerProxy"]; !chained {
					cur["interface"] = o.PhysIface
				}
				ss["sockopt"] = cur
			}
			outbounds = append(outbounds, ex)
		}
	}
	outbounds = append(outbounds, map[string]any{"tag": "block", "protocol": "blackhole"})

	toProxy := func(r map[string]any) map[string]any {
		if o.Auto && len(proxyTags) > 1 {
			r["balancerTag"] = "auto"
		} else {
			r["outboundTag"] = proxyTags[0]
		}
		return r
	}

	rules := []any{
		map[string]any{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "direct"},
		map[string]any{"type": "field", "domain": []string{"geosite:private"}, "outboundTag": "direct"},
		// Госуслуги, банки и сервисы «только из РФ» — никогда через VPN.
		map[string]any{"type": "field", "domain": []string{"geosite:category-gov-ru", "geosite:ru-available-only-inside"}, "outboundTag": "direct"},
	}
	// Сервисы, которые сами не пускают из России (РКН их не блокирует, поэтому
	// их нет в базе блокировок). Правило стоит выше «того, что чинит zapret»:
	// иначе gemini.google.com ушёл бы напрямую вместе с остальным Google.
	rules = append(rules, toProxy(map[string]any{"type": "field", "domain": GeoBlockedServices}))
	if len(o.UserDomains) > 0 {
		rules = append(rules, toProxy(map[string]any{"type": "field", "domain": o.UserDomains}))
	}
	if len(o.ZapretDomains) > 0 {
		rules = append(rules, map[string]any{"type": "field", "domain": o.ZapretDomains, "outboundTag": "direct"})
	}
	// QUIC (UDP/443) к заблокированным сайтам сразу отбиваем: Vision его не
	// пропускает, и браузер долго ждёт, прежде чем перейти на TCP.
	rules = append(rules,
		map[string]any{"type": "field", "network": "udp", "port": "443", "domain": []string{"geosite:ru-blocked"}, "outboundTag": "block"},
	)
	rules = append(rules,
		toProxy(map[string]any{"type": "field", "domain": []string{"geosite:ru-blocked"}}),
		toProxy(map[string]any{"type": "field", "ip": append([]string{"geoip:ru-blocked-community"}, telegramCIDRs...)}),
	)
	if o.RouteAll {
		// Заблокированные .ru уже ушли в VPN правилом выше; остальное
		// российское — напрямую, всё прочее — через VPN.
		rules = append(rules,
			map[string]any{"type": "field", "domain": ruDirect(), "outboundTag": "direct"},
			// QUIC через VPN не пройдёт (Vision его отвергает) — отбиваем
			// сразу, браузер мгновенно переходит на TCP.
			map[string]any{"type": "field", "network": "udp", "port": "443", "outboundTag": "block"},
			toProxy(map[string]any{"type": "field", "network": "tcp,udp"}),
		)
	}

	inbounds := []any{
		map[string]any{
			"tag": "http-in", "protocol": "http", "listen": "127.0.0.1", "port": o.HTTPPort,
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls"}, "routeOnly": true},
		},
		map[string]any{
			"tag": "socks-in", "protocol": "socks", "listen": "127.0.0.1", "port": o.SocksPort,
			"settings": map[string]any{"udp": true},
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
		},
	}
	if o.Mode == ModeTUN {
		inbounds = append(inbounds, map[string]any{
			"tag": "tun-in", "protocol": "tun", "port": 0,
			"settings": map[string]any{"name": o.tunName(), "mtu": 1500},
			// routeOnly=true: домен из SNI нужен только для выбора маршрута,
			// соединение идёт на исходный IP. С false xray иногда вытаскивал
			// из Chrome домен с точкой в начале (".www.google.com") и падал на
			// DNS (видно в диагностике). Подмены DNS провайдером у нас нет —
			// блокируют по SNI/IP, IP приходят настоящие.
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
		})
	}

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning", "access": "none", "error": o.LogFile},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": rules},
	}
	if o.Auto && len(proxyTags) > 1 {
		cfg["routing"].(map[string]any)["balancers"] = []any{map[string]any{
			"tag": "auto", "selector": []string{"proxy-"},
			"strategy":    map[string]any{"type": "leastPing"},
			"fallbackTag": proxyTags[0],
		}}
		cfg["observatory"] = map[string]any{
			"subjectSelector":   []string{"proxy-"},
			"probeUrl":          probeURL,
			"probeInterval":     "1m",
			"enableConcurrency": true,
		}
	}
	return json.MarshalIndent(cfg, "", "  ")
}
