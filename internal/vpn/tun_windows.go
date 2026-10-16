package vpn

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"pochinka/internal/assets"
	"pochinka/internal/paths"
)

var (
	wintunOnce sync.Once
	wintunErr  error
)

// ensureWintun кладёт wintun.dll туда, где её найдёт библиотека wintun, и
// загружает по полному пути.
//
// Библиотека ищет DLL только в папке программы и в System32
// (LOAD_LIBRARY_SEARCH_APPLICATION_DIR|SYSTEM32) — загруженную из другой
// папки копию она не видит. Поэтому DLL — рядом с exe, но только если это
// установленная копия в Program Files: туда без прав администратора не
// записать. Exe в «Загрузках» рядом с собой DLL не кладёт (её подменили бы
// между записью и загрузкой) — такой запуск main() сразу передаёт
// установленной копии; здесь остаётся запасной путь через папку данных.
func ensureWintun() error {
	wintunOnce.Do(func() {
		dir := paths.BinDir()
		exe, exeErr := os.Executable()
		if exeErr == nil && paths.RunningInstalled() {
			dir = filepath.Dir(exe)
		}
		p := filepath.Join(dir, "wintun.dll")
		if b, err := os.ReadFile(p); err != nil || !bytes.Equal(b, assets.Wintun) {
			if err := writeAtomic(p, assets.Wintun); err != nil {
				wintunErr = err
				return
			}
		}
		if _, err := windows.LoadLibraryEx(p, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH); err != nil {
			wintunErr = err
			return
		}
		// Копию из прошлых версий рядом с неустановленным exe убираем (если она наша).
		if exeErr == nil && !paths.RunningInstalled() {
			old := filepath.Join(filepath.Dir(exe), "wintun.dll")
			if b, err := os.ReadFile(old); err == nil && bytes.Equal(b, assets.Wintun) {
				_ = os.Remove(old)
			}
		}
	})
	return wintunErr
}

var (
	iphlpapi          = windows.NewLazySystemDLL("iphlpapi.dll")
	getBestInterfaceP = iphlpapi.NewProc("GetBestInterface")
)

// adapter — сетевой адаптер Windows с тем, что нам о нём важно.
type adapter struct {
	index       int
	name, descr string
	ifType      uint32
	up, gateway bool
	metric      uint32
}

func listAdapters() ([]adapter, error) {
	size := uint32(16 << 10)
	var buf []byte
	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_INET, windows.GAA_FLAG_INCLUDE_GATEWAYS,
			0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, err
		}
	}
	var out []adapter
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		out = append(out, adapter{
			index: int(a.IfIndex), ifType: a.IfType, metric: a.Ipv4Metric,
			name:    windows.UTF16PtrToString(a.FriendlyName),
			descr:   windows.UTF16PtrToString(a.Description),
			up:      a.OperStatus == windows.IfOperStatusUp,
			gateway: a.FirstGatewayAddress != nil,
		})
	}
	return out, nil
}

// Признаки виртуальных адаптеров: VPN-клиенты нередко притворяются
// обычным Ethernet (Radmin VPN, OpenVPN TAP), поэтому смотрим и на описание.
var virtualHints = []string{"vpn", "tap-", "tap ", "tunnel", "wintun", "wireguard", "virtual", "hamachi",
	"zerotier", "tailscale", "radmin", "happ", "xray", "sing-box", "clash", "outline", "amnezia", "hyper-v", "vmware", "virtualbox", "loopback"}

func isVirtual(a adapter) bool {
	if a.ifType != windows.IF_TYPE_ETHERNET_CSMACD && a.ifType != windows.IF_TYPE_IEEE80211 {
		return true
	}
	s := strings.ToLower(a.name + " " + a.descr)
	for _, h := range virtualHints {
		if strings.Contains(s, h) {
			return true
		}
	}
	return false
}

