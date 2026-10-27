package tgproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- «Telegram Desktop» ----------

type fakeClient struct {
	c        net.Conn
	enc, dec interface{ XORKeyStream(dst, src []byte) }
}

// dialClient делает то же, что Telegram Desktop с секретом "dd…".
func dialClient(t *testing.T, addr string, secret []byte, dcIdx int, tag [4]byte) *fakeClient {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return handshake(c, secret, dcIdx, tag)
}

// handshake — init-пакет obfuscated2 от имени Telegram-клиента.
func handshake(c net.Conn, secret []byte, dcIdx int, tag [4]byte) *fakeClient {
	rnd := make([]byte, 64)
	for {
		rand.Read(rnd)
		if rnd[0] != 0xef && !bytes.Equal(rnd[4:8], []byte{0, 0, 0, 0}) {
			break
		}
	}
	k := sha256.Sum256(append(append([]byte{}, rnd[8:40]...), secret...))
	enc := newCTR(k[:], rnd[40:56])
	encd := make([]byte, 64)
	enc.XORKeyStream(encd, rnd)
	tail := make([]byte, 8)
	copy(tail, tag[:])
	binary.LittleEndian.PutUint16(tail[4:], uint16(int16(dcIdx)))
	init := append([]byte{}, rnd...)
	for i := 0; i < 8; i++ {
		init[56+i] = tail[i] ^ (encd[56+i] ^ rnd[56+i])
	}
	rev := reverse(rnd[8:56])
	dk := sha256.Sum256(append(append([]byte{}, rev[:32]...), secret...))
	_, _ = c.Write(init) // ошибку увидит чтение ответа
	return &fakeClient{c: c, enc: enc, dec: newCTR(dk[:], rev[32:])}
}

func (f *fakeClient) send(t *testing.T, plain []byte) {
	b := make([]byte, len(plain))
	f.enc.XORKeyStream(b, plain)
	if _, err := f.c.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeClient) recv(t *testing.T, n int) []byte {
	_ = f.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, n)
	if _, err := io.ReadFull(f.c, b); err != nil {
		t.Fatalf("клиент не дождался ответа: %v", err)
	}
	f.dec.XORKeyStream(b, b)
	return b
}

// ---------- «Telegram» ----------

type tgSession struct {
	dc       int
	tag      [4]byte
	frames   int
	dec, enc interface{ XORKeyStream(dst, src []byte) }
}

// parseRelay — сторона Telegram: obfuscated2 без секрета.
func parseRelay(init []byte) *tgSession {
	key, iv := init[8:40], init[40:56]
	dec := newCTR(key, iv)
	plain := make([]byte, 64)
	dec.XORKeyStream(plain, init)
	rev := reverse(init[8:56])
	s := &tgSession{dec: dec, enc: newCTR(rev[:32], rev[32:])}
	copy(s.tag[:], plain[56:60])
	s.dc = int(int16(binary.LittleEndian.Uint16(plain[60:62])))
	return s
}

// echo: на каждый абриджед-пакет [len][payload] отвечает пакетом "OK:"+payload.
func abridged(payload []byte) []byte { return append([]byte{byte(len(payload) / 4)}, payload...) }

