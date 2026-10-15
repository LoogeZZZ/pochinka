package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"

	"pochinka/internal/applog"
)

// Тексты лицензий вшиты в exe: программа распространяется одним файлом, а
// MIT/BSD/Apache требуют прикладывать уведомления к бинарнику.
//
//go:embed LICENSE
var licenseText string

//go:embed third_party/THIRD_PARTY_LICENSES.txt
var thirdPartyText string

// OpenLicenses показывает лицензии в Блокноте (с правами пользователя, через
// оболочку: Блокнот с правами администратора открыл бы любой файл системы).
func (a *App) OpenLicenses() {
	p := filepath.Join(os.TempDir(), "Pochinka-licenses.txt")
	text := "Починка " + appVersion + "\r\n\r\n" + licenseText + "\r\n\r\n" + thirdPartyText
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		applog.Printf("лицензии: %v", err)
		return
	}
	_ = exec.Command("explorer.exe", p).Start()
}
