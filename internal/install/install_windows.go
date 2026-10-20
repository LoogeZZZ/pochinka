// Package install — установка в Program Files, запуск без UAC, автозапуск,
// удаление и замена exe при обновлении.
package install

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"pochinka/internal/applog"
	"pochinka/internal/paths"
)

// Установка, запуск без UAC и автозапуск.
//
// Починке нужны права администратора (WinDivert, TUN), но запрашивать UAC
// при каждом запуске незачем. Делаем как обычные «установленные» программы:
//
//   - exe копируется в Program Files — туда без прав администратора не
//     записать, значит, и не подменить;
//   - в Планировщике регистрируется задача «PochinkaOpen» с наивысшими
//     правами, которая запускает эту копию. Пользователь может её запускать,
//     менять — только администратор;
//   - обычный запуск (exe без прав) просто просит Планировщик выполнить
//     задачу и выходит. UAC — только при первом запуске и при обновлении
//     (новый exe отличается от копии в Program Files);
//   - автозапуск — вторая задача «Pochinka» на вход в Windows.
const (
	TaskAutostart = "Pochinka"
	TaskOpen      = "PochinkaOpen"
)

// InstalledExe — путь копии (см. paths.InstalledExe).
func InstalledExe() (string, error) { return paths.InstalledExe() }

func SystemDir() string {
	d, err := windows.GetSystemDirectory()
	if err != nil {
		return `C:\Windows\System32`
	}
	return d
}

func Schtasks(args ...string) ([]byte, error) {
	cmd := exec.Command(filepath.Join(SystemDir(), "Schtasks.exe"), args...)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.CombinedOutput()
}

func TaskExists(name string) bool {
	_, err := Schtasks("/Query", "/TN", name)
	return err == nil
}

