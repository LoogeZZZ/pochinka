package app

import (
	"pochinka/internal/applog"
	"pochinka/internal/install"
)

// SetAutostart — переключатель «Запускать вместе с Windows».
func (a *App) SetAutostart(on bool) string {
	var err error
	if on {
		err = install.EnableAutostart()
	} else {
		err = install.DisableAutostart()
	}
	a.mu.Lock()
	a.autostart = install.AutostartEnabled()
	a.mu.Unlock()
	a.emitState()
	if err != nil {
		applog.Printf("автозапуск: %v", err)
		return err.Error()
	}
	if on {
		applog.Printf("автозапуск включён")
	} else {
		applog.Printf("автозапуск выключен")
	}
	return ""
}
