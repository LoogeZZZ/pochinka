package zapret

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Сверка с настоящим cmd.exe. ZAPRET_CMP_DIR — копия релиза, где
// bin\winws.exe подменён программой, пишущей свои аргументы в $ECHO_OUT.
func TestParseMatchesCmd(t *testing.T) {
	dir := os.Getenv("ZAPRET_CMP_DIR")
	if dir == "" {
		t.Skip("ZAPRET_CMP_DIR не задан")
	}
	list, err := ListStrategies(dir)
	if err != nil {
		t.Fatal(err)
	}
	tcp, udp := GameFilterPorts("disabled")
	for _, s := range list {
		tmp := t.TempDir()
		out := filepath.Join(tmp, "args.json")
		wrapper := filepath.Join(tmp, "run.bat")
		_ = os.WriteFile(wrapper, []byte("@call \""+s.File+"\"\r\n"), 0o644)
		cmd := exec.Command("cmd.exe", "/c", wrapper)
		cmd.Dir = dir
		var env []string
		for _, e := range os.Environ() {
			// С этой переменной cmd не ищет service.bat в текущей папке.
			if !strings.HasPrefix(strings.ToUpper(e), "NODEFAULTCURRENTDIRECTORYINEXEPATH=") {
				env = append(env, e)
			}
		}
		cmd.Env = append(env, "ECHO_OUT="+out, "NO_UPDATE_CHECK=1")
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: cmd: %v\n%s", s.Name, err, o)
			continue
		}
		var want []string
		for i := 0; i < 50; i++ {
			if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
				_ = json.Unmarshal(b, &want)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		got, err := ParseBat(s.File, Vars{BatDir: dir, GameFilterTCP: tcp, GameFilterUDP: udp})
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			for i := 0; i < len(got) || i < len(want); i++ {
				var g, w string
				if i < len(got) {
					g = got[i]
				}
				if i < len(want) {
					w = want[i]
				}
				if g != w {
					t.Errorf("%s: аргумент %d:\n  наш: %q\n  cmd: %q", s.Name, i, g, w)
					break
				}
			}
			continue
		}
		t.Logf("%-28s совпадает с cmd.exe (%d аргументов)", s.Name, len(got))
	}
}