func AutostartEnabled() bool { return TaskExists(TaskAutostart) }

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// taskXML — описание задачи. Через XML, потому что из командной строки
// не снять ограничения по умолчанию (не стартует от батареи, убивается
// через 72 часа) и не задать права на саму задачу.
//
// Права задачи: менять — только SYSTEM и администраторы, читать и
// запускать — этот пользователь (без этого обычный запуск не смог бы её
// выполнить).
func taskXML(exe, args, sid string, logon bool) string {
	triggers := "<Triggers/>"
	policy := "Parallel" // окно «открыть»: второй экземпляр сам передаст управление первому
	if logon {
		triggers = `<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + sid + `</UserId><Delay>PT5S</Delay></LogonTrigger></Triggers>`
		policy = "IgnoreNew"
	}
	argXML := ""
	if args != "" {
		argXML = "<Arguments>" + xmlEscape(args) + "</Arguments>"
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Починка</Description>
    <SecurityDescriptor>D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GRGX;;;` + sid + `)</SecurityDescriptor>
  </RegistrationInfo>
  ` + triggers + `
  <Principals>
    <Principal id="Author"><UserId>` + sid + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>` + policy + `</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>4</Priority>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>` + xmlEscape(exe) + `</Command>` + argXML + `</Exec>
  </Actions>
</Task>
`
}

func currentUserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// registerTask. XML пишется в папку данных (только администраторы), а не
// во временную: там его успели бы подменить между записью и регистрацией —
// и задача с правами администратора запускала бы чужую программу.
func registerTask(name, xmlText string) error {
	u := utf16.Encode([]rune(xmlText))
	buf := []byte{0xFF, 0xFE} // Schtasks ждёт UTF-16 с BOM
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	f, err := os.CreateTemp(paths.DataDir(), "task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(buf)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if out, err := Schtasks("/Create", "/TN", name, "/XML", f.Name(), "/F"); err != nil {
		return fmt.Errorf("планировщик: %s", bytes.TrimSpace(out))
	}
	return nil
}

// syncInstalledCopy кладёт в Program Files копию запущенного exe, если
// там другой файл (или его нет). Запущенную копию Windows перезаписать не
// даёт, но переименовать разрешает — старую убираем в .old.
func syncInstalledCopy() (string, error) {
	dst, err := InstalledExe()
	if err != nil {
		return "", err
	}
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	_ = os.Remove(dst + ".old") // хвост прошлого обновления
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) || sameFile(src, dst) {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".new"
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, dst+".old"); err != nil {
			os.Remove(tmp)
			return "", err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(dst+".old", dst)
		return "", err
	}
	_ = os.Remove(dst + ".old")
	applog.Printf("установка: копия обновлена в %s", dst)
	return dst, nil
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil || fa.Size() != fb.Size() {
		return false
	}
	ba, err1 := os.ReadFile(a)
	bb, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(ba, bb)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// EnsureInstalled — при каждом запуске с правами: копия в Program Files
// = этой версии, задача «открыть без UAC» есть. Задачи указывают на
// неизменный путь копии, поэтому после обновления их перерегистрировать
// не нужно.
func EnsureInstalled() bool {
	exe, err := syncInstalledCopy()
	if err != nil {
		applog.Printf("установка: не удалось обновить копию: %v", err)
		return false
	}
	if TaskExists(TaskOpen) {
		return true
	}
	sid, err := currentUserSID()
	if err == nil {
		err = registerTask(TaskOpen, taskXML(exe, "", sid, false))
	}
	if err != nil {
		applog.Printf("установка: запуск без UAC не настроен: %v", err)
		return false
	}
	applog.Printf("установка: дальше программа запускается без запроса UAC")
	return true
}

func EnableAutostart() error {
	exe, err := syncInstalledCopy()
	if err != nil {
		return fmt.Errorf("копия в Program Files: %w", err)
	}
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	return registerTask(TaskAutostart, taskXML(exe, "--minimized", sid, true))
}

func DisableAutostart() error {
	if !AutostartEnabled() {
		return nil
	}
	if out, err := Schtasks("/Delete", "/TN", TaskAutostart, "/F"); err != nil {
		return fmt.Errorf("планировщик: %s", bytes.TrimSpace(out))
	}
	return nil
}

// ---- запуск без прав ----

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	allowSetForegroundWindowFn = user32.NewProc("AllowSetForegroundWindow")
)

// LaunchElevated — exe запущен без прав администратора. Если установленная
// копия совпадает с этим exe — открываем её через задачу (без UAC). Иначе
// (первый запуск, новая версия) — один раз спрашиваем UAC: запущенная с
// правами копия сама обновит Program Files и задачу. Не возвращается.
func LaunchElevated() {
	self, _ := os.Executable()
	// Через задачу — только обычный запуск: аргументы (--diag-tun) она не передаёт.
	if inst, err := InstalledExe(); err == nil && len(os.Args) == 1 && sameFile(self, inst) {
		// Окно, открытое Планировщиком, иначе может остаться позади.
		const asfwAny = ^uintptr(0)
		allowSetForegroundWindowFn.Call(asfwAny)
		if _, err := Schtasks("/Run", "/TN", TaskOpen); err == nil {
			os.Exit(0)
		}
	}
	var args []string
	for _, a := range os.Args[1:] {
		args = append(args, syscall.EscapeArg(a))
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(self)
	params, _ := windows.UTF16PtrFromString(strings.Join(args, " "))
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(self))
	_ = windows.ShellExecute(0, verb, file, params, dir, windows.SW_SHOWNORMAL)
	os.Exit(0)
}

// HardenEnv — процесс с правами администратора, а переменные окружения
// пользователь задаёт сам (HKCU\Environment) — и они достаются нам и при
// запуске через Планировщик, без UAC. Через них можно было бы подменить
// папку данных (ProgramData), системные программы (PATH, windir), движок
// WebView2 или добавить ему флаги (WEBVIEW2_*). Ставим системные значения.
func HardenEnv() {
	sys := SystemDir()
	win, err := windows.GetWindowsDirectory()
	if err != nil {
		win = filepath.Dir(sys)
	}
	for k, v := range map[string]string{
		"SystemRoot": win, "windir": win,
		"ComSpec": filepath.Join(sys, "cmd.exe"),
		"PATH":    sys + ";" + win + ";" + filepath.Join(sys, "Wbem") + ";" + filepath.Join(sys, `WindowsPowerShell\v1.0`),
	} {
		_ = os.Setenv(k, v)
	}
	if pd, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil {
		_ = os.Setenv("ProgramData", pd)
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(k)
		if strings.HasPrefix(u, "WEBVIEW2_") || strings.HasPrefix(u, "COREWEBVIEW2") ||
			strings.HasPrefix(u, "GODEBUG") || strings.HasSuffix(u, "_PROXY") ||
			u == "SSLKEYLOGFILE" || strings.HasPrefix(u, "XRAY_") {
			_ = os.Unsetenv(k)
		}
	}
}

// ---- удаление и обновление ----

// RemoveAfterExit запускает скрытый cmd, который ждёт выхода программы и
// удаляет папки. Пути берутся только из системных папок (не из окружения и
// не от пользователя), поэтому подставлять их в команду безопасно.
func RemoveAfterExit(dirs []string) error {
	var b strings.Builder
	// ~4 секунды на выход программы и WebView2.
	b.WriteString("ping 127.0.0.1 -n 5 >nul")
	for _, d := range dirs {
		if !safeToRemove(d) {
			return fmt.Errorf("странный путь %q", d)
		}
		fmt.Fprintf(&b, ` & rd /s /q "%s"`, d)
	}
	cmd := exec.Command(filepath.Join(SystemDir(), "cmd.exe"))
	// Командную строку целиком: cmd по-своему разбирает кавычки.
	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
		CmdLine:       `cmd.exe /d /c "` + b.String() + `"`,
	}
	return cmd.Start()
}

// safeToRemove — последний рубеж: удаляем только папки с «Pochinka» в имени
// и не корень диска, что бы ни случилось с путями выше.
func safeToRemove(d string) bool {
	d = filepath.Clean(d)
	base := strings.ToLower(filepath.Base(d))
	return len(d) > 10 && strings.Contains(base, "pochinka") && !strings.ContainsAny(d, `"&|<>^%`)
}

// ReplaceExe ставит новый exe на место установленного. Запущенный файл
// перезаписать нельзя, но переименовать можно — старый уходит в .old и
// удаляется при следующем запуске (syncInstalledCopy).
func ReplaceExe(src, dst string) error {
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

// StartAfterExit запускает exe через пару секунд, когда эта копия закроется.
func StartAfterExit(exe string) error {
	if !safeExePath(exe) {
		return fmt.Errorf("странный путь %q", exe)
	}
	cmd := exec.Command(filepath.Join(SystemDir(), "cmd.exe"))
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
