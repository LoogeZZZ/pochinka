package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Папка данных доступна только SYSTEM и администраторам. По умолчанию в
// %ProgramData% любой пользователь может создавать файлы — а приложение
// работает с правами администратора и запускает оттуда winws.exe и xray,
// то есть подложенный файл дал бы повышение прав.
//
// logs\ — исключение: читать журналы может любой пользователь (секретов
// в них нет — ссылки подписок и ключи вырезаются).
const (
	sddlData = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	sddlLogs = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FR;;;BU)"
)

func applySDDL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil)
}

// emptyDACL — пустой (но не NULL!) список прав. ACLFromEntries(nil, nil)
// для этого не годится: Windows отдаёт NULL, и функция падает.
func emptyDACL() (*windows.ACL, error) {
	sd, err := windows.SecurityDescriptorFromString("D:")
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	if err == nil && dacl == nil {
		err = errors.New("пустой DACL не получен")
	}
	return dacl, err
}

// resetToInherited — владелец «Администраторы», явные права убраны: файл
// получает только права, унаследованные от папки. Так сбрасываются и
// файлы, которые кто-то успел подложить до блокировки.
func resetToInherited(path string, owner *windows.SID, empty *windows.ACL) error {
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, empty, nil)
}

// Secure закрывает папку данных. Вызывается при каждом запуске. Любой
// сбой здесь — только запись в журнал: приложение обязано запуститься.
func Secure() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("сбой при выставлении прав: %v", r)
		}
	}()
	empty, err := emptyDACL()
	if err != nil {
		return err
	}
	if err := applySDDL(DataDir(), sddlData); err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	logs := LogsDir()
	_ = filepath.WalkDir(DataDir(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == DataDir() {
			return nil
		}
		// Подменённые ссылки (junction/symlink) не трогаем и не обходим:
		// через них можно было бы заставить нас сменить права на чужие файлы.
		// WalkDir внутрь ссылок не заходит; SkipDir у файла пропустил бы всю папку.
		if d.Type()&fs.ModeSymlink != 0 || d.Type()&fs.ModeIrregular != 0 {
			_ = os.Remove(p)
			return nil
		}
		if p == logs {
			return nil // отдельные права ниже
		}
		_ = resetToInherited(p, admins, empty)
		return nil
	})
	return applySDDL(logs, sddlLogs)
}

// InstalledExe — установленная копия программы. Program Files берём у
// системы, а не из переменной окружения: её можно подменить.
func InstalledExe() (string, error) {
	pf, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(pf, AppName, AppName+".exe"), nil
}

// RunningInstalled — запущена ли установленная копия (папка, куда без
// прав администратора не записать).
func RunningInstalled() bool {
	inst, err1 := InstalledExe()
	self, err2 := os.Executable()
	return err1 == nil && err2 == nil && strings.EqualFold(filepath.Clean(self), filepath.Clean(inst))
}
