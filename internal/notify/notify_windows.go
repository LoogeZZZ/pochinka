//go:build windows

// Package notify - уведомления Windows (тосты) о событиях программы:
// подключение/отключение, ошибки, обновления, предупреждения подписки.
// Показываются только если в конфиге включено (config.Notify).
//
// Тост показывает Windows через WinRT (go-toast): чтобы он отображался под
// именем «Починка», приложение один раз регистрируется в реестре под своим
// AppUserModelID. Для незарегистрированного AUMID CreateToastNotifier не
// находит приложение и уведомление не показывается вовсе.
package notify

import (
	"sync"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"golang.org/x/sys/windows/registry"

	"pochinka/internal/applog"
	"pochinka/internal/config"
)

// AppID - идентификатор приложения в системе уведомлений Windows. Имя,
// под которым уведомления видны в Центре уведомлений, ставится в реестре
// при первой регистрации (DisplayName).
const AppID = "LoogeZZZ.Pochinka"

var regOnce sync.Once

// register ставит DisplayName в реестре. Без этого тост либо не покажется
// вовсе, либо будет подписан чужим приложением (PowerShell).
func register() {
	regOnce.Do(func() {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+AppID, registry.SET_VALUE)
		if err != nil {
			applog.Printf("уведомления: не удалось зарегистрировать приложение: %v", err)
			return
		}
		defer k.Close()
		if err := k.SetStringValue("DisplayName", "Починка"); err != nil {
			applog.Printf("уведомления: не удалось записать имя приложения: %v", err)
		}
	})
}

// Enabled - включены ли уведомления в конфиге.
func Enabled() bool { return config.Load().Notify }

// Show показывает уведомление, если они включены. Ошибки показа только в
// журнал: уведомления - сервисная функция, падать из-за них нельзя.
func Show(title, message string) {
	if !Enabled() {
		return
	}
	register()
	n := toast.Notification{AppID: AppID, Title: title, Body: message}
	if err := n.Push(); err != nil {
		applog.Printf("уведомление не показалось: %v", err)
	}
}

// Text - строка на языке интерфейса: ru - если в конфиге пусто/ru,
// иначе en. Для уведомлений, которые рождаются в бэкенде.
func Text(ru, en string) string {
	if config.Load().Lang == "en" {
		return en
	}
	return ru
}
