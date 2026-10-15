//go:build windows

// Package winutil — обвязка вокруг WinAPI, нужная для управления winws.
package winutil

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Job — job object с KILL_ON_JOB_CLOSE: когда наш процесс завершается (даже
// аварийно), Windows закрывает хэндл и убивает всё, что в job. Так winws не
// остаётся висеть сиротой после краша приложения.
type Job struct{ h windows.Handle }

func NewKillOnCloseJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Job{h: h}, nil
}

func (j *Job) Assign(pid int) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.h, ph)
}

// HideWindow настраивает команду так, чтобы не мелькала консоль.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}

// FindProcesses возвращает PID всех процессов с указанным именем exe.
func FindProcesses(exeName string) ([]uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var pids []uint32
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), exeName) {
			pids = append(pids, pe.ProcessID)
		}
	}
	return pids, nil
}

// ProcessPath — полный путь к exe процесса.
func ProcessPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func KillPID(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return err
	}
	_, _ = windows.WaitForSingleObject(h, 5000)
	return nil
}

// ServiceExists проверяет, установлена ли служба.
func ServiceExists(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// StopService останавливает службу и ждёт до timeout. Отсутствие службы — не ошибка.
func StopService(name string, timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return nil
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == windows.ERROR_SERVICE_NOT_ACTIVE {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err = s.Query()
		if err != nil || st.State == svc.Stopped {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("служба " + name + " не остановилась вовремя")
}

// SetServiceManual переводит службу в ручной запуск, чтобы она не
// конфликтовала с нами после перезагрузки (нужно для чужой службы "zapret").
func SetServiceManual(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return nil
	}
	defer s.Close()
	c, err := s.Config()
	if err != nil {
		return err
	}
	if c.StartType == mgr.StartManual || c.StartType == mgr.StartDisabled {
		return nil
	}
	c.StartType = mgr.StartManual
	return s.UpdateConfig(c)
}

// IsAdmin — запущены ли мы с повышенными правами.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