// PhysicalInterface — настоящая сетевая карта (Ethernet или Wi‑Fi со
// шлюзом). Не «куда сейчас идёт маршрут по умолчанию»: если работает другой
// VPN в режиме TUN (Happ, v2rayN...), маршрут ведёт в его туннель, и наши
// соединения, привязанные туда, ломаются.
func PhysicalInterface() (*net.Interface, error) {
	list, err := listAdapters()
	if err != nil {
		return nil, fmt.Errorf("список адаптеров: %w", err)
	}
	var best *adapter
	for i := range list {
		a := &list[i]
		if !a.up || !a.gateway || isVirtual(*a) {
			continue
		}
		if best == nil || a.metric < best.metric {
			best = a
		}
	}
	if best == nil {
		return nil, errors.New("не найдена сетевая карта с выходом в интернет")
	}
	return net.InterfaceByIndex(best.index)
}

// waitTunGone ждёт, пока адаптер name исчезнет из системы. true — исчез.
func waitTunGone(name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := net.InterfaceByName(name); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// ForeignVPN — имя чужого VPN-адаптера, который сейчас перехватывает
// интернет (маршрут к 1.1.1.1 идёт не через сетевую карту и не через нас).
// Пустая строка — всё чисто.
func ForeignVPN() string {
	addr := netip.MustParseAddr("1.1.1.1").As4()
	ip := *(*uint32)(unsafe.Pointer(&addr[0]))
	var idx uint32
	if r, _, _ := getBestInterfaceP.Call(uintptr(ip), uintptr(unsafe.Pointer(&idx))); r != 0 {
		return ""
	}
	list, err := listAdapters()
	if err != nil {
		return ""
	}
	for _, a := range list {
		if a.index != int(idx) {
			continue
		}
		if strings.HasPrefix(a.name, TunName) || !isVirtual(a) { // наш (основной или запасной)
			return ""
		}
		if a.descr != "" && a.descr != a.name {
			return a.descr
		}
		return a.name
	}
	return ""
}

// setupTunRoutes ждёт появления адаптера, даёт ему адрес и заворачивает в
// него весь IPv4 двумя маршрутами /1 (они точнее основного 0.0.0.0/0, и при
// этом сам основной маршрут не трогаем — после выхода всё как было).
// Маршруты store=active: исчезают вместе с адаптером и после перезагрузки.
func setupTunRoutes(name string) error {
	var ifc *net.Interface
	for i := 0; i < 50; i++ {
		if c, err := net.InterfaceByName(name); err == nil {
			ifc = c
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ifc == nil {
		return errors.New("адаптер TUN не появился")
	}
	steps := [][]string{
		{"interface", "ipv4", "set", "address", "name=" + name, "source=static", "address=198.18.0.1", "mask=255.255.255.252"},
		{"interface", "ipv4", "add", "route", "prefix=0.0.0.0/1", "interface=" + name, "nexthop=0.0.0.0", "metric=1", "store=active"},
		{"interface", "ipv4", "add", "route", "prefix=128.0.0.0/1", "interface=" + name, "nexthop=0.0.0.0", "metric=1", "store=active"},
	}
	for _, args := range steps {
		cmd := exec.Command("netsh", args...)
		hideWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("netsh %v: %v %s", args[2:4], err, bytes.TrimSpace(out))
		}
	}
	// IPv6 тоже в туннель: иначе на сетях с IPv6 заблокированные сайты
	// уходили бы мимо VPN по IPv6. Без IPv6 у провайдера это ничего не
	// меняет, поэтому ошибки тут не фатальны.
	for _, args := range [][]string{
		{"interface", "ipv6", "add", "address", "interface=" + name, "address=fdfe:dcba:9876::1/126", "store=active"},
		{"interface", "ipv6", "add", "route", "prefix=::/1", "interface=" + name, "metric=1", "store=active"},
		{"interface", "ipv6", "add", "route", "prefix=8000::/1", "interface=" + name, "metric=1", "store=active"},
	} {
		cmd := exec.Command("netsh", args...)
		hideWindow(cmd)
		_ = cmd.Run()
	}
	return nil
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
