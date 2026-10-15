package vpn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/infra/conf/serial"
)

// SubInfo — сведения о подписке из заголовков ответа панели.
type SubInfo struct {
	Title      string `json:"title"`
	Upload     int64  `json:"upload"`
	Download   int64  `json:"download"`
	Total      int64  `json:"total"`  // 0 — безлимит
	Expire     int64  `json:"expire"` // unix-время, 0 — бессрочно
	SupportURL string `json:"supportUrl"`
	WebPage    string `json:"webPage"`
	Announce   string `json:"announce"`
	UpdateHrs  int    `json:"updateHours"`
	FetchedAt  int64  `json:"fetchedAt"`
}

// FetchSubscription скачивает и разбирает подписку.
//
// User-Agent v2rayN: панели (Remnawave, Marzban, 3x-ui) отдают на него
// обычный список ссылок, а иногда xray-JSON — разбираем оба варианта.
func FetchSubscription(ctx context.Context, subURL string) ([]*Server, SubInfo, error) {
	subURL = strings.TrimSpace(subURL)
	var info SubInfo
	if !strings.HasPrefix(subURL, "http://") && !strings.HasPrefix(subURL, "https://") {
		// Вставили не ссылку на подписку, а сами ссылки на серверы.
		servers, err := parseBody([]byte(subURL))
		info.FetchedAt = time.Now().Unix()
		return servers, info, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", subURL, nil)
	if err != nil {
		return nil, info, errors.New("неверная ссылка")
	}
	req.Header.Set("User-Agent", "v2rayN/7.15.4")
	req.Header.Set("Accept", "*/*")
	c := &http.Client{Timeout: 25 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		// Текст ошибки *url.Error содержит всю ссылку с токеном доступа —
		// в окно и журнал отдаём только причину.
		var ue *neturl.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, info, fmt.Errorf("подписка недоступна (%s): %v", subHost(subURL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, info, fmt.Errorf("подписка: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, info, err
	}
	info = parseHeaders(resp.Header)
	servers, err := parseBody(body)
	return servers, info, err
}

func parseHeaders(h http.Header) SubInfo {
	info := SubInfo{FetchedAt: time.Now().Unix()}
	info.Title = clipText(decodeMaybeB64(h.Get("Profile-Title")), 60)
	info.Announce = clipText(decodeMaybeB64(h.Get("Announce")), maxInfoText)
	info.SupportURL = safeURL(h.Get("Support-Url"))
	info.WebPage = safeURL(h.Get("Profile-Web-Page-Url"))
	if v, err := strconv.Atoi(strings.TrimSpace(h.Get("Profile-Update-Interval"))); err == nil {
		info.UpdateHrs = v
	}
	// subscription-userinfo: upload=1; download=2; total=3; expire=4
	for _, part := range strings.Split(h.Get("Subscription-Userinfo"), ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch strings.ToLower(k) {
		case "upload":
			info.Upload = n
		case "download":
			info.Download = n
		case "total":
			info.Total = n
		case "expire":
			info.Expire = n
		}
	}
	return info
}

// decodeMaybeB64: значения вида "base64:...." раскодируем.
func decodeMaybeB64(s string) string {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "base64:"); ok {
		if b, err := decodeB64(rest); err == nil {
			return string(b)
		}
	}
	return s
}

func parseBody(body []byte) ([]*Server, error) {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil, errors.New("подписка пустая")
	}
	if text[0] == '[' || text[0] == '{' {
		return parseXrayJSON([]byte(text))
	}
	if !strings.Contains(text, "://") {
		if dec, err := decodeB64(text); err == nil {
			text = string(dec)
		}
	}
	var out []*Server
	var lastErr error
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "://") {
			continue
		}
		s, err := ParseLink(line)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("ни один сервер не подошёл: %v", lastErr)
		}
		return nil, errors.New("в подписке нет серверов")
	}
	return finish(out)
}

// parseXrayJSON разбирает подписку в виде полных конфигов xray (так отдаёт
// Remnawave для части клиентов): берём outbound "proxy" и всё, на что он
// ссылается через dialerProxy (фрагментация, цепочки).
func parseXrayJSON(b []byte) ([]*Server, error) {
	var configs []map[string]any
	if b[0] == '{' {
		var one map[string]any
		if err := json.Unmarshal(b, &one); err != nil {
			return nil, err
		}
		configs = []map[string]any{one}
	} else if err := json.Unmarshal(b, &configs); err != nil {
		return nil, err
	}
	var out []*Server
	for i, cfg := range configs {
		outs, _ := cfg["outbounds"].([]any)
		byTag := map[string]map[string]any{}
		var main map[string]any
		for _, o := range outs {
			ob, ok := o.(map[string]any)
			if !ok {
				continue
			}
			tag, _ := ob["tag"].(string)
			byTag[tag] = ob
			p, _ := ob["protocol"].(string)
			if main == nil && (tag == "proxy" || (p != "freedom" && p != "blackhole" && p != "dns" && p != "loopback")) {
				main = ob
			}
		}
		if main == nil {
			continue
		}
		name, _ := cfg["remarks"].(string)
		s := &Server{Name: firstNonEmpty(name, fmt.Sprintf("Сервер %d", i+1)), Outbound: cloneMap(main)}
		delete(s.Outbound, "tag")
		s.Protocol, _ = main["protocol"].(string)
		s.Address, s.Port = outboundEndpoint(main)
		if ss, ok := main["streamSettings"].(map[string]any); ok {
			n, _ := ss["network"].(string)
			s.UDP = n == "kcp" || n == "hysteria"
		}
		s.Extra = collectDialers(s.Outbound, byTag, 0)
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("в JSON-подписке нет серверов")
	}
	return finish(out)
}

