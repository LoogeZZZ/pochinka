package tgproxy

import (
	"context"
	"crypto/tls"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// settle ждёт, пока число горутин опустится до want (или истечёт время).
func settle(want int, d time.Duration) int {
	deadline := time.Now().Add(d)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
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

// Тысячи сессий (нормальных, с чужим секретом, оборванных на полуслове) не
// должны оставлять ни горутин, ни соединений, ни памяти.
func TestNoLeakManySessions(t *testing.T) {
	if testing.Short() {
		t.Skip("долгий")
	}
	tg := startFakeWSTelegram(t)
	tgAddr := tg.ln.Addr().String()
	p, err := Start(Options{
		Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true,
		DCRedirects: map[int]string{2: "149.154.167.220"},
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, tgAddr)
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	round := func(n int) {
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				switch i % 4 {
				case 0, 1: // обычная сессия
					cl := dialClient(t, p.Addr(), testSecret(), 2, tagAbridged)
					cl.send(t, abridged([]byte("ping0001")))
					cl.recv(t, 13)
					cl.c.Close()
				case 2: // чужой секрет
					cl := dialClient(t, p.Addr(), []byte("wrong-secret-123"), 2, tagAbridged)
					cl.c.Close()
				case 3: // рукопожатие, данные и обрыв без ожидания ответа
					cl := dialClient(t, p.Addr(), testSecret(), 2, tagAbridged)
					cl.send(t, abridged([]byte("half0001")))
					cl.c.Close()
				}
			}(i)
		}
		wg.Wait()
		// фейковый Telegram запоминает сессии — это память теста, не прокси
		tg.mu.Lock()
		tg.sessions, tg.hosts = nil, nil
		tg.mu.Unlock()
	}

	round(200) // прогрев: пул WS, TLS-кеши
	base := settle(0, 3*time.Second)
	heap0 := heapMB()
	for r := 0; r < 30; r++ {
		round(200)
	}
	g := settle(base, 10*time.Second)
	heap1 := heapMB()
	st := p.Stats()
	t.Logf("сессий %d, горутин до/после: %d/%d, куча: %.1f → %.1f МБ, активных: %d",
		st.Total, base, g, heap0, heap1, st.Active)
	if st.Active != 0 {
		t.Errorf("после закрытия всех клиентов активных соединений %d", st.Active)
	}
	if g > base+5 {
		t.Errorf("горутины не вернулись: было %d, стало %d", base, g)
	}
	if heap1 > heap0+8 {
		t.Errorf("куча выросла: %.1f → %.1f МБ", heap0, heap1)
	}

	// Close должен погасить вообще всё, включая пул WS.
	before := settle(0, 0)
	p.Close()
	after := settle(0, 3*time.Second)
	t.Logf("горутин до/после Close: %d/%d", before, after)
}

// Клиент подключился и молчит: через 10 с прокси обязан его отпустить, иначе
// 512 таких соединений заняли бы все места.
func TestSilentClientReleased(t *testing.T) {
	if testing.Short() {
		t.Skip("долгий")
	}
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true, DCRedirects: map[int]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	start := time.Now()
	_, err = c.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("молчащий клиент получил данные")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("прокси не закрыл молчащее соединение за 15 с")
	}
	t.Logf("молчащий клиент отключён через %v", time.Since(start).Round(time.Second))
}
