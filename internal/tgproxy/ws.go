package tgproxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// wsConn — минимальный клиент WebSocket (RFC 6455), только бинарные кадры.
type wsConn struct {
	c       net.Conn
	r       *bufio.Reader
	wmu     sync.Mutex
	closed  bool
	created time.Time
}

const maxWSMessage = 16 << 20

// dialFunc — как устанавливать TCP; в тестах подменяется.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

var defaultDial dialFunc = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext

// wsDial: TCP до addr (IP:443), TLS с SNI = sni (пусто — host), HTTP Upgrade
// с Host = host. insecure — не проверять сертификат (только для фронтинга,
// где SNI намеренно чужой).
func wsDial(ctx context.Context, dial dialFunc, addr, host, sni, path string, secure, insecure bool, tlsCfg *tls.Config) (*wsConn, error) {
	raw, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	conn := raw
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	_ = raw.SetDeadline(deadline)
	if secure {
		if sni == "" {
			sni = host
		}
		cfg := &tls.Config{ServerName: sni, InsecureSkipVerify: insecure, NextProtos: []string{"http/1.1"}, MinVersion: tls.VersionTLS12}
		if tlsCfg != nil {
			cfg = tlsCfg.Clone()
			cfg.ServerName = sni
			if insecure {
				cfg.InsecureSkipVerify = true
			}
		}
		tc := tls.Client(raw, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		conn = tc
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: binary\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("websocket: HTTP %d", resp.StatusCode)
	}
	_ = raw.SetDeadline(time.Time{})
	return &wsConn{c: conn, r: br, created: time.Now()}, nil
}

func (w *wsConn) frame(op byte, data []byte) []byte {
	n := len(data)
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 65536:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	hdr = append(hdr, mask[:]...)
	out := make([]byte, len(hdr)+n)
	copy(out, hdr)
	for i := 0; i < n; i++ {
		out[len(hdr)+i] = data[i] ^ mask[i&3]
	}
	return out
}

// send шлёт части отдельными кадрами одной записью.
func (w *wsConn) send(parts ...[]byte) error {
	var buf []byte
	for _, p := range parts {
		buf = append(buf, w.frame(0x2, p)...)
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if w.closed {
		return net.ErrClosed
	}
	_, err := w.c.Write(buf)
	return err
}

var errWSClosed = errors.New("websocket закрыт сервером")

// recv возвращает следующее бинарное сообщение (собирая фрагменты).
func (w *wsConn) recv() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxWSMessage {
			return nil, fmt.Errorf("websocket: кадр %d байт — слишком большой", n)
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(w.r, mask); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range payload {
				payload[i] ^= mask[i&3]
			}
		}
		switch op {
		case 0x8: // close
			w.wmu.Lock()
			if !w.closed {
				_, _ = w.c.Write(w.frame(0x8, payload[:min(2, len(payload))]))
				w.closed = true
			}
			w.wmu.Unlock()
			return nil, errWSClosed
		case 0x9: // ping
			w.wmu.Lock()
			if !w.closed {
				_, _ = w.c.Write(w.frame(0xA, payload))
			}
			w.wmu.Unlock()
			continue
		case 0xA: // pong
			continue
		case 0x0, 0x1, 0x2:
			msg = append(msg, payload...)
			if len(msg) > maxWSMessage {
				return nil, errors.New("websocket: сообщение слишком большое")
			}
			if fin {
				return msg, nil
			}
		}
	}
}

func (w *wsConn) close() {
	w.wmu.Lock()
	if !w.closed {
		w.closed = true
		_ = w.c.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = w.c.Write(w.frame(0x8, nil))
	}
	w.wmu.Unlock()
	_ = w.c.Close()
}

func (w *wsConn) isClosed() bool {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.closed
}

// alive — соединение из пула ещё живо: сервер не закрыл его, пока оно
// лежало без дела (иначе первый же пакет клиента ушёл бы в пустоту).
func (w *wsConn) alive() bool {
	if w.isClosed() {
		return false
	}
	_ = w.c.SetReadDeadline(time.Now().Add(time.Millisecond))
	_, err := w.r.Peek(1)
	_ = w.c.SetReadDeadline(time.Time{})
	var ne net.Error
	return err != nil && errors.As(err, &ne) && ne.Timeout()
}

func hostOnly(s string) string {
	if i := strings.LastIndex(s, ":"); i > 0 {
		return s[:i]
	}
	return s
}
