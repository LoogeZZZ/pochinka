package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"golang.org/x/sys/windows/registry"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/paths"
	"pochinka/internal/vpn"
	"pochinka/internal/winutil"
)

//go:embed all:frontend/dist
var frontend embed.FS

func main() {
	// Без прав администратора — только запускаем себя с правами (через
	// задачу Планировщика без UAC или один раз через UAC) и выходим.
	if !winutil.IsAdmin() {
		launchElevated()
	}
	hardenEnv() // до первого обращения к путям и до WebView2
	if err := paths.Ensure(); err != nil {
		os.Exit(1)
	}
	secErr := paths.Secure()
	defer func() {
		if r := recover(); r != nil {
			applog.Printf("ПАДЕНИЕ: %v\n%s", r, debug.Stack())
			applog.Close()
			panic(r)
		}
	}()
	applog.Init(paths.LogFile())
	log.SetFlags(0)
	log.SetOutput(stdLog{})
	if secErr != nil {
		applog.Printf("ВНИМАНИЕ: не удалось закрыть права на папку данных: %v", secErr)
	}
	// Журналы прошлых версий лежали в корне папки данных.
	for _, old := range []string{"pochinka.log", "pochinka.log.old", "xray.log", "xray-config.json", "diag-tun.txt", "diag-xray.log", "webview"} {
		_ = os.RemoveAll(filepath.Join(paths.DataDir(), old))
	}
	if hasArg("--diag-tun") {
		applog.Printf("диагностика TUN: %s", vpn.DiagTUN())
		return
	}
	applog.Printf("Починка запущена")
	// Копия в Program Files = эта версия, запуск без UAC настроен. До
	// wails.Run: если уже работает старый экземпляр, этот сразу выйдет.
	// Работаем всегда из установленной копии: ей одной можно класть рядом
	// wintun.dll (TUN) — в её папку без прав администратора не записать.
	if ensureInstalled() && !paths.RunningInstalled() && len(os.Args) == 1 {
		if _, err := schtasks("/Run", "/TN", taskOpen); err == nil {
			applog.Printf("перезапуск из установленной копии")
			applog.Close()
			return
		}
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "Починка",
		Width:            360,
		Height:           580,
		DisableResize:    true,
		Frameless:        true, // своя шапка в интерфейсе, без системной рамки
		StartHidden:      hasArg("--minimized"),
		BackgroundColour: windowBackground(),
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "pochinka-7f3e2a91-zapret-tg",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.showWindow()
			},
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
	})
	if err != nil {
		applog.Printf("ошибка окна: %v", err)
	}
}

func hasArg(a string) bool {
	for _, x := range os.Args[1:] {
		if x == a {
			return true
		}
	}
	return false
}

// windowBackground — цвет окна до загрузки интерфейса, по выбранной теме,
// чтобы при старте не мелькал «не тот» фон.
func windowBackground() *options.RGBA {
	dark := &options.RGBA{R: 17, G: 18, B: 20, A: 255}
	light := &options.RGBA{R: 244, G: 245, B: 247, A: 255}
	switch config.Load().Theme {
	case "dark":
		return dark
	case "light":
		return light
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return dark
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil && v == 1 {
		return light
	}
	return dark
}

// stdLog — стандартный лог Go → журнал Починки. В него пишут библиотеки:
// wintun объясняет здесь, почему не создался адаптер TUN. Без этого
// сообщения уходили в stderr, которого у оконного приложения нет.
type stdLog struct{}

func (stdLog) Write(p []byte) (int, error) {
	if s := strings.TrimSpace(string(p)); s != "" {
		applog.Printf("%s", s)
	}
	return len(p), nil
}
