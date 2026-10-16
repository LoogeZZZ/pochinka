package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/zapret"
)

// Уведомление о новой версии Починки: раз в 6 часов спрашиваем GitHub о
// последнем релизе и, если он новее, показываем плашку со ссылкой.
// Сами ничего не скачиваем и не запускаем.

const (
	appRepo       = "LoogeZZZ/pochinka"
	appReleaseURL = "https://github.com/" + appRepo + "/releases/"
)

// AppUpdate — есть ли версия новее.
type AppUpdate struct {
	Version string `json:"version"` // "1.0.2"; пусто — обновлений нет
	URL     string `json:"url"`     // страница релиза
}

var tagRe = regexp.MustCompile(`^v?\d+(\.\d+){1,3}$`)

// latestAppRelease — тег и страница последнего релиза. Сначала API, при
// лимите или блокировке — редирект /releases/latest (у него лимита нет).
func latestAppRelease(ctx context.Context) (tag, page string, err error) {
	cl := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+appRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Pochinka/"+appVersion)
	if resp, e := cl.Do(req); e == nil {
		var body struct {
			Tag  string `json:"tag_name"`
			Page string `json:"html_url"`
		}
		ok := resp.StatusCode == 200 && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) == nil
		resp.Body.Close()
		if ok && body.Tag != "" {
			return body.Tag, body.Page, nil
		}
	}
	nr := *cl
	nr.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ = http.NewRequestWithContext(ctx, "HEAD", "https://github.com/"+appRepo+"/releases/latest", nil)
	resp, err := nr.Do(req)
	if err != nil {
		return "", "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", "", errors.New("неожиданный ответ GitHub")
	}
	return loc[i+len("/tag/"):], loc, nil
}

// checkAppUpdate — новее ли релиз на GitHub. Тег и ссылку проверяем: в
// окно уходит только версия вида 1.2.3 и только страница нашего репозитория.
func checkAppUpdate(ctx context.Context) (AppUpdate, error) {
	tag, page, err := latestAppRelease(ctx)
	if err != nil {
		return AppUpdate{}, err
	}
	if !tagRe.MatchString(tag) {
		return AppUpdate{}, errors.New("странный тег релиза")
	}
	if !strings.HasPrefix(page, appReleaseURL) {
		page = appReleaseURL + "latest"
	}
	if zapret.CompareVersions(tag, appVersion) <= 0 {
		return AppUpdate{}, nil
	}
	return AppUpdate{Version: strings.TrimPrefix(tag, "v"), URL: page}, nil
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
		u, err := checkAppUpdate(a.ctx)
		if err != nil {
			continue // нет сети или GitHub недоступен — спросим позже
		}
		a.mu.Lock()
		changed := a.appUpdate.Version != u.Version
		a.appUpdate = u
		a.mu.Unlock()
		if changed {
			if u.Version != "" {
				applog.Printf("доступна новая версия Починки: %s", u.Version)
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
	if strings.HasPrefix(u.URL, appReleaseURL) {
		openAsUser(u.URL)
	}
}
