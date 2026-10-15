package tgproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// LIVE_TG=1 — настоящий Telegram: через прокси шлём req_pq_multi (первый шаг
// авторизации MTProto, без ключей) и ждём resPQ с нашим nonce.
func TestLiveTelegram(t *testing.T) {
	if os.Getenv("LIVE_TG") == "" {
		t.Skip("LIVE_TG не задан")
	}
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(), NoCF: true,
		Logf: func(f string, a ...any) { t.Logf(f, a...) }})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	for _, dc := range []int{2, 4, -2, 1, 3, 5} {
		cl := dialClient(t, p.Addr(), testSecret(), dc, tagAbridged)
		nonce := make([]byte, 16)
		rand.Read(nonce)
		body := binary.LittleEndian.AppendUint32(nil, 0xbe7e8ef1) // req_pq_multi
		body = append(body, nonce...)
		msg := make([]byte, 8) // auth_key_id = 0
		msg = binary.LittleEndian.AppendUint64(msg, uint64(time.Now().Unix())<<32)
		msg = binary.LittleEndian.AppendUint32(msg, uint32(len(body)))
		msg = append(msg, body...)
		start := time.Now()
		cl.send(t, abridged(msg))

		hdr := cl.recv(t, 1)
		n := int(hdr[0]) * 4
		resp := cl.recv(t, n)
		ctor := binary.LittleEndian.Uint32(resp[20:24])
		if ctor != 0x05162463 || !bytes.Equal(resp[24:40], nonce) {
			t.Errorf("DC%d: не resPQ (ctor %#x)", dc, ctor)
		} else {
			t.Logf("DC%d: resPQ получен за %v", dc, time.Since(start).Round(time.Millisecond))
		}
		cl.c.Close()
	}
	t.Logf("статистика: %+v", p.Stats())
}

// LIVE_TG=1 — путь через Cloudflare: WS-фронт и прямой TCP закрыты.
func TestLiveTelegramViaCF(t *testing.T) {
	if os.Getenv("LIVE_TG") == "" {
		t.Skip("LIVE_TG не задан")
	}
	blocked := map[string]bool{}
	for _, ip := range dcDirectIPs {
		blocked[ip] = true
	}
	p, err := Start(Options{Listen: "127.0.0.1:0", Secret: testSecret(), DCRedirects: map[int]string{},
		Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
			if blocked[hostOnly(a)] {
				return nil, errors.New("заблокировано тестом")
			}
			return defaultDial(ctx, n, a)
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	time.Sleep(2 * time.Second) // свежий список доменов
	cl := dialClient(t, p.Addr(), testSecret(), 2, tagAbridged)
	defer cl.c.Close()
	nonce := make([]byte, 16)
	rand.Read(nonce)
	body := binary.LittleEndian.AppendUint32(nil, 0xbe7e8ef1)
	body = append(body, nonce...)
	msg := make([]byte, 8)
	msg = binary.LittleEndian.AppendUint64(msg, uint64(time.Now().Unix())<<32)
	msg = binary.LittleEndian.AppendUint32(msg, uint32(len(body)))
	cl.send(t, abridged(append(msg, body...)))
	hdr := cl.recv(t, 1)
	resp := cl.recv(t, int(hdr[0])*4)
	if binary.LittleEndian.Uint32(resp[20:24]) != 0x05162463 || !bytes.Equal(resp[24:40], nonce) {
		t.Fatal("через Cloudflare resPQ не пришёл")
	}
	t.Logf("через Cloudflare: %+v, домены: %d", p.Stats(), len(p.CFDomains()))
}
