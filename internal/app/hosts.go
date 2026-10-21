package app

import (
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/hostsfile"
)

// SetHostsBlock — переключатель «Записи hosts для Discord и Telegram».
func (a *App) SetHostsBlock(on bool) string {
	var err error
	n := 0
	if on {
		n, err = hostsfile.Enable(a.ctx)
	} else {
		err = hostsfile.Disable()
	}
	if err != nil {
		applog.Printf("hosts: %v", err)
		a.emitState()
		return err.Error()
	}
	_, _ = config.Update(func(c *config.Config) { c.HostsBlock = on })
	if on {
		applog.Printf("hosts: записей для Discord и Telegram: %d", n)
	} else {
		applog.Printf("hosts: записи Починки убраны")
	}
	a.emitState()
	return ""
}

// refreshHosts — при запуске обновить записи: адреса в источнике меняются,
// а устаревшая запись в hosts хуже, чем никакой.
func (a *App) refreshHosts() {
	if !config.Load().HostsBlock {
		return
	}
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(30 * time.Second): // не мешаем подключению при старте
	}
	if n, err := hostsfile.Enable(a.ctx); err != nil {
		applog.Printf("hosts: не обновились (%v), остались прежние", err)
	} else {
		applog.Printf("hosts: обновлено, записей: %d", n)
	}
}
