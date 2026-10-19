package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/health"
	"pochinka/internal/zapret"
)

// pickWant — сколько полностью рабочих стратегий ищем, чтобы у пользователя
// был выбор, а не одна «подошедшая»: у разных стратегий разная скорость, и
// какая-то может перестать работать завтра.
const pickWant = 7

// PickResult — стратегия, на которой открылся хотя бы один сервис.
type PickResult struct {
	Name    string `json:"name"`   // как в списке стратегий ("general (ALT2)")
	Pretty  string `json:"pretty"` // для показа ("ALT2")
	Discord bool   `json:"discord"`
	YouTube bool   `json:"youtube"`
	Latency int64  `json:"latencyMs"` // сумма по обоим сервисам
}

func (r PickResult) full() bool { return r.Discord && r.YouTube }

// PickState — автоподбор стратегии для окна.
type PickState struct {
	Running bool         `json:"running"`
	Current string       `json:"current"` // какую стратегию пробуем сейчас
	Done    int          `json:"done"`
	Total   int          `json:"total"`
	Found   int          `json:"found"`  // сколько полностью рабочих уже нашли
	Result  string       `json:"result"` // итог последнего подбора
	OK      bool         `json:"ok"`
	Results []PickResult `json:"results"` // рабочие, лучшие сверху
}

// AutoPickStrategy проверяет стратегии zapret (начиная с текущей), пока не
// наберёт pickWant полностью рабочих или не кончится список. Включает самую
// быструю, остальные показывает списком на выбор.
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

// StopPickStrategy — «Остановить» в окне. Уже найденное не пропадает.
func (a *App) StopPickStrategy() {
	a.mu.Lock()
	c := a.pickCancel
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

// ApplyPicked — выбор из списка «Подошли».
func (a *App) ApplyPicked(name string) string {
	a.mu.Lock()
	running, known := a.pick.Running, false
	for _, r := range a.pick.Results {
		if r.Name == name {
			known = true
		}
	}
	a.mu.Unlock()
	if running || !known {
		return ""
	}
	return a.SetStrategy(name)
}

func (a *App) runPick(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	var results []PickResult
	publish := func() {
		sorted := rankPicks(results)
		a.mu.Lock()
		a.pick.Results = sorted
		a.pick.Found = countFull(sorted)
		a.mu.Unlock()
	}
	finish := func(ok bool, msg string) {
		applog.Printf("подбор стратегии: %s", msg)
		publish()
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

	stopped := false
	for i, name := range order {
		if ctx.Err() != nil {
			stopped = true
			break
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
		if d.OK || y.OK {
			results = append(results, PickResult{Name: name, Pretty: prettyStrategy(name),
				Discord: d.OK, YouTube: y.OK, Latency: d.Latency + y.Latency})
			publish()
			a.emitState()
		}
		if countFull(results) >= pickWant {
			break
		}
	}

	ranked := rankPicks(results)
	switch {
	case len(ranked) > 0 && ranked[0].full():
		a.restoreStrategy(ranked[0].Name)
		msg := fmt.Sprintf("включил самую быструю — «%s»; подошло вариантов: %d", ranked[0].Pretty, countFull(ranked))
		if stopped {
			msg = "остановлено — " + msg
		}
		finish(true, msg)
	case len(ranked) > 0:
		a.restoreStrategy(ranked[0].Name)
		finish(false, fmt.Sprintf("полностью не подошла ни одна — включил «%s»: открывается только часть сервисов", ranked[0].Pretty))
	default:
		a.restoreStrategy(orig)
		if stopped {
			finish(false, "остановлено — вернул «"+prettyStrategy(orig)+"»")
			return
		}
		finish(false, "ни одна стратегия не открыла Discord и YouTube — возможно, их блокируют иначе; попробуйте VPN")
	}
}

// rankPicks: сначала открывшие оба сервиса, внутри — по скорости.
func rankPicks(in []PickResult) []PickResult {
	out := append([]PickResult(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].full() != out[j].full() {
			return out[i].full()
		}
		return out[i].Latency < out[j].Latency
	})
	return out
}

func countFull(rs []PickResult) int {
	n := 0
	for _, r := range rs {
		if r.full() {
			n++
		}
	}
	return n
}

func (a *App) restoreStrategy(name string) {
	if a.zapret.State().Strategy != name {
		if err := a.zapret.SetStrategy(a.ctx, name); err != nil {
			applog.Printf("подбор: не удалось включить «%s»: %v", name, err)
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
