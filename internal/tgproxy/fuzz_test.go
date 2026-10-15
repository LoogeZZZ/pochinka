package tgproxy

import (
	"bufio"
	"bytes"
	"net"
	"testing"
)

// Произвольный поток от «Telegram» в WS-клиент: без паники и без
// выделения памяти больше лимита.
func FuzzWSRecv(f *testing.F) {
	f.Add([]byte{0x82, 0x02, 'h', 'i'})
	f.Add([]byte{0x02, 0x01, 'a', 0x80, 0x01, 'b'})                           // фрагменты
	f.Add([]byte{0x82, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // 2^64-1
	f.Add([]byte{0x89, 0x00, 0x88, 0x00})                                     // ping, close
	f.Fuzz(func(t *testing.T, data []byte) {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		go func() { b.Write(data); b.Close() }()
		go func() {
			buf := make([]byte, 1024)
			for {
				if _, err := b.Read(buf); err != nil {
					return
				}
			}
		}()
		w := &wsConn{c: a, r: bufio.NewReader(a)}
		for i := 0; i < 64; i++ {
			if _, err := w.recv(); err != nil {
				return
			}
		}
	})
}

func FuzzSplitter(f *testing.F) {
	f.Add([]byte{0x02, 1, 2, 3, 4, 5, 6, 7, 8}, byte(0))
	f.Add([]byte{0x7f, 0xff, 0xff, 0xff}, byte(1))
	f.Add([]byte{4, 0, 0, 0, 1, 2, 3, 4}, byte(2))
	f.Fuzz(func(t *testing.T, data []byte, kind byte) {
		tags := [][4]byte{tagAbridged, tagIntermediate, tagSecure, {1, 2, 3, 4}}
		sp := &splitter{tag: tags[int(kind)%len(tags)]}
		var out [][]byte
		for i := 0; i < len(data); i += 7 {
			end := min(i+7, len(data))
			out = append(out, sp.split(data[i:end], data[i:end])...)
		}
		if tail := sp.flush(); tail != nil {
			out = append(out, tail)
		}
		// Ни один байт не теряется и не дублируется.
		if got := bytes.Join(out, nil); !bytes.Equal(got, data) {
			t.Fatalf("нарезка исказила поток: %x → %x", data, got)
		}
	})
}

func FuzzClientHello(f *testing.F) {
	f.Add(make([]byte, 64), []byte("0123456789abcdef"))
	f.Fuzz(func(t *testing.T, hs, secret []byte) { _ = parseClientHello(hs, secret) })
}
