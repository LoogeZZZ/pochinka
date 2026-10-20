package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"pochinka/internal/applog"
	"pochinka/internal/install"
	"pochinka/internal/paths"
	"pochinka/internal/selfupdate"
)

// Новая версия Починки: раз в 6 часов спрашиваем GitHub и, если есть
// версия новее, показываем плашку. «Обновить» скачивает её, проверяет
// подпись (selfupdate) и ставит вместо текущей (install).

// AppUpdate — плашка обновления в окне.
type AppUpdate struct {
	Version string `json:"version"` // "1.0.2"; пусто — обновлений нет
	URL     string `json:"url"`     // страница релиза
	Tag     string `json:"-"`       // "v1.0.2"

	Busy     bool   `json:"busy"`
	Progress int    `json:"progress"` // 0..100
	Error    string `json:"error"`
}

// watchAppUpdates — первая проверка через минуту после старта (не мешаем
// подключению), дальше раз в 6 часов.
func (a *App) watchAppUpdates() {
	delay := time.Minute
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = 6 * time.Hour
		r, err := selfupdate.Check(a.ctx, Version)
		if err != nil {
			continue // нет сети или GitHub недоступен — спросим позже
		}
		a.mu.Lock()
		if a.appUpdate.Busy { // идёт установка — не мешаем
			a.mu.Unlock()
			continue
		}
		changed := a.appUpdate.Version != r.Version
		a.appUpdate = AppUpdate{Version: r.Version, URL: r.URL, Tag: r.Tag}
		a.mu.Unlock()
		if changed {
			if r.Version != "" {
				applog.Printf("доступна новая версия Починки: %s", r.Version)
			}
			a.emitState()
		}
	}
}

// OpenAppUpdate — открыть страницу новой версии.
func (a *App) OpenAppUpdate() {
	a.mu.Lock()
	u := a.appUpdate
	a.mu.Unlock()
	if strings.HasPrefix(u.URL, selfupdate.ReleaseURL) {
		openAsUser(u.URL)
	}
}

// InstallAppUpdate — кнопка «Обновить»: скачать, проверить подпись,
// заменить программу и перезапуститься. Включённое восстановится само.
func (a *App) InstallAppUpdate() string {
	a.mu.Lock()
	u := a.appUpdate
	if u.Version == "" || u.Busy {
		a.mu.Unlock()
		return ""
	}
	a.appUpdate.Busy, a.appUpdate.Progress, a.appUpdate.Error = true, 0, ""
	a.mu.Unlock()
	a.emitState()
	go func() {
		err := a.installUpdate(u)
		if err == nil {
			return // программа уже перезапускается
		}
		applog.Printf("обновление: %v", err)
		a.mu.Lock()
		a.appUpdate.Busy, a.appUpdate.Error = false, err.Error()
		a.mu.Unlock()
		a.emitState()
	}()
	return ""
}

func (a *App) installUpdate(u AppUpdate) error {
	dir := filepath.Join(paths.DataDir(), "update") // только для администраторов
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	exe, err := selfupdate.Download(ctx, u.Tag, dir, "Pochinka/"+Version, func(p int) {
		a.mu.Lock()
		a.appUpdate.Progress = p
		a.mu.Unlock()
		a.emitState()
	})
	if err != nil {
		return err
	}
	applog.Printf("обновление %s скачано, подпись верна — устанавливаю", u.Version)

	inst, err := install.InstalledExe()
	if err != nil {
		return err
	}
	if err := install.ReplaceExe(exe, inst); err != nil {
		return err
	}
	// Новый экземпляр — после выхода этого: иначе он увидит работающий старый
	// (одна копия на компьютер), передаст ему управление и закроется.
	if err := install.StartAfterExit(inst); err != nil {
		return err
	}
	applog.Printf("перезапуск в версию %s", u.Version)
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	runtime.Quit(a.ctx)
	return nil
}
