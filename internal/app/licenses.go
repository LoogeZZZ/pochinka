package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"pochinka/internal/applog"
)

// Тексты лицензий вшиты в exe: программа распространяется одним файлом, а
// MIT/BSD/Apache требуют прикладывать уведомления к бинарнику. Вшивает их
// main (go:embed видит только свою папку и ниже) и кладёт сюда.
var LicenseText, ThirdPartyText string

// OpenLicenses показывает лицензии в Блокноте (с правами пользователя, через
// оболочку: Блокнот с правами администратора открыл бы любой файл системы).
func (a *App) OpenLicenses() {
	p := filepath.Join(os.TempDir(), "Pochinka-licenses.txt")
	text := "Починка " + Version + "\r\n\r\n" + LicenseText + "\r\n\r\n" + ThirdPartyText
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		applog.Printf("лицензии: %v", err)
		return
	}
	_ = exec.Command("explorer.exe", p).Start()
}
