package vpn

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Server — один сервер из подписки, уже в виде outbound для xray.
type Server struct {
	ID       string         `json:"id"`   // стабильный ключ: протокол+адрес+порт+имя
	Name     string         `json:"name"` // как в подписке, с флагом
	Protocol string         `json:"protocol"`
	Address  string         `json:"address"`
	Port     int            `json:"port"`
	UDP      bool           `json:"udp"` // транспорт поверх UDP (hysteria, kcp) — TCP-пинг не показателен
	Outbound map[string]any `json:"-"`   // outbound xray без tag
	// Extra — вспомогательные outbound'ы, на которые ссылается основной
	// (dialerProxy в JSON-подписках). Теги уже переименованы.
	Extra []map[string]any `json:"-"`
}

// ParseLink разбирает одну ссылку вида vless://, vmess://, trojan://, ss://, hysteria2://.
func ParseLink(link string) (*Server, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return nil, errors.New("не ссылка")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVless(link)
	case "vmess":
		return parseVmess(link)
	case "trojan":
		return parseTrojan(link)
	case "ss":
		return parseSS(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	}
	return nil, fmt.Errorf("протокол %s не поддерживается", scheme)
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port <= 0 || port > 65535 {
		return "", 0, errors.New("нет адреса или порта")
	}
	return host, port, nil
}

func fragmentName(u *url.URL, fallback string) string {
	if n := strings.TrimSpace(u.Fragment); n != "" {
		return n
	}
	return fallback
}

func parseVless(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	user := map[string]any{"id": u.User.Username(), "encryption": firstNonEmpty(q.Get("encryption"), "none")}
	if f := q.Get("flow"); f != "" {
		user["flow"] = f
	}
	stream, udp, err := streamFromQuery(q, host)
	if err != nil {
		return nil, err
	}
	return &Server{
		Name: fragmentName(u, host), Protocol: "vless", Address: host, Port: port, UDP: udp,
		Outbound: map[string]any{
			"protocol":       "vless",
			"settings":       map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}}},
			"streamSettings": stream,
		},
	}, nil
}

func parseTrojan(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if q.Get("security") == "" {
		q.Set("security", "tls") // у trojan TLS по умолчанию
	}
	stream, udp, err := streamFromQuery(q, host)
	if err != nil {
		return nil, err
	}
	return &Server{
		Name: fragmentName(u, host), Protocol: "trojan", Address: host, Port: port, UDP: udp,
		Outbound: map[string]any{
			"protocol":       "trojan",
			"settings":       map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "password": u.User.Username()}}},
			"streamSettings": stream,
		},
	}, nil
}

func parseVmess(link string) (*Server, error) {
	raw := strings.TrimPrefix(strings.TrimPrefix(link, "vmess://"), "VMESS://")
	b, err := decodeB64(raw)
	if err != nil {
		return nil, errors.New("vmess: не base64")
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, errors.New("vmess: не JSON")
	}
	s := func(k string) string { return strings.TrimSpace(fmt.Sprint(valueOr(v[k], ""))) }
	host := s("add")
	port, _ := strconv.Atoi(s("port"))
	if host == "" || port <= 0 {
		return nil, errors.New("vmess: нет адреса")
	}
	q := url.Values{}
	q.Set("type", firstNonEmpty(s("net"), "tcp"))
	q.Set("headerType", s("type"))
	q.Set("host", s("host"))
	q.Set("path", s("path"))
	q.Set("sni", s("sni"))
	q.Set("alpn", s("alpn"))
	q.Set("fp", s("fp"))
	if s("tls") == "tls" {
		q.Set("security", "tls")
	}
	if q.Get("type") == "grpc" {
		q.Set("serviceName", s("path"))
	}
	stream, udp, err := streamFromQuery(q, host)
	if err != nil {
		return nil, err
	}
	aid, _ := strconv.Atoi(s("aid"))
	return &Server{
		Name: firstNonEmpty(s("ps"), host), Protocol: "vmess", Address: host, Port: port, UDP: udp,
		Outbound: map[string]any{
			"protocol": "vmess",
			"settings": map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{
				map[string]any{"id": s("id"), "alterId": aid, "security": firstNonEmpty(s("scy"), "auto")},
			}}}},
			"streamSettings": stream,
		},
	}, nil
}

// ss://BASE64(method:password)@host:port#name  или  ss://BASE64(method:password@host:port)#name
func parseSS(link string) (*Server, error) {
	body := strings.TrimPrefix(link, "ss://")
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	if i := strings.Index(body, "?"); i >= 0 {
		body = body[:i] // плагины не поддерживаем
	}
	var userinfo, hp string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userinfo, hp = body[:at], body[at+1:]
		if dec, err := decodeB64(userinfo); err == nil {
			userinfo = string(dec)
		} else if un, err := url.PathUnescape(userinfo); err == nil {
			userinfo = un
		}
	} else {
		dec, err := decodeB64(body)
		if err != nil {
			return nil, errors.New("ss: не разобрать")
		}
		at := strings.LastIndex(string(dec), "@")
		if at < 0 {
			return nil, errors.New("ss: нет адреса")
		}
		userinfo, hp = string(dec[:at]), string(dec[at+1:])
	}
	method, password, ok := strings.Cut(userinfo, ":")
	host, portS, err := net.SplitHostPort(hp)
	port, _ := strconv.Atoi(portS)
	if !ok || err != nil || port <= 0 {
		return nil, errors.New("ss: неверный формат")
	}
	return &Server{
		Name: firstNonEmpty(name, host), Protocol: "shadowsocks", Address: host, Port: port,
		Outbound: map[string]any{
			"protocol": "shadowsocks",
			"settings": map[string]any{"servers": []any{map[string]any{
				"address": host, "port": port, "method": method, "password": password,
			}}},
		},
	}, nil
}

