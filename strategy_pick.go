package main

import (
	"context"
	"fmt"
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/health"
	"pochinka/internal/zapret"
)

// PickState — автоподбор стратегии для окна.
type PickState struct {
	Running bool   `json:"running"`
	Current string `json:"current"` // какую стратегию пробуем сейчас
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Result  string `json:"result"` // итог последнего подбора
	OK      bool   `json:"ok"`
}

// AutoPickStrategy перебирает стратегии zapret, пока не откроются и Discord,
// и YouTube. Начинает с текущей. Если ни одна не открыла оба — оставляет ту,
// что открыла хотя бы один (быстрее — лучше), а если и такой нет — исходную.
func (a *App) AutoPickStrategy() string {
	if a.zapret == nil {
		return "Обход недоступен — см. журнал"
	}
	a.mu.Lock()
	if a.pick.Running {
		a.mu.Unlock()
		return ""
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.pick = PickState{Running: true}
	a.pickCancel = cancel
	a.mu.Unlock()
	a.emitState()
	go a.runPick(ctx, cancel)
	return ""
}

// StopPickStrategy — «Остановить» в окне.
func (a *App) StopPickStrategy() {
	a.mu.Lock()
	c := a.pickCancel
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

func (a *App) runPick(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	finish := func(ok bool, msg string) {
		applog.Printf("подбор стратегии: %s", msg)
		a.mu.Lock()
		a.pick.Running, a.pick.OK, a.pick.Result, a.pick.Current = false, ok, msg, ""
		a.pickCancel = nil
		a.mu.Unlock()
		a.emitState()
	}

	// Обход должен работать (VPN при этом выключится — они взаимоисключающие).
	if a.zapret.State().Status != zapret.StatusRunning {
		if msg := a.SetZapret(true); msg != "" {
			finish(false, "не удалось включить обход: "+msg)
			return
		}
	}
	st := a.zapret.State()
	orig := st.Strategy
	order := []string{orig}
	for _, n := range st.Strategies {
		if n != orig {
			order = append(order, n)
		}
	}

	type cand struct {
		name    string
		latency int64
	}
	var partial *cand
	for i, name := range order {
		if ctx.Err() != nil {
			a.restoreStrategy(orig)
			finish(false, "остановлено — вернул «"+prettyStrategy(orig)+"»")
			return
		}
		// Обход выключили вручную — подбирать нечего.
		if i > 0 && a.zapret.State().Status != zapret.StatusRunning {
			finish(false, "обход выключен — подбор остановлен")
			return
		}
		a.mu.Lock()
		a.pick.Current, a.pick.Done, a.pick.Total = prettyStrategy(name), i, len(order)
		a.mu.Unlock()
		a.emitState()

		if name != a.zapret.State().Strategy {
			if err := a.zapret.SetStrategy(ctx, name); err != nil {
				applog.Printf("подбор: «%s» не запустилась: %v", name, err)
				continue
			}
		}
		select { // даём WinDivert и winws встать
		case <-ctx.Done():
			continue
		case <-time.After(1500 * time.Millisecond):
		}
		cctx, ccancel := context.WithTimeout(ctx, 15*time.Second)
		res := health.CheckAll(cctx)
		ccancel()
		if ctx.Err() != nil {
			continue
		}
		a.mu.Lock()
		a.checks = res
		a.mu.Unlock()

		d, y := res["discord"], res["youtube"]
		applog.Printf("подбор: «%s» — Discord %v, YouTube %v", prettyStrategy(name), d.OK, y.OK)
		if d.OK && y.OK {
			finish(true, fmt.Sprintf("подошла «%s»: Discord и YouTube открываются", prettyStrategy(name)))
			return
		}
		if d.OK || y.OK {
			lat := d.Latency + y.Latency
			if partial == nil || lat < partial.latency {
				partial = &cand{name, lat}
			}
		}
	}
	if partial != nil {
		a.restoreStrategy(partial.name)
		finish(false, fmt.Sprintf("полностью не подошла ни одна — оставил «%s»: открывается только часть сервисов", prettyStrategy(partial.name)))
		return
	}
	a.restoreStrategy(orig)
	finish(false, "ни одна стратегия не открыла Discord и YouTube — возможно, их блокируют иначе; попробуйте VPN")
}

func (a *App) restoreStrategy(name string) {
	if a.zapret.State().Strategy != name {
		if err := a.zapret.SetStrategy(a.ctx, name); err != nil {
			applog.Printf("подбор: не удалось вернуть «%s»: %v", name, err)
		}
	}
	go a.RunChecks()
}

// prettyStrategy: "general (ALT2)" → "ALT2", "general" → "Стандартная".
func prettyStrategy(n string) string {
	s := n
	if len(s) >= 7 && s[:7] == "general" {
		s = s[7:]
	}
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		return "Стандартная"
	}
	return s
}
