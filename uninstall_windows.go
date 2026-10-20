package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"pochinka/internal/applog"
	"pochinka/internal/paths"
	"pochinka/internal/securedns"
	"pochinka/internal/winutil"
)

// Uninstall — «Удалить Починку»: выключить всё, вернуть системные настройки
// и убрать за собой всё, что программа создала. Скачанный exe (например, в
// «Загрузках») не трогаем: его пользователь положил сам.
//
// Запущенный exe и открытые файлы удалить нельзя, поэтому папки удаляет
// отдельный cmd через пару секунд после выхода.
func (a *App) Uninstall() string {
	applog.Printf("удаление Починки")

	// 1. Выключить: VPN вернёт системный прокси и уберёт адаптер TUN,
	//    обход выгрузит WinDivert, прокси Telegram закроется.
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	if a.vpn != nil {
		a.vpn.Close()
	}
	if a.zapret != nil {
		a.zapret.Stop()
	}
	if a.tg != nil {
		a.tg.stop()
	}
	// Защищённый DNS — вернуть прежние DNS сетевых карт (до удаления папки
	// данных: там записано, какие они были).
	if securedns.Active() {
		if err := securedns.Disable(); err != nil {
			applog.Printf("удаление: DNS не вернулся: %v", err)
		}
	}

	// 2. Задачи Планировщика: запуск без UAC и автозапуск.
	for _, t := range []string{taskOpen, taskAutostart} {
		if taskExists(t) {
			_, _ = schtasks("/Delete", "/TN", t, "/F")
		}
	}

	// 3. Служба драйвера WinDivert (её создаёт winws). Если она нужна другой
	//    программе на WinDivert, та создаст её заново при запуске.
	_ = winutil.DeleteService("WinDivert")

	// 4. Папки: копия в Program Files, данные (настройки, подписки, zapret,
	//    журналы), профиль окна WebView2 в %APPDATA%.
	var dirs []string
	if inst, err := installedExe(); err == nil {
		dirs = append(dirs, filepath.Dir(inst))
	}
	dirs = append(dirs, paths.DataDir())
	if ad, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0); err == nil {
		if exe, err := os.Executable(); err == nil {
			dirs = append(dirs, filepath.Join(ad, filepath.Base(exe)))
		}
	}
	if err := removeAfterExit(dirs); err != nil {
		applog.Printf("удаление: %v", err)
		return "Не удалось запланировать удаление файлов: " + err.Error()
	}

	applog.Printf("удаление: программа закрывается, файлы удалятся через несколько секунд")
	applog.Close()
	if a.tray != nil {
		a.tray.stop()
	}
	runtime.Quit(a.ctx)
	return ""
}

// removeAfterExit запускает скрытый cmd, который ждёт выхода программы и
// удаляет папки. Пути берутся только из системных папок (не из окружения и
// не от пользователя), поэтому подставлять их в команду безопасно.
func removeAfterExit(dirs []string) error {
	var b strings.Builder
	// ~4 секунды на выход программы и WebView2.
	b.WriteString("ping 127.0.0.1 -n 5 >nul")
	for _, d := range dirs {
		if !safeToRemove(d) {
			return fmt.Errorf("странный путь %q", d)
		}
		fmt.Fprintf(&b, ` & rd /s /q "%s"`, d)
	}
	cmd := exec.Command(filepath.Join(systemDir(), "cmd.exe"))
	// Командную строку целиком: cmd по-своему разбирает кавычки.
	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
		CmdLine:       `cmd.exe /d /c "` + b.String() + `"`,
	}
	return cmd.Start()
}

// safeToRemove — последний рубеж: удаляем только папки с «Pochinka» в имени
// и не корень диска, что бы ни случилось с путями выше.
func safeToRemove(d string) bool {
	d = filepath.Clean(d)
	base := strings.ToLower(filepath.Base(d))
	return len(d) > 10 && strings.Contains(base, "pochinka") && !strings.ContainsAny(d, `"&|<>^%`)
}