// hysteria2://auth@host:port?sni=..&insecure=1&obfs=..#name
func parseHysteria2(link string) (*Server, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if q.Get("obfs") != "" {
		return nil, errors.New("hysteria2 с obfs не поддерживается ядром")
	}
	auth := u.User.Username()
	if p, ok := u.User.Password(); ok {
		auth += ":" + p
	}
	tls := map[string]any{"serverName": firstNonEmpty(q.Get("sni"), host), "alpn": []string{"h3"}}
	pinTLS(tls, q)
	hy := map[string]any{"version": 2, "auth": auth}
	if mp := q.Get("mport"); mp != "" {
		hy["udphop"] = map[string]any{"port": mp, "interval": 30}
	}
	return &Server{
		Name: fragmentName(u, host), Protocol: "hysteria", Address: host, Port: port, UDP: true,
		Outbound: map[string]any{
			"protocol": "hysteria",
			"settings": map[string]any{"version": 2, "address": host, "port": port},
			"streamSettings": map[string]any{
				"network": "hysteria", "security": "tls",
				"tlsSettings": tls, "hysteriaSettings": hy,
			},
		},
	}, nil
}

// streamFromQuery собирает streamSettings из параметров ссылки (общий
// формат для vless/trojan/vmess, как у v2rayN и Remnawave).
func streamFromQuery(q url.Values, host string) (map[string]any, bool, error) {
	network := strings.ToLower(firstNonEmpty(q.Get("type"), "tcp"))
	udp := false
	s := map[string]any{}
	switch network {
	case "tcp", "raw":
		network = "raw"
		if q.Get("headerType") == "http" {
			req := map[string]any{"path": []string{firstNonEmpty(q.Get("path"), "/")}}
			if h := q.Get("host"); h != "" {
				req["headers"] = map[string]any{"Host": strings.Split(h, ",")}
			}
			s["rawSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	case "ws":
		s["wsSettings"] = map[string]any{"path": firstNonEmpty(q.Get("path"), "/"), "host": q.Get("host")}
	case "grpc":
		g := map[string]any{"serviceName": q.Get("serviceName"), "multiMode": q.Get("mode") == "multi"}
		if a := q.Get("authority"); a != "" {
			g["authority"] = a
		}
		s["grpcSettings"] = g
	case "xhttp", "splithttp":
		network = "xhttp"
		x := map[string]any{"path": firstNonEmpty(q.Get("path"), "/"), "host": q.Get("host"), "mode": firstNonEmpty(q.Get("mode"), "auto")}
		if extra := q.Get("extra"); extra != "" {
			var e any
			if err := json.Unmarshal([]byte(extra), &e); err == nil {
				x["extra"] = e
			}
		}
		s["xhttpSettings"] = x
	case "httpupgrade":
		s["httpupgradeSettings"] = map[string]any{"path": firstNonEmpty(q.Get("path"), "/"), "host": q.Get("host")}
	case "kcp", "mkcp":
		network = "kcp"
		udp = true
		k := map[string]any{}
		if seed := q.Get("seed"); seed != "" {
			k["seed"] = seed
		}
		if ht := q.Get("headerType"); ht != "" && ht != "none" {
			k["header"] = map[string]any{"type": ht}
		}
		s["kcpSettings"] = k
	default:
		return nil, false, fmt.Errorf("транспорт %s не поддерживается", network)
	}
	s["network"] = network

	alpn := func() []string {
		if a := q.Get("alpn"); a != "" {
			return strings.Split(a, ",")
		}
		return nil
	}
	switch strings.ToLower(q.Get("security")) {
	case "tls":
		t := map[string]any{"serverName": firstNonEmpty(q.Get("sni"), q.Get("host"), host)}
		if fp := q.Get("fp"); fp != "" {
			t["fingerprint"] = fp
		}
		if a := alpn(); a != nil {
			t["alpn"] = a
		}
		pinTLS(t, q)
		s["security"] = "tls"
		s["tlsSettings"] = t
	case "reality":
		r := map[string]any{
			"serverName":  q.Get("sni"),
			"fingerprint": firstNonEmpty(q.Get("fp"), "chrome"),
			"publicKey":   q.Get("pbk"),
			"shortId":     q.Get("sid"),
			"spiderX":     q.Get("spx"),
		}
		if pqv := q.Get("pqv"); pqv != "" {
			r["mldsa65Verify"] = pqv
		}
		if r["publicKey"] == "" {
			return nil, false, errors.New("reality без publicKey")
		}
		s["security"] = "reality"
		s["realitySettings"] = r
	default:
		s["security"] = "none"
	}
	return s, udp, nil
}

// pinTLS переносит закрепление сертификата из ссылки. allowInsecure в этой
// версии xray удалён (конфиг с ним не загрузится), вместо него pcs/vcn.
func pinTLS(t map[string]any, q url.Values) {
	if pcs := q.Get("pcs"); pcs != "" {
		t["pinnedPeerCertSha256"] = pcs
	}
	if vcn := q.Get("vcn"); vcn != "" {
		t["verifyPeerCertByName"] = vcn
	}
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\r", "", "\n", "", " ", "").Replace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("не base64")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func valueOr(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}
