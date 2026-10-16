package vpn

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Пинг — это реальный запрос ЧЕРЕЗ сервер (как «реальная задержка» в
// v2rayN), а не TCP-подключение к его порту: мёртвый сервер может принимать
// TCP и при этом ничего не пропускать.
// Не Google: серверы «белых списков» пропускают Google, но не остальной
// интернет, и замер через gstatic принимал их за живые.
// probeURL — переменная только ради тестов (локальная страница вместо
// Cloudflare).
var probeURL = "https://www.cloudflare.com/cdn-cgi/trace"

// PingAll проверяет все серверы одним временным экземпляром xray: у каждого
// сервера свой локальный порт. Результат — мс, -1 если сервер не пропускает
// трафик.
func PingAll(ctx context.Context, servers []*Server) map[string]int { return pingAll(ctx, servers, "") }

// pingAll: iface — через какую сетевую карту ходить (в режиме TUN, чтобы
// замеры не шли через наш же туннель); пусто — как обычно.
func pingAll(ctx context.Context, servers []*Server, iface string) map[string]int {
	out := make(map[string]int, len(servers))
	if len(servers) == 0 {
		return out
	}
	ports, err := freePorts(len(servers))
	if err != nil {
		return out
	}
	js, err := buildProbeConfig(servers, ports, iface)
	if err != nil {
		return out
	}
	e := &engine{}
	if err := e.start(js); err != nil {
		for _, s := range servers {
			out[s.ID] = -1
		}
		reclaimMainLog()
		return out
	}
	defer func() {
		e.stop()
		// Логгер у xray общий на процесс: временный экземпляр забрал его себе
		// и, закрывшись, заглушил бы журнал основного подключения.
		reclaimMainLog()
	}()

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, s := range servers {
		wg.Add(1)
		go func(i int, s *Server) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ms := probe(ctx, ports[i])
			mu.Lock()
			out[s.ID] = ms
			mu.Unlock()
		}(i, s)
	}
	wg.Wait()
	return out
}

// probe делает два запроса: первый прогревает соединение (TLS/REALITY
// рукопожатие), второй измеряет задержку как её почувствует пользователь.
func probe(ctx context.Context, port int) int {
	pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	cl := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	best := -1
	for i := 0; i < 2; i++ {
		start := time.Now()
		req, _ := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
		resp, err := cl.Do(req)
		if err != nil {
			return best
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 204 && resp.StatusCode != 200 {
			return best
		}
		best = max(1, int(time.Since(start).Milliseconds()))
	}
	return best
}

// buildProbeConfig: вход 127.0.0.1:ports[i] → строго через сервер i.
func buildProbeConfig(servers []*Server, ports []int, iface string) ([]byte, error) {
	o := BuildOptions{Servers: servers, Mode: ModeProxy, HTTPPort: 1, SocksPort: 2, LogFile: xrayLog()}
	if iface != "" {
		// sockopt.interface на всех outbound'ах; inbound TUN rewireForProbe уберёт.
		o.Mode, o.PhysIface = ModeTUN, iface
	}
	cfg, err := BuildConfig(o)
	if err != nil {
		return nil, err
	}
	return rewireForProbe(cfg, ports)
}

// freePorts — свободные локальные порты от системы (не случайный диапазон:
// его могут занять Hyper-V/WSL/Docker).
func freePorts(n int) ([]int, error) {
	var lns []net.Listener
	defer func() {
		for _, l := range lns {
			l.Close()
		}
	}()
	var ports []int
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		lns = append(lns, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

// portFree — порт можно занять на 127.0.0.1.
func portFree(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}