func selfSigned(t *testing.T) tls.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		DNSNames: []string{"*.web.telegram.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

type fakeTG struct {
	ln       net.Listener
	mu       sync.Mutex
	sessions []*tgSession
	hosts    []string
}

func startFakeWSTelegram(t *testing.T) *fakeTG {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeTG{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeTG) serve(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.hosts = append(f.hosts, req.Host+req.URL.Path)
	f.mu.Unlock()
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return
	}
	io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Protocol: binary\r\n\r\n")
	srv := &wsConn{c: c, r: br}
	readFrame := func() ([]byte, error) { return srv.recv() }
	init, err := readFrame()
	if err != nil || len(init) != 64 {
		return
	}
	s := parseRelay(init)
	f.mu.Lock()
	f.sessions = append(f.sessions, s)
	f.mu.Unlock()
	for {
		fr, err := readFrame()
		if err != nil {
			return
		}
		s.frames++
		plain := make([]byte, len(fr))
		s.dec.XORKeyStream(plain, fr)
		reply := abridged(append([]byte("OK:."), plain[1:]...)) // payload кратен 4
		out := make([]byte, len(reply))
		s.enc.XORKeyStream(out, reply)
		// сервер шлёт кадры без маски
		hdr := []byte{0x82, byte(len(out))}
		c.Write(append(hdr, out...))
	}
}

func testSecret() []byte { return []byte("0123456789abcdef") }

func TestProxyViaWebSocket(t *testing.T) {
	tg := startFakeWSTelegram(t)
	tgAddr := tg.ln.Addr().String()
	p, err := Start(Options{
		Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true,
		DCRedirects: map[int]string{2: "149.154.167.220"},
		// любой адрес → наш фейковый Telegram
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, tgAddr)
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	cl := dialClient(t, p.Addr(), testSecret(), 2, tagAbridged)
	defer cl.c.Close()
	// Два пакета одной записью: прокси должен разрезать их на два WS-кадра.
	pkt1 := abridged([]byte("ping0001"))
	pkt2 := abridged([]byte("hello-tg1234"))
	cl.send(t, append(append([]byte{}, pkt1...), pkt2...))

	r1 := cl.recv(t, 1+12)
	r2 := cl.recv(t, 1+16)
	if string(r1[1:]) != "OK:.ping0001" || string(r2[1:]) != "OK:.hello-tg1234" {
		t.Fatalf("неверный ответ через прокси: %q / %q", r1, r2)
	}
	tg.mu.Lock()
	defer tg.mu.Unlock()
	var s *tgSession
	for _, x := range tg.sessions {
		if x.frames > 0 {
			s = x
		}
	}
	if s == nil || s.dc != 2 || s.tag != tagAbridged || s.frames != 2 {
		t.Fatalf("Telegram увидел неверную сессию: %+v", s)
	}
	found := false
	for _, h := range tg.hosts {
		if h == "kws2.web.telegram.org/apiws" {
			found = true
		}
	}
	if !found {
		t.Fatalf("WS шёл не на kws2.web.telegram.org/apiws: %v", tg.hosts)
	}
	if st := p.Stats(); st.ViaWS != 1 || st.BadHello != 0 {
		t.Fatalf("статистика: %+v", st)
	}
}

func TestProxyRejectsWrongSecret(t *testing.T) {
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true, DCRedirects: map[int]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cl := dialClient(t, p.Addr(), []byte("wrong-secret-123"), 2, tagAbridged)
	defer cl.c.Close()
	_ = cl.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := cl.c.Read(make([]byte, 1)); err == nil {
		t.Fatal("соединение с чужим секретом должно закрываться")
	}
	if p.Stats().BadHello != 1 {
		t.Fatalf("статистика: %+v", p.Stats())
	}
}

// Без WS (DC без фронта) и без Cloudflare — прямой TCP до DC.
func TestProxyTCPFallback(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	got := make(chan *tgSession, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		init := make([]byte, 64)
		io.ReadFull(c, init)
		s := parseRelay(init)
		buf := make([]byte, 9)
		io.ReadFull(c, buf)
		s.dec.XORKeyStream(buf, buf)
		reply := abridged(append([]byte("OK:."), buf[1:]...))
		s.enc.XORKeyStream(reply, reply)
		c.Write(reply)
		got <- s
		time.Sleep(time.Second)
	}()
	p, err := Start(Options{
		Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true, DCRedirects: map[int]string{},
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, ln.Addr().String())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cl := dialClient(t, p.Addr(), testSecret(), -3, tagAbridged) // DC3, media
	defer cl.c.Close()
	cl.send(t, abridged([]byte("tcp-path")))
	r := cl.recv(t, 13)
	s := <-got
	if string(r[1:]) != "OK:.tcp-path" || s.dc != -3 {
		t.Fatalf("TCP-фолбэк: ответ %q, dc=%d", r, s.dc)
	}
}

func TestDecodeCFDomains(t *testing.T) {
	for _, e := range cfEncoded {
		d := decodeCF(e)
		if !strings.HasSuffix(d, ".co.uk") || !validDomain(d) {
			t.Errorf("%s → %s", e, d)
		}
	}
}

func TestSplitterIntermediate(t *testing.T) {
	sp := &splitter{tag: tagIntermediate}
	pkt := func(p string) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(len(p)))
		return append(b, p...)
	}
	all := append(pkt("abcd"), pkt("12345678")...)
	// приходит кусками: 3 байта, потом остальное
	parts := sp.split(all[:3], all[:3])
	parts = append(parts, sp.split(all[3:], all[3:])...)
	if len(parts) != 2 || len(parts[0]) != 8 || len(parts[1]) != 12 {
		t.Fatalf("нарезка: %v", parts)
	}
}

// Фронт, который уже не ответил, не держит соединения Telegram: пул сразу
// отдаёт nil, и прокси идёт запасным путём, а не ждёт таймаут на каждом.
func TestPoolSkipsFailingFront(t *testing.T) {
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true,
		DCRedirects: map[int]string{2: "192.0.2.1"},
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			<-ctx.Done() // фронт режут: соединение висит до таймаута
			return nil, ctx.Err()
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	key := poolKey{dc: 2}
	p.pool.fail(key)
	start := time.Now()
	if ws := p.pool.get(key); ws != nil {
		t.Fatal("мёртвый фронт отдал соединение")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("пул ждал мёртвый фронт %v", d)
	}
}
