// Package securedns — «Защищённый DNS»: DNS-серверы Google/Cloudflare/
// Quad9 на сетевых картах с шифрованием DoH (встроено в Windows 11).
// Нужно, когда DNS провайдера не отдаёт адрес сайта (YouTube и т.п.):
// zapret это не чинит — он работает уже с соединением.
//
// Прежние настройки каждой карты (вручную заданные серверы или «от
// роутера») запоминаются и возвращаются при выключении — в том числе
// при удалении программы.
package securedns

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"pochinka/internal/paths"
)

// Provider — набор серверов одного сервиса.
type Provider struct {
	ID, Name string
	V4, V6   []string
	DoH      string
}

var Providers = []Provider{
	{ID: "google", Name: "Google", V4: []string{"8.8.8.8", "8.8.4.4"},
		V6: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"}, DoH: "https://dns.google/dns-query"},
	{ID: "cloudflare", Name: "Cloudflare", V4: []string{"1.1.1.1", "1.0.0.1"},
		V6: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}, DoH: "https://cloudflare-dns.com/dns-query"},
	{ID: "quad9", Name: "Quad9", V4: []string{"9.9.9.9", "149.112.112.112"},
		V6: []string{"2620:fe::fe", "2620:fe::9"}, DoH: "https://dns.quad9.net/dns-query"},
}

func Find(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// adapter — сетевая карта, которой меняем DNS.
type adapter struct {
	GUID string `json:"guid"` // {…} — ключ в реестре
	Name string `json:"name"` // для netsh
	// Прежние вручную заданные серверы; пусто — были «от роутера» (DHCP).
	V4 string `json:"v4"`
	V6 string `json:"v6"`
}

func statePath() string { return filepath.Join(paths.DataDir(), "securedns-prev.json") }

// adapters — работающие сетевые карты с выходом в сеть (без виртуальных:
// TUN, VPN, Hyper-V и т.п. не трогаем).
func adapters() ([]adapter, error) {
	size := uint32(16 << 10)
	var buf []byte
	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, err
		}
	}
	var out []adapter
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp || a.FirstGatewayAddress == nil {
			continue
		}
		if a.IfType != windows.IF_TYPE_ETHERNET_CSMACD && a.IfType != windows.IF_TYPE_IEEE80211 {
			continue
		}
		name := windows.UTF16PtrToString(a.FriendlyName)
		descr := strings.ToLower(windows.UTF16PtrToString(a.Description) + " " + name)
		if virtual(descr) {
			continue
		}
		out = append(out, adapter{GUID: windows.BytePtrToString(a.AdapterName), Name: name})
	}
	if len(out) == 0 {
		return nil, errors.New("не найдена сетевая карта с выходом в интернет")
	}
	return out, nil
}

func virtual(s string) bool {
	for _, h := range []string{"vpn", "tap-", "tunnel", "wintun", "wireguard", "virtual", "hamachi", "zerotier",
		"tailscale", "radmin", "happ", "xray", "sing-box", "pochinka", "hyper-v", "vmware", "virtualbox", "loopback"} {
		if strings.Contains(s, h) {
			return true
		}
	}
	return false
}

// staticServers — вручную заданные DNS карты (пусто — «от роутера»).
func staticServers(guid string, v6 bool) string {
	path := `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\` + guid
	if v6 {
		path = `SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\Interfaces\` + guid
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("NameServer")
	return strings.TrimSpace(strings.NewReplacer(",", " ").Replace(v))
}

func netsh(args ...string) error {
	sys, _ := windows.GetSystemDirectory()
	cmd := exec.Command(filepath.Join(sys, "netsh.exe"), args...)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s: %s", strings.Join(args[:3], " "), bytes.TrimSpace(out))
	}
	return nil
}

func setServers(name, family string, servers []string) error {
	if len(servers) == 0 {
		return netsh("interface", family, "set", "dnsservers", "name="+name, "source=dhcp")
	}
	if err := netsh("interface", family, "set", "dnsservers", "name="+name, "source=static",
		"address="+servers[0], "register=primary", "validate=no"); err != nil {
		return err
	}
	for i, s := range servers[1:] {
		if err := netsh("interface", family, "add", "dnsservers", "name="+name, "address="+s,
			fmt.Sprintf("index=%d", i+2), "validate=no"); err != nil {
			return err
		}
	}
	return nil
}

// DoHSupported — шифрование DNS встроено в Windows 11 (сборка 22000+).
func DoHSupported() bool { return windows.RtlGetVersion().BuildNumber >= 22000 }

// Enable ставит серверы провайдера на все карты. Прежние настройки
// запоминаются только при первом включении (повторное — смена провайдера
// или новая карта — не затирает исходные).
func Enable(p Provider) error {
	list, err := adapters()
	if err != nil {
		return err
	}
	prev := loadPrev()
	known := map[string]bool{}
	for _, a := range prev {
		known[a.GUID] = true
	}
	for i := range list {
		if !known[list[i].GUID] {
			list[i].V4 = staticServers(list[i].GUID, false)
			list[i].V6 = staticServers(list[i].GUID, true)
			prev = append(prev, list[i])
		}
	}
	if err := savePrev(prev); err != nil {
		return fmt.Errorf("не удалось запомнить прежние настройки: %w", err)
	}
	if DoHSupported() {
		for _, ip := range append(append([]string(nil), p.V4...), p.V6...) {
			// add — новая запись; если такая уже есть (Windows знает
			// Google/Cloudflare/Quad9 сама) — set обновляет её.
			if netsh("dns", "add", "encryption", "server="+ip, "dohtemplate="+p.DoH, "autoupgrade=yes", "udpfallback=no") != nil {
				_ = netsh("dns", "set", "encryption", "server="+ip, "dohtemplate="+p.DoH, "autoupgrade=yes", "udpfallback=no")
			}
		}
	}
	var errs []string
	for _, a := range list {
		if err := setServers(a.Name, "ipv4", p.V4); err != nil {
			errs = append(errs, err.Error())
		}
		_ = setServers(a.Name, "ipv6", p.V6) // IPv6 может быть выключен — не ошибка
	}
	flush()
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Disable возвращает прежние настройки DNS.
func Disable() error {
	prev := loadPrev()
	var errs []string
	for _, a := range prev {
		if err := setServers(a.Name, "ipv4", strings.Fields(a.V4)); err != nil {
			errs = append(errs, err.Error())
		}
		_ = setServers(a.Name, "ipv6", strings.Fields(a.V6))
	}
	flush()
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	_ = os.Remove(statePath())
	return nil
}

// Active — включён ли (есть запомненные прежние настройки).
func Active() bool {
	_, err := os.Stat(statePath())
	return err == nil
}

func loadPrev() []adapter {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return nil
	}
	var list []adapter
	_ = json.Unmarshal(b, &list)
	return list
}

func savePrev(list []adapter) error {
	b, _ := json.MarshalIndent(list, "", "  ")
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath())
}

var flushProc = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")

func flush() { _, _, _ = flushProc.Call() }
