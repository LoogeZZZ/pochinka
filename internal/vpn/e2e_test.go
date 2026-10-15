package vpn

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
)

// Сквозная проверка маршрутизации: локальный vless-«сервер» пишет журнал
// доступа, клиент собран нашим BuildConfig. Через сервер должны пройти только
// заблокированные сайты и «свои» сайты пользователя.
func TestRoutingEndToEnd(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	dir := t.TempDir()
	const uuid = "11111111-2222-3333-4444-555555555555"

	seen := startVlessSniffer(t, "127.0.0.1:39443")

	s, err := ParseLink("vless://" + uuid + "@127.0.0.1:39443?type=tcp&security=none#local")
	if err != nil {
		t.Fatal(err)
	}
	client, err := BuildConfig(BuildOptions{
		Servers: []*Server{s}, Mode: ModeProxy, HTTPPort: 39010, SocksPort: 39011,
		LogFile:       filepath.Join(dir, "client.log"),
		ZapretDomains: DomainRules("discord.com\n"),
		UserDomains:   DomainRules("my-own-site-pochinka.com\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, client)

	for _, host := range []string{"instagram.com", "example.com", "gosuslugi.ru", "discord.com", "my-own-site-pochinka.com", "x.com", "api.anthropic.com"} {
		connect(t, "127.0.0.1:39010", host+":443")
	}
	time.Sleep(500 * time.Millisecond)
	log := seen()
	t.Logf("журнал сервера:\n%s", log)

	mustVPN := []string{"instagram.com", "my-own-site-pochinka.com", "x.com", "api.anthropic.com"}
	mustDirect := []string{"example.com", "gosuslugi.ru", "discord.com"}
	for _, h := range mustVPN {
		if !strings.Contains(log, h+":443") {
			t.Errorf("%s должен идти через VPN", h)
		}
	}
	for _, h := range mustDirect {
		if strings.Contains(log, h+":443") {
			t.Errorf("%s должен идти напрямую, а ушёл в VPN", h)
		}
	}
}

func startXray(t *testing.T, js []byte) {
	t.Helper()
	cfg, err := serial.LoadJSONConfig(bytes.NewReader(js))
	if err != nil {
		t.Fatal(err)
	}
	inst, err := core.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
}

// connect шлёт CONNECT через HTTP-прокси и ждёт ответа (или закрытия).
func connect(t *testing.T, proxy, target string) {
	c, err := net.DialTimeout("tcp", proxy, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _ = bufio.NewReader(c).ReadString('\n')
	// Отправим немного данных, чтобы соединение точно дошло до outbound.
	_, _ = c.Write([]byte("\x16\x03\x01\x00\x05hello"))
	time.Sleep(200 * time.Millisecond)
}

// startVlessSniffer — «сервер», который только читает заголовок VLESS и
// запоминает адрес назначения (домен:порт). Второй xray в том же процессе
// не годится: логгер у xray глобальный и конфиг клиента перетрёт журнал.
func startVlessSniffer(t *testing.T, addr string) func() string {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var got []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
				r := bufio.NewReader(c)
				// version(1) uuid(16) addonsLen(1) addons cmd(1) port(2) atyp(1) addr
				h := make([]byte, 18)
				if _, err := io.ReadFull(r, h); err != nil {
					return
				}
				if _, err := io.ReadFull(r, make([]byte, int(h[17]))); err != nil {
					return
				}
				p := make([]byte, 4)
				if _, err := io.ReadFull(r, p); err != nil {
					return
				}
				port := int(p[1])<<8 | int(p[2])
				var host string
				switch p[3] {
				case 2: // домен
					l, _ := r.ReadByte()
					b := make([]byte, l)
					io.ReadFull(r, b)
					host = string(b)
				case 1:
					b := make([]byte, 4)
					io.ReadFull(r, b)
					host = net.IP(b).String()
				}
				mu.Lock()
				got = append(got, fmt.Sprintf("%s:%d", host, port))
				mu.Unlock()
			}(c)
		}
	}()
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(got, "\n")
	}
}

// Режим «всё, кроме российского»: зарубежное (в т.ч. панели на
// DigitalOcean, которые ТСПУ замедляет) — через VPN, российское — напрямую.
func TestRoutingAllExceptRU(t *testing.T) {
	geo := os.Getenv("GEO_TEST_DIR")
	if geo == "" {
		t.Skip("GEO_TEST_DIR не задан")
	}
	SetAssetDir(geo)
	const uuid = "11111111-2222-3333-4444-555555555555"
	seen := startVlessSniffer(t, "127.0.0.1:39445")
	s, _ := ParseLink("vless://" + uuid + "@127.0.0.1:39445?type=tcp&security=none#local")
	client, err := BuildConfig(BuildOptions{
		Servers: []*Server{s}, Mode: ModeProxy, HTTPPort: 39030, SocksPort: 39031,
		LogFile: filepath.Join(t.TempDir(), "c.log"), RouteAll: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	startXray(t, client)
	for _, h := range []string{"youtube.com", "discord.com", "instagram.com", "ya.ru", "gosuslugi.ru", "vk.com", "sberbank.ru"} {
		connect(t, "127.0.0.1:39030", h+":443")
	}
	time.Sleep(500 * time.Millisecond)
	log := seen()
	t.Logf("через VPN:\n%s", log)
	for _, h := range []string{"youtube.com", "discord.com", "instagram.com"} {
		if !strings.Contains(log, h+":443") {
			t.Errorf("%s должен идти через VPN", h)
		}
	}
	for _, h := range []string{"ya.ru", "gosuslugi.ru", "vk.com", "sberbank.ru"} {
		if strings.Contains(log, h+":443") {
			t.Errorf("%s должен идти напрямую", h)
		}
	}
}
