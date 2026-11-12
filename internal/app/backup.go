package app

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"pochinka/internal/applog"
	"pochinka/internal/config"
	"pochinka/internal/install"
	"pochinka/internal/paths"
)

// BackupResult - ответ окну на экспорт/импорт резервной копии.
type BackupResult struct {
	Error string `json:"error"`
}

// backupMaxEntry - 8 МБ на файл, 64 МБ всего, 200 файлов: наша копия -
// конфиг, подписки и списки, больше там не бывает. Ограничения - защита от
// случайно выбранного чужого архива.
const (
	backupMaxEntries = 200
	backupMaxEntry   = 8 << 20
	backupMaxTotal   = 64 << 20
)

// backupFiles - файлы, которые попадают в копию: config.json, подписки и
// свои списки. Журналы и скачанный zapret не копируем - они большие и
// восстанавливаются сами.
func backupFiles() []struct{ name, path string } {
	var out []struct{ name, path string }
	out = append(out, struct{ name, path string }{"config.json", paths.ConfigFile()})
	if subs, _ := filepath.Glob(filepath.Join(paths.DataDir(), "vpn-subs", "*.json")); subs != nil {
		for _, s := range subs {
			out = append(out, struct{ name, path string }{"vpn-subs/" + filepath.Base(s), s})
		}
	}
	if users, _ := filepath.Glob(filepath.Join(paths.UserDir(), "*")); users != nil {
		for _, u := range users {
			if st, err := os.Stat(u); err == nil && !st.IsDir() {
				out = append(out, struct{ name, path string }{"user/" + filepath.Base(u), u})
			}
		}
	}
	return out
}

// exportZip собирает резервную копию в dst. При ошибке файл удаляется.
func exportZip(dst string) error {
	files := backupFiles()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(dst)
		}
	}()
	zw := zip.NewWriter(f)
	for _, e := range files {
		b, err := os.ReadFile(e.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

// ExportBackup - кнопка «Создать резервную копию»: путь выбран в окне.
func (a *App) ExportBackup(path string) BackupResult {
	path = strings.TrimSpace(path)
	if path == "" {
		return BackupResult{Error: "не указан путь"}
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	if err := exportZip(path); err != nil {
		applog.Printf("резервная копия: %v", err)
		return BackupResult{Error: fmt.Sprintf("не удалось сохранить: %v", err)}
	}
	applog.Printf("резервная копия сохранена: %s", path)
	return BackupResult{}
}

// safeBackupName проверяет имя файла из архива: только наши три корня,
// без «..», обратных слэшей и абсолютных путей (защита от zip-slip).
func safeBackupName(name string) bool {
	if name == "config.json" {
		return true
	}
	for _, root := range []string{"vpn-subs/", "user/"} {
		if strings.HasPrefix(name, root) {
			rest := name[len(root):]
			if rest == "" || rest == "." || rest == ".." {
				return false
			}
			return rest == filepath.Base(rest) && !strings.Contains(rest, "\\")
		}
	}
	return false
}

// ImportBackup - кнопка «Восстановить из копии»: заменяет настройки,
// подписки и списки содержимым архива и перезапускает программу. Перед
// заменой текущее состояние сохраняется рядом (restore-backup.zip).
func (a *App) ImportBackup(path string) BackupResult {
	path = strings.TrimSpace(path)
	if path == "" {
		return BackupResult{Error: "не указан путь"}
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return BackupResult{Error: "это не архив: " + err.Error()}
	}
	defer zr.Close()
	if len(zr.File) == 0 || len(zr.File) > backupMaxEntries {
		return BackupResult{Error: "архив не похож на резервную копию Починки"}
	}

	stage := filepath.Join(paths.DataDir(), "restore-stage")
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return BackupResult{Error: err.Error()}
	}
	defer os.RemoveAll(stage)

	var total uint64
	haveConfig := false
	for _, f := range zr.File {
		if !safeBackupName(f.Name) {
			return BackupResult{Error: "в архиве неожиданный файл: " + f.Name}
		}
		if f.UncompressedSize64 > backupMaxEntry || total+f.UncompressedSize64 > backupMaxTotal {
			return BackupResult{Error: "архив слишком большой"}
		}
		total += f.UncompressedSize64
		if f.Name == "config.json" {
			haveConfig = true
		}
		dst := filepath.Join(stage, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return BackupResult{Error: err.Error()}
		}
		rc, err := f.Open()
		if err != nil {
			return BackupResult{Error: err.Error()}
		}
		w, err := os.Create(dst)
		if err == nil {
			_, err = io.Copy(w, io.LimitReader(rc, backupMaxEntry+1))
		}
		rc.Close()
		if w != nil {
			w.Close()
		}
		if err != nil {
			return BackupResult{Error: err.Error()}
		}
	}
	if !haveConfig {
		return BackupResult{Error: "в архиве нет config.json - это не копия Починки"}
	}
	// Конфиг должен быть настоящим JSON с нашими полями.
	b, err := os.ReadFile(filepath.Join(stage, "config.json"))
	if err != nil {
		return BackupResult{Error: err.Error()}
	}
	var c config.Config
	if err := json.Unmarshal(b, &c); err != nil {
		return BackupResult{Error: "в архиве повреждённый config.json"}
	}

	// Текущее состояние - рядом, на случай, если копия не подойдёт.
	if err := exportZip(filepath.Join(paths.DataDir(), "restore-backup.zip")); err != nil {
		applog.Printf("резервная копия: не удалось сохранить текущее состояние перед восстановлением: %v", err)
	}
	// Замена: старые подписки и списки убираем целиком, потом раскладываем
	// файлы из архива по местам.
	_ = os.RemoveAll(filepath.Join(paths.DataDir(), "vpn-subs"))
	oldUsers, _ := filepath.Glob(filepath.Join(paths.UserDir(), "*"))
	for _, u := range oldUsers {
		if st, err := os.Stat(u); err == nil && !st.IsDir() {
			_ = os.Remove(u)
		}
	}
	err = filepath.WalkDir(stage, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(stage, p)
		rel = filepath.FromSlash(rel)
		dst := filepath.Join(paths.DataDir(), rel)
		// Подписки содержат ссылки с токенами - папка только для
		// администраторов, как и при обычном сохранении.
		mode := os.FileMode(0o755)
		if strings.HasPrefix(rel, "vpn-subs") {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(dst), mode); err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeAtomicFile(dst, b)
	})
	if err != nil {
		return BackupResult{Error: "не удалось восстановить: " + err.Error()}
	}
	applog.Printf("настройки восстановлены из копии - перезапускаюсь")
	inst, err := install.InstalledExe()
	if err == nil {
		err = install.StartAfterExit(inst)
	}
	if err != nil {
		return BackupResult{Error: "настройки восстановлены, но не удалось перезапустить программу - перезапустите её сами: " + err.Error()}
	}
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	runtime.Quit(a.ctx)
	return BackupResult{}
}

// writeAtomicFile - запись во временный файл с переименованием поверх
// (как config.Save), чтобы подменённый заранее файл не прочитался.
func writeAtomicFile(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), "restore-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
