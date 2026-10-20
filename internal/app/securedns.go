package app

import (
	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/securedns"
)

// SecureDNSState — для окна.
type SecureDNSState struct {
	Provider string `json:"provider"` // "" — выключен
	DoH      bool   `json:"doh"`      // шифрование доступно (Windows 11)
	Error    string `json:"error"`
}

func (a *App) secureDNSState() SecureDNSState {
	a.mu.Lock()
	e := a.sdnsErr
	a.mu.Unlock()
	return SecureDNSState{Provider: config.Load().SecureDNS, DoH: securedns.DoHSupported(), Error: e}
}

// SetSecureDNS: "" — выключить (вернуть прежние DNS), google | cloudflare | quad9.
func (a *App) SetSecureDNS(id string) string {
	var err error
	if id == "" {
		err = securedns.Disable()
	} else if p, ok := securedns.Find(id); ok {
		err = securedns.Enable(p)
	} else {
		return "неизвестный DNS"
	}
	msg := ""
	if err != nil {
		msg = err.Error()
		applog.Printf("защищённый DNS: %v", err)
	} else if id == "" {
		applog.Printf("защищённый DNS выключен — прежние настройки возвращены")
	} else {
		applog.Printf("защищённый DNS: %s", id)
	}
	if err == nil {
		_, _ = config.Update(func(c *config.Config) { c.SecureDNS = id })
	}
	a.mu.Lock()
	a.sdnsErr = msg
	a.mu.Unlock()
	a.emitState()
	if err == nil {
		go a.RunChecks()
	}
	return msg
}

// ensureSecureDNS — при запуске: если включён, применить и к картам,
// появившимся с прошлого раза (новый Wi‑Fi адаптер и т.п.).
func (a *App) ensureSecureDNS() {
	id := config.Load().SecureDNS
	if id == "" {
		return
	}
	if p, ok := securedns.Find(id); ok {
		if err := securedns.Enable(p); err != nil {
			applog.Printf("защищённый DNS: %v", err)
		}
	}
}
