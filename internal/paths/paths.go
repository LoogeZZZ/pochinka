// Package paths определяет, где приложение хранит данные.
//
// Используем %ProgramData%\Pochinka, а не профиль пользователя: путь всегда
// латинский (winws/cygwin ломается на кириллице в пути), а права у нас и так
// админские — без них WinDivert не стартует. Права на папку закрываются
// (см. acl_windows.go).
package paths

import (
	"os"
	"path/filepath"
)

const AppName = "Pochinka"

func DataDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, AppName)
}

// ZapretVersionsDir — сюда распаковываются релизы zapret, каждый в свою папку
// по версии. Так обновление готовится рядом, не трогая работающий winws.
func ZapretVersionsDir() string { return filepath.Join(DataDir(), "zapret") }

// UserDir — пользовательские списки, переживают обновления zapret.
func UserDir() string { return filepath.Join(DataDir(), "user") }

// BinDir — свои библиотеки (wintun.dll): в папке только для администраторов,
// а не рядом с exe, который может лежать в «Загрузках».
func BinDir() string { return filepath.Join(DataDir(), "bin") }

// LogsDir — журналы; единственная папка, которую могут читать все.
func LogsDir() string { return filepath.Join(DataDir(), "logs") }

func ConfigFile() string { return filepath.Join(DataDir(), "config.json") }

func LogFile() string { return filepath.Join(LogsDir(), "pochinka.log") }

func Ensure() error {
	for _, d := range []string{DataDir(), ZapretVersionsDir(), UserDir(), BinDir(), LogsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
