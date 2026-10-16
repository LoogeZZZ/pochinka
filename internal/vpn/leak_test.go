package vpn

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func settle(want int, d time.Duration) int {
	deadline := time.Now().Add(d)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
		n = runtime.NumGoroutine()
	}
	return n
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

// Запуск/остановка движка и замеры пинга (каждый — свой временный xray)
// повторяются десятки раз: горутины, соединения и память должны вернуться.
func TestNoLeakEngineAndPing(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" || testing.Short() {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	dir := t.TempDir()
	xrayLog = func() string { return os.DevNull }
	const uuid = "11111111-2222-3333-4444-555555555555"

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer page.Close()
	old := probeURL
	probeURL = page.URL
	defer func() { probeURL = old }()

	startVlessServer(t, 39643, uuid)
	var servers []*Server
	for i := 0; i < 5; i++ {
		s, err := ParseLink(fmt.Sprintf("vless://%s@127.0.0.1:39643?type=tcp&security=none#s%d", uuid, i))
		if err != nil {
			t.Fatal(err)
		}
		s.ID = fmt.Sprintf("vless|s%d", i) // обычно ставит разбор подписки
		servers = append(servers, s)
	}
	client, err := BuildConfig(BuildOptions{
		Servers: servers[:1], Mode: ModeProxy, HTTPPort: 39610, SocksPort: 39611,
		LogFile: filepath.Join(dir, "client.log"),
	})
	if err != nil {
		t.Fatal(err)
	}

	cycle := func() {
		e := &engine{}
		if err := e.start(client); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			connect(t, "127.0.0.1:39610", "example.com:443")
		}
		e.stop()
		res := pingAll(context.Background(), servers, "")
		for _, s := range servers {
			if res[s.ID] <= 0 {
				t.Fatalf("замер не прошёл: %v", res)
			}
		}
	}

	cycle() // прогрев
	base := settle(0, 3*time.Second)
	heap0 := heapMB()
	for i := 0; i < 25; i++ {
		cycle()
	}
	g := settle(base, 15*time.Second)
	heap1 := heapMB()
	t.Logf("25 циклов (движок + 5 соединений + замер 5 серверов): горутин %d → %d, куча %.1f → %.1f МБ", base, g, heap0, heap1)
	if g > base+10 {
		t.Errorf("горутины не вернулись: было %d, стало %d", base, g)
	}
	if heap1 > heap0+20 {
		t.Errorf("куча выросла: %.1f → %.1f МБ", heap0, heap1)
	}
}

// startVlessServer — минимальный VLESS-сервер (без шифрования): читает
// заголовок, соединяется с целью и пробрасывает данные. Второй xray не
// годится: свежие версии не выпускают трафик на 127.0.0.1.
func startVlessServer(t *testing.T, port int, uuid string) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				h := make([]byte, 18) // version, uuid(16), addonsLen
				if _, err := io.ReadFull(r, h); err != nil {
					return
				}
				if _, err := io.ReadFull(r, make([]byte, int(h[17]))); err != nil {
					return
				}
				p := make([]byte, 4) // cmd, port(2), atyp
				if _, err := io.ReadFull(r, p); err != nil {
					return
				}
				var host string
				switch p[3] {
				case 1:
					b := make([]byte, 4)
					io.ReadFull(r, b)
					host = net.IP(b).String()
				case 2:
					l, _ := r.ReadByte()
					b := make([]byte, l)
					io.ReadFull(r, b)
					host = string(b)
				default:
					return
				}
				dst, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(int(p[1])<<8|int(p[2]))))
				if err != nil {
					return
				}
				defer dst.Close()
				c.Write([]byte{0, 0}) // ответ VLESS: version, addonsLen
				go func() { io.Copy(dst, r); dst.Close() }()
				io.Copy(c, dst)
			}(c)
		}
	}()
}

// Трафик через движок: после прокачки гигабайтов куча должна вернуться
// (буферы xray переиспользуются, а не копятся).
func TestNoLeakUnderTraffic(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" || testing.Short() {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	xrayLog = func() string { return os.DevNull }
	const uuid = "11111111-2222-3333-4444-555555555555"
	chunk := make([]byte, 1<<20)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 64; i++ { // 64 МБ на запрос
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer page.Close()
	startVlessServer(t, 39743, uuid)
	s, _ := ParseLink("vless://" + uuid + "@127.0.0.1:39743?type=tcp&security=none#s")
	s.ID = "vless|s"
	ports, _ := freePorts(1)
	js, err := buildProbeConfig([]*Server{s}, ports, "")
	if err != nil {
		t.Fatal(err)
	}
	e := &engine{}
	if err := e.start(js); err != nil {
		t.Fatal(err)
	}
	defer e.stop()
	pu, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", ports[0]))
	tr := &http.Transport{Proxy: http.ProxyURL(pu)}
	cl := &http.Client{Transport: tr, Timeout: time.Minute}
	pump := func(n int) int64 {
		var wg sync.WaitGroup
		var total atomic.Int64
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := cl.Get(page.URL)
				if err != nil {
					t.Error(err)
					return
				}
				k, _ := io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				total.Add(k)
			}()
		}
		wg.Wait()
		return total.Load()
	}
	pump(4)
	tr.CloseIdleConnections()
	base := settle(0, 3*time.Second)
	heap0 := heapMB()
	var sum int64
	for r := 0; r < 4; r++ {
		sum += pump(8) // 8 параллельных потоков по 64 МБ
	}
	tr.CloseIdleConnections()
	g := settle(base, 10*time.Second)
	heap1 := heapMB()
	t.Logf("прокачано %.1f ГБ: горутин %d → %d, куча %.1f → %.1f МБ", float64(sum)/(1<<30), base, g, heap0, heap1)
	if heap1 > heap0+20 {
		t.Errorf("куча выросла: %.1f → %.1f МБ", heap0, heap1)
	}
	if g > base+5 {
		t.Errorf("горутины не вернулись: %d → %d", base, g)
	}
}
