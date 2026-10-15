package vpn

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"pochinka/internal/applog"
)

// Проверочный сайт из базы блокировок: открывается только через VPN.
const checkURL = "https://www.instagram.com/"

// SelfCheck проверяет VPN целиком, как его видит пользователь:
// 1) заблокированный сайт открывается через наш локальный вход;
// 2) системный прокси указывает на нас (а не на Happ или расширение);
// 3) интернет не перехватывает другой VPN.
func (m *Manager) SelfCheck(ctx context.Context) {
	m.mu.Lock()
	running := m.state.Status == StatusRunning
	mode := m.state.Mode
	m.mu.Unlock()
	if !running {
		return
	}
	m.set(func(s *State) {
		s.Check, s.CheckOK = "Проверяю, открывается ли Instagram через VPN…", false
	})

	port := cfgHTTPPort()
	pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	cl := &http.Client{
		Timeout:       12 * time.Second,
		Transport:     &http.Transport{Proxy: http.ProxyURL(pu)},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	resp, err := cl.Do(req)
	var msg string
	ok := false
	switch {
	case err != nil:
		msg = "Сервер не пропускает трафик: Instagram не открылся. Выберите другой сервер или «Авто»."
	default:
		resp.Body.Close()
		ms := time.Since(start).Milliseconds()
		ok = true
		msg = fmt.Sprintf("Instagram открывается через VPN · %d мс", ms)
		if foreign := ForeignVPN(); foreign != "" && mode == ModeProxy {
			ok = false
			msg = "VPN работает, но интернет перехватывает «" + foreign + "». Выключите его."
		} else if mode == ModeProxy && !systemProxyIsOurs(port) {
			ok = false
			msg = "VPN работает, но системный прокси перехватила другая программа — браузер идёт мимо VPN."
		} else if h := BrowserProxyHijackers(); mode == ModeProxy && len(h) > 0 {
			ok = false
			msg = "VPN работает, но браузер идёт мимо него: прокси браузера перехватывает «" + strings.Join(h, "», «") +
				"». Отключите это расширение — или включите режим TUN, ему расширения не мешают."
		}
	}
	applog.Printf("vpn: самопроверка — %s", msg)
	m.set(func(s *State) { s.Check, s.CheckOK = msg, ok })
}

func systemProxyIsOurs(port int) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	en, _, _ := k.GetIntegerValue("ProxyEnable")
	srv, _, _ := k.GetStringValue("ProxyServer")
	auto, _, _ := k.GetStringValue("AutoConfigURL")
	return en == 1 && srv == fmt.Sprintf("127.0.0.1:%d", port) && auto == ""
}