// collectDialers находит outbound'ы из sockopt.dialerProxy и переименовывает
// их теги, чтобы у разных серверов они не пересекались.
func collectDialers(ob map[string]any, byTag map[string]map[string]any, depth int) []map[string]any {
	if depth > 3 {
		return nil
	}
	ss, _ := ob["streamSettings"].(map[string]any)
	so, _ := ss["sockopt"].(map[string]any)
	dp, _ := so["dialerProxy"].(string)
	if dp == "" || byTag[dp] == nil {
		return nil
	}
	d := cloneMap(byTag[dp])
	newTag := fmt.Sprintf("dp-%s-%d", dp, depth)
	d["tag"] = newTag
	so["dialerProxy"] = newTag
	return append([]map[string]any{d}, collectDialers(d, byTag, depth+1)...)
}

func outboundEndpoint(ob map[string]any) (string, int) {
	st, _ := ob["settings"].(map[string]any)
	pick := func(m map[string]any) (string, int) {
		a, _ := m["address"].(string)
		p, _ := m["port"].(float64)
		return a, int(p)
	}
	for _, key := range []string{"vnext", "servers"} {
		if arr, ok := st[key].([]any); ok && len(arr) > 0 {
			if m, ok := arr[0].(map[string]any); ok {
				return pick(m)
			}
		}
	}
	return pick(st)
}

// rejected — серверы, отброшенные при последней загрузке (только для
// журнала). Один негодный сервер в общем конфиге уронил бы весь VPN,
// поэтому их отбрасываем по одному, а не все разом.
var (
	rejectedMu sync.Mutex
	rejected   []string
)

func setRejected(r []string) {
	rejectedMu.Lock()
	rejected = r
	rejectedMu.Unlock()
}

func lastRejected() []string {
	rejectedMu.Lock()
	defer rejectedMu.Unlock()
	return append([]string(nil), rejected...)
}

// validateServers проверяет каждый сервер ядром xray и возвращает годные.
func validateServers(list []*Server) (ok []*Server, rej []string) {
	for _, s := range list {
		if len(ok) >= maxServers {
			rej = append(rej, fmt.Sprintf("ещё %d серверов — больше %d не берём", len(list)-len(ok)-len(rej), maxServers))
			break
		}
		if !sanitizeServer(s) {
			rej = append(rej, clipText(s.Name, maxNameLen)+": протокол не поддерживается")
			continue
		}
		// Минимальный конфиг из одного сервера, без правил маршрутизации:
		// проверке не нужны базы блокировок (их может ещё не быть на диске).
		outs := []any{withTag(s.Outbound, "proxy")}
		for _, e := range s.Extra {
			outs = append(outs, e)
		}
		js, err := json.Marshal(map[string]any{"outbounds": outs})
		if err == nil {
			_, err = serial.LoadJSONConfig(bytes.NewReader(js))
		}
		if err != nil {
			msg := err.Error()
			if i := strings.LastIndex(msg, "> "); i >= 0 {
				msg = msg[i+2:]
			}
			rej = append(rej, s.Name+": "+clipText(msg, 200))
			continue
		}
		ok = append(ok, s)
	}
	return ok, rej
}

// finalize проверяет серверы и даёт им стабильные ID (чтобы выбор пережил
// обновление подписки).
func finalize(list []*Server) []*Server {
	list, rej := validateServers(list)
	setRejected(rej)
	seen := map[string]int{}
	for _, s := range list {
		// По названию, а не по адресу: провайдеры (Dream VPN и др.) отдают на
		// каждый запрос подписки другой хост того же сервера (балансировка).
		id := s.Protocol + "|" + s.Name
		seen[id]++
		if seen[id] > 1 {
			id += fmt.Sprintf("#%d", seen[id])
		}
		s.ID = id
	}
	return list
}

func cloneMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func finish(list []*Server) ([]*Server, error) {
	out := finalize(list)
	if len(out) == 0 {
		if rej := lastRejected(); len(rej) > 0 {
			return nil, fmt.Errorf("ни один сервер не подходит: %s", rej[0])
		}
		return nil, errors.New("в подписке нет серверов")
	}
	return out, nil
}

func withTag(ob map[string]any, tag string) map[string]any {
	c := cloneMap(ob)
	c["tag"] = tag
	return c
}

// safeURL — только https/http/tg-ссылки разумной длины (их можно открыть из окна).
func safeURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return ""
	}
	u, err := neturl.Parse(s)
	if err != nil || u.Host == "" && u.Scheme != "tg" {
		return ""
	}
	switch u.Scheme {
	case "https", "http", "tg":
		return u.String()
	}
	return ""
}
