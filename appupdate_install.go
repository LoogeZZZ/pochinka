package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"pochinka/internal/applog"
	"pochinka/internal/paths"
)

// Открытый ключ подписи релизов (Ed25519). Закрытый — только у автора, вне
// репозитория (tools/signer). Починка работает с правами администратора,
// поэтому ставит только exe с верной подписью: подмена файла на GitHub или
// по дороге не пройдёт, а контрольная сумма с той же страницы от этого не
// защищает.
const updatePubKey = "Rtz+zm+j7AwzNdCpOsYHFCHc4XBD+4x2WcxAlHlsNrk="

const maxUpdateSize = 200 << 20

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
	if !tagRe.MatchString(u.Tag) {
		return errors.New("странный тег релиза")
	}
	ver := strings.TrimPrefix(u.Tag, "v")
	base := "https://github.com/" + appRepo + "/releases/download/" + u.Tag + "/Pochinka-" + ver + ".exe"

	dir := filepath.Join(paths.DataDir(), "update") // только для администраторов
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()

	sigB, err := fetch(ctx, base+".sig", 4<<10, nil)
	if err != nil {
		return fmt.Errorf("подпись не скачалась: %w", err)
	}
	exePath := filepath.Join(dir, "Pochinka-new.exe")
	f, err := os.Create(exePath)
	if err != nil {
		return err
	}
	_, err = fetch(ctx, base, maxUpdateSize, &progressWriter{w: f, onPct: func(p int) {
		a.mu.Lock()
		a.appUpdate.Progress = p
		a.mu.Unlock()
		a.emitState()
	}})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("обновление не скачалось: %w", err)
	}
	if err := verifyUpdate(exePath, sigB); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	applog.Printf("обновление %s скачано, подпись верна — устанавливаю", ver)

	inst, err := installedExe()
	if err != nil {
		return err
	}
	if err := replaceExe(exePath, inst); err != nil {
		return fmt.Errorf("не удалось заменить программу: %w", err)
	}
	// Новый экземпляр — после выхода этого: иначе он увидит работающий старый
	// (одна копия на компьютер), передаст ему управление и закроется.
	if err := startAfterExit(inst); err != nil {
		return err
	}
	applog.Printf("перезапуск в версию %s", ver)
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	runtime.Quit(a.ctx)
	return nil
}

// verifyUpdate — подпись Ed25519 над всем файлом.
func verifyUpdate(exe string, sigB64 []byte) error {
	pub, _ := base64.StdEncoding.DecodeString(updatePubKey)
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

// replaceExe ставит новый exe на место установленного. Запущенный файл
// перезаписать нельзя, но переименовать можно — старый уходит в .old и
// удаляется при следующем запуске (syncInstalledCopy).
func replaceExe(src, dst string) error {
	_ = os.Remove(dst + ".old")
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, dst+".old"); err != nil {
			return err
		}
	}
	if err := copyFile(src, dst); err != nil {
		_ = os.Rename(dst+".old", dst)
		return err
	}
	_ = os.Remove(src)
	return nil
}

// startAfterExit запускает exe через пару секунд, когда эта копия закроется.
func startAfterExit(exe string) error {
	if !safeExePath(exe) {
		return fmt.Errorf("странный путь %q", exe)
	}
	cmd := exec.Command(filepath.Join(systemDir(), "cmd.exe"))
	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
		CmdLine:       `cmd.exe /d /c "ping 127.0.0.1 -n 4 >nul & start "" "` + exe + `""`,
	}
	return cmd.Start()
}

func safeExePath(p string) bool {
	return strings.EqualFold(filepath.Base(p), "Pochinka.exe") && !strings.ContainsAny(p, `"&|<>^%`)
}

// fetch скачивает url (только https, редиректы GitHub на его же хранилище)
// в память (w == nil) или в w, не больше limit байт.
func fetch(ctx context.Context, url string, limit int64, w io.Writer) ([]byte, error) {
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
	req.Header.Set("User-Agent", "Pochinka/"+appVersion)
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
	if p.total > 0 {
		if pct := int(p.done * 100 / p.total); pct >= p.last+2 || pct == 100 {
			p.last = pct
			p.onPct(pct)
		}
	}
	return n, err
}
