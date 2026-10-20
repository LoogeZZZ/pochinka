package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Защищённый DNS (DNS-over-HTTPS) — по IP, без системного DNS: он-то и
// может быть сломан. Сертификаты этих серверов выписаны и на IP-адреса.
var dohServers = []string{
	"https://8.8.8.8/resolve",   // Google, JSON API
	"https://1.1.1.1/dns-query", // Cloudflare, JSON API
}

var dohClient = &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: nil}}

// ResolveDoH — IPv4-адрес имени через защищённый DNS.
func ResolveDoH(ctx context.Context, host string) (net.IP, error) {
	var last error
	for _, srv := range dohServers {
		ip, err := dohQuery(ctx, srv, host)
		if err == nil {
			return ip, nil
		}
		last = err
	}
	return nil, last
}

func dohQuery(ctx context.Context, srv, host string) (net.IP, error) {
	q := url.Values{"name": {host}, "type": {"A"}}
	req, _ := http.NewRequestWithContext(ctx, "GET", srv+"?"+q.Encode(), nil)
	req.Header.Set("Accept", "application/dns-json")
	resp, err := dohClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New(resp.Status)
	}
	var body struct {
		Status int
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return nil, err
	}
	for _, a := range body.Answer {
		if a.Type == 1 { // A
			if ip := net.ParseIP(a.Data).To4(); ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified() {
				return ip, nil
			}
		}
	}
	return nil, errors.New("нет адреса")
}
