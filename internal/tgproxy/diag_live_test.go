package tgproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// LIVE_TG=1 — диагностика: каждый путь до каждого дата-центра по отдельности
// (WS-фронт, Cloudflare, прямой TCP; обычные соединения и медиа). Ничего не
// роняет, в конце печатает таблицу — её и присылать.
//
//	go test ./internal/tgproxy -run TestLiveDiag -v -count=1
func TestLiveDiag(t *testing.T) {
	if os.Getenv("LIVE_TG") == "" {
		t.Skip("LIVE_TG не задан")
	}
	blockDirect := func(ctx context.Context, n, a string) (net.Conn, error) {
		for _, ip := range dcDirectIPs {
			if hostOnly(a) == ip {
				return nil, errors.New("прямой TCP выключен тестом")
			}
		}
		var d net.Dialer
		return d.DialContext(ctx, n, a)
	}
	paths := []struct {
		name string
		opt  Options
	}{
		{"WS (149.154.167.220)", Options{NoCF: true, Dial: blockDirect}},
		{"Cloudflare", Options{DCRedirects: map[int]string{}, Dial: blockDirect}},
		{"прямой TCP", Options{NoCF: true, DCRedirects: map[int]string{}}},
	}
	dcs := []int{1, 2, 3, 4, 5, -1, -2, -3, -4, -5}

	var mu sync.Mutex
	res := map[string]string{}
	var wg sync.WaitGroup
	for _, pth := range paths {
		o := pth.opt
		o.Listen, o.Secret = "127.0.0.1:0", testSecret()
		p, err := Start(o)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		time.Sleep(2 * time.Second) // пул WS и список доменов Cloudflare
		for _, dc := range dcs {
			wg.Add(1)
			go func(name string, dc int) {
				defer wg.Done()
				d, err := probe(p.Addr(), dc)
				s := fmt.Sprintf("ок %v", d.Round(time.Millisecond))
				if err != nil {
					s = "НЕТ: " + err.Error()
				}
				mu.Lock()
				res[fmt.Sprintf("%-22s %s", name, dcName(dc))] = s
				mu.Unlock()
			}(pth.name, dc)
		}
	}
	wg.Wait()
	keys := make([]string, 0, len(res))
	for k := range res {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s  %s", k, res[k])
	}
}

func dcName(dc int) string {
	if dc < 0 {
		return fmt.Sprintf("DC%d медиа", -dc)
	}
	return fmt.Sprintf("DC%d       ", dc)
}

// probe — первый шаг авторизации MTProto (req_pq_multi) через прокси;
// ответ resPQ с нашим nonce значит, что путь до Telegram живой.
func probe(addr string, dc int) (time.Duration, error) {
	start := time.Now()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	cl := handshake(c, testSecret(), dc, tagAbridged)
	nonce := make([]byte, 16)
	rand.Read(nonce)
	body := binary.LittleEndian.AppendUint32(nil, 0xbe7e8ef1)
	body = append(body, nonce...)
	msg := make([]byte, 8)
	msg = binary.LittleEndian.AppendUint64(msg, uint64(time.Now().Unix())<<32)
	msg = binary.LittleEndian.AppendUint32(msg, uint32(len(body)))
	plain := abridged(append(msg, body...))
	enc := make([]byte, len(plain))
	cl.enc.XORKeyStream(enc, plain)
	if _, err := c.Write(enc); err != nil {
		return 0, err
	}
	_ = c.SetReadDeadline(time.Now().Add(40 * time.Second))
	hdr := make([]byte, 1)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return 0, errors.New("нет ответа")
	}
	cl.dec.XORKeyStream(hdr, hdr)
	resp := make([]byte, int(hdr[0])*4)
	if _, err := io.ReadFull(c, resp); err != nil || len(resp) < 40 {
		return 0, errors.New("ответ оборвался")
	}
	cl.dec.XORKeyStream(resp, resp)
	if binary.LittleEndian.Uint32(resp[20:24]) != 0x05162463 || !bytes.Equal(resp[24:40], nonce) {
		return 0, errors.New("не тот ответ")
	}
	return time.Since(start), nil
}

// LIVE_TG=1 — фронт 149.154.167.220 молча режут (как у многих в РФ), прямой
// TCP тоже: соединения должны быстро уходить на Cloudflare, а не ждать таймаут.
func TestLiveDeadFront(t *testing.T) {
	if os.Getenv("LIVE_TG") == "" {
		t.Skip("LIVE_TG не задан")
	}
	dead := map[string]bool{"149.154.167.220": true}
	for _, ip := range dcDirectIPs {
		dead[ip] = true
	}
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(),
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			if dead[hostOnly(a)] {
				<-ctx.Done() // пакеты молча пропадают
				return nil, ctx.Err()
			}
			var d net.Dialer
			return d.DialContext(ctx, n, a)
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	time.Sleep(12 * time.Second) // прогрев пула успевает провалиться
	for i := 0; i < 3; i++ {
		for _, dc := range []int{2, 4, -2} {
			d, err := probe(p.Addr(), dc)
			if err != nil {
				t.Errorf("%s: %v", dcName(dc), err)
				continue
			}
			t.Logf("%s: %v", dcName(dc), d.Round(time.Millisecond))
			if d > 5*time.Second {
				t.Errorf("%s ждал %v — висит на мёртвом фронте", dcName(dc), d)
			}
		}
	}
}
