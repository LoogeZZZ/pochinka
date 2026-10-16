package vpn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"pochinka/internal/paths"
)

// DeviceInfo — что клиент сообщает панели о себе при загрузке подписки.
// Стандарт заголовков задал Happ; по ним Remnawave (HWID Device Limit)
// считает устройства и без x-hwid отдаёт 404.
type DeviceInfo struct {
	HWID      string // x-hwid
	OS        string // x-device-os
	OSVersion string // x-ver-os
	Model     string // x-device-model
}

var (
	deviceOnce sync.Once
	device     DeviceInfo
)

// Device — сведения об этом компьютере (считаются один раз).
func Device() DeviceInfo {
	deviceOnce.Do(func() {
		device = DeviceInfo{HWID: hwid(), OS: "Windows", OSVersion: osVersion(), Model: deviceModel()}
	})
	return device
}

// hwid — постоянный идентификатор устройства: хеш MachineGuid (меняется
// только при переустановке Windows) с нашей солью. Сам MachineGuid наружу
// не уходит, и с HWID других программ наш не совпадает. Формат подходит под
// проверку Remnawave: ^[a-zA-Z0-9=-]{10,64}$.
func hwid() string {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY); err == nil {
		g, _, err := k.GetStringValue("MachineGuid")
		k.Close()
		if err == nil && strings.TrimSpace(g) != "" {
			sum := sha256.Sum256([]byte("pochinka-hwid:" + strings.ToLower(strings.TrimSpace(g))))
			return hex.EncodeToString(sum[:16])
		}
	}
	// Без MachineGuid — случайный, но запомненный: иначе каждый запуск был
	// бы «новым устройством» и съедал лимит.
	p := filepath.Join(paths.DataDir(), "hwid")
	if b, err := os.ReadFile(p); err == nil && validHWID(strings.TrimSpace(string(b))) {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	_ = writeAtomic(p, []byte(id))
	return id
}

func validHWID(s string) bool {
	if len(s) < 10 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '=' || c == '-') {
			return false
		}
	}
	return true
}

func osVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// deviceModel — модель из BIOS («Micro-Star MS-7D25»), чтобы в панели было
// видно, что за устройство. Заглушки производителей отбрасываем.
func deviceModel() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return "PC"
	}
	defer k.Close()
	var parts []string
	for _, name := range []string{"SystemManufacturer", "SystemProductName"} {
		v, _, err := k.GetStringValue(name)
		v = asciiClean(v)
		l := strings.ToLower(v)
		if err != nil || v == "" || strings.Contains(l, "o.e.m") || strings.Contains(l, "default string") ||
			l == "system manufacturer" || l == "system product name" {
			continue
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "PC"
	}
	return clipText(strings.Join(parts, " "), 64)
}

// asciiClean — только печатный ASCII: значение уходит в HTTP-заголовок.
func asciiClean(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= 0x20 && c < 0x7f {
			b.WriteRune(c)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
