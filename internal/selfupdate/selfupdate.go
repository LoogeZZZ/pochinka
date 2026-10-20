// Package selfupdate — новые версии Починки: проверка релизов на GitHub,
// скачивание и проверка подписи. Установку (замена exe, перезапуск) делает
// пакет install.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pochinka/internal/zapret"
)

const (
	Repo       = "LoogeZZZ/pochinka"
	ReleaseURL = "https://github.com/" + Repo + "/releases/"

	// PubKey — открытый ключ подписи релизов (Ed25519). Закрытый — только у
	// автора, вне репозитория (tools/signer). Починка работает с правами
	// администратора, поэтому ставит только exe с верной подписью: подмена
	// файла на GitHub или по дороге не пройдёт, а контрольная сумма с той же
	// страницы от этого не защищает.
	PubKey = "Rtz+zm+j7AwzNdCpOsYHFCHc4XBD+4x2WcxAlHlsNrk="

	maxSize = 200 << 20
)

// Release — версия новее текущей. Пустая Version — обновлений нет.
type Release struct {
	Version string // "1.0.2"
	URL     string // страница релиза
	Tag     string // "v1.0.2"
}

var tagRe = regexp.MustCompile(`^v?\d+(\.\d+){1,3}$`)

// ValidTag — тег вида v1.2.3 (только такой уходит в ссылки и окно).
func ValidTag(t string) bool { return tagRe.MatchString(t) }

// latest — тег и страница последнего релиза. Сначала API, при лимите или
// блокировке — редирект /releases/latest (у него лимита нет).
func latest(ctx context.Context, ua string) (tag, page string, err error) {
	cl := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", ua)
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
	req, _ = http.NewRequestWithContext(ctx, "HEAD", "https://github.com/"+Repo+"/releases/latest", nil)
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

// Check — есть ли релиз новее current. Тег и ссылку проверяем: дальше идут
// только версия вида 1.2.3 и только страница нашего репозитория.
func Check(ctx context.Context, current string) (Release, error) {
	tag, page, err := latest(ctx, "Pochinka/"+current)
	if err != nil {
		return Release{}, err
	}
	if !ValidTag(tag) {
		return Release{}, errors.New("странный тег релиза")
	}
	if !strings.HasPrefix(page, ReleaseURL) {
		page = ReleaseURL + "latest"
	}
	if zapret.CompareVersions(tag, current) <= 0 {
		return Release{}, nil
	}
	return Release{Version: strings.TrimPrefix(tag, "v"), URL: page, Tag: tag}, nil
}

// Download скачивает exe и подпись релиза tag в dir и проверяет подпись.
// Возвращает путь к exe; при неверной подписи файл удаляется.
func Download(ctx context.Context, tag, dir, ua string, onPct func(int)) (string, error) {
	if !ValidTag(tag) {
		return "", errors.New("странный тег релиза")
	}
	ver := strings.TrimPrefix(tag, "v")
	base := "https://github.com/" + Repo + "/releases/download/" + tag + "/Pochinka-" + ver + ".exe"
	sig, err := fetch(ctx, base+".sig", 4<<10, nil, ua)
	if err != nil {
		return "", fmt.Errorf("подпись не скачалась: %w", err)
	}
	exe := filepath.Join(dir, "Pochinka-new.exe")
	f, err := os.Create(exe)
	if err != nil {
		return "", err
	}
	_, err = fetch(ctx, base, maxSize, &progressWriter{w: f, onPct: onPct}, ua)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("обновление не скачалось: %w", err)
	}
	if err := Verify(exe, sig); err != nil {
		_ = os.Remove(exe)
		return "", err
	}
	return exe, nil
}

// Verify — подпись Ed25519 над всем файлом.
func Verify(exe string, sigB64 []byte) error {
	pub, _ := base64.StdEncoding.DecodeString(PubKey)
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("подпись обновления повреждена — установка отменена")
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("подпись обновления не сходится — файл подменён или повреждён, установка отменена")
	}
	return nil
}

// fetch скачивает url (только https, редиректы GitHub на его же хранилище)
// в память (w == nil) или в w, не больше limit байт.
func fetch(ctx context.Context, url string, limit int64, w io.Writer, ua string) ([]byte, error) {
	cl := &http.Client{
		Timeout: 15 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) > 5 {
				return errors.New("неожиданный редирект")
			}
			return nil
		},
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", ua)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("файл слишком большой")
	}
	body := io.LimitReader(resp.Body, limit+1)
	if w == nil {
		b, err := io.ReadAll(body)
		if err == nil && int64(len(b)) > limit {
			err = errors.New("файл слишком большой")
		}
		return b, err
	}
	if pw, ok := w.(*progressWriter); ok {
		pw.total = resp.ContentLength
	}
	n, err := io.Copy(w, body)
	if err == nil && n > limit {
		err = errors.New("файл слишком большой")
	}
	return nil, err
}

type progressWriter struct {
	w     io.Writer
	total int64
	done  int64
	last  int
	onPct func(int)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.total > 0 && p.onPct != nil {
		if pct := int(p.done * 100 / p.total); pct >= p.last+2 || pct == 100 {
			p.last = pct
			p.onPct(pct)
		}
	}
	return n, err
}
