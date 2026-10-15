package zapret

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzSplitArgs(f *testing.F) {
	f.Add(`--a=1 --b="x y" --c=^! "unterminated`)
	f.Fuzz(func(t *testing.T, s string) { _ = splitArgs(s) })
}

// Произвольный .bat: разбор не паникует.
func FuzzParseBat(f *testing.F) {
	f.Add("start \"z\" /min \"%BIN%winws.exe\" --a=%LISTS%x ^\n--b=%GameFilterTCP%\n")
	f.Add("winws.exe\" %UNKNOWN% ^\n^\n")
	f.Fuzz(func(t *testing.T, s string) {
		p := filepath.Join(t.TempDir(), "x.bat")
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Skip()
		}
		_, _ = ParseBat(p, Vars{BatDir: `C:\z`, GameFilterTCP: "12", GameFilterUDP: "12"})
	})
}
