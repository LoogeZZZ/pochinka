package app

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"pochinka/internal/applog"
	"pochinka/internal/install"
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
	for _, t := range []string{install.TaskOpen, install.TaskAutostart} {
		if install.TaskExists(t) {
			_, _ = install.Schtasks("/Delete", "/TN", t, "/F")
		}
	}

	// 3. Служба драйвера WinDivert (её создаёт winws). Если она нужна другой
	//    программе на WinDivert, та создаст её заново при запуске.
	_ = winutil.DeleteService("WinDivert")

	// 4. Папки: копия в Program Files, данные (настройки, подписки, zapret,
	//    журналы), профиль окна WebView2 в %APPDATA%.
	var dirs []string
	if inst, err := install.InstalledExe(); err == nil {
		dirs = append(dirs, filepath.Dir(inst))
	}
	dirs = append(dirs, paths.DataDir())
	if ad, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0); err == nil {
		if exe, err := os.Executable(); err == nil {
			dirs = append(dirs, filepath.Join(ad, filepath.Base(exe)))
		}
	}
	if err := install.RemoveAfterExit(dirs); err != nil {
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
