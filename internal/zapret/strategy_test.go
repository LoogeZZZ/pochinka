package zapret

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ZAPRET_TEST_DIR — путь к распакованному релизу zapret-discord-youtube.
func TestParseAllStrategies(t *testing.T) {
	dir := os.Getenv("ZAPRET_TEST_DIR")
	if dir == "" {
		t.Skip("ZAPRET_TEST_DIR не задан")
	}
	list, err := ListStrategies(dir)
	if err != nil || len(list) == 0 {
		t.Fatalf("стратегии не найдены: %v", err)
	}
	tcp, udp := GameFilterPorts("disabled")
	for _, s := range list {
		args, err := ParseBat(s.File, Vars{BatDir: dir, GameFilterTCP: tcp, GameFilterUDP: udp})
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		for _, a := range args {
			if strings.ContainsAny(a, `"^%`) || !strings.HasPrefix(a, "--") {
				t.Errorf("%s: подозрительный аргумент %q", s.Name, a)
			}
		}
		t.Logf("%-28s %d аргументов, первый %s", s.Name, len(args), args[0])
	}
}

func TestSplitArgs(t *testing.T) {
	got := splitArgs(` --a=1  --hostlist="C:\x y\l.txt" --b="q" --fake=^! --c="^x" `)
	want := []string{"--a=1", `--hostlist=C:\x y\l.txt`, "--b=q", "--fake=!", "--c=^x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.10.10", "1.10.3") <= 0 || CompareVersions("1.10.3", "1.10.3") != 0 || CompareVersions("1.9", "1.10.0") >= 0 {
		t.Fatal("сравнение версий сломано")
	}
}

func TestUnzipRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, n := range []string{"root/ok.txt", `root/a\..\..\..\evil1.txt`, "root/../../evil2.txt", "root/C:/evil3.txt"} {
		w, _ := zw.Create(n)
		w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()
	dst := filepath.Join(dir, "out")
	if err := unzipStrip(zp, dst); err != nil {
		t.Fatal(err)
	}
	// Внутри out\ «сплющенный» путь допустим; главное — ничего снаружи.
	for _, bad := range []string{"evil1.txt", "evil2.txt", "evil3.txt"} {
		for _, outside := range []string{filepath.Join(dir, bad), filepath.Join(filepath.Dir(dir), bad), filepath.Join(`C:\`, bad)} {
			if _, err := os.Stat(outside); err == nil {
				t.Fatalf("файл вне папки распаковки: %s", outside)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "ok.txt")); err != nil {
		t.Fatal("нормальный файл не распакован")
	}
	if ValidVersion("..") || ValidVersion(`..\x`) || ValidVersion("a/b") || !ValidVersion("1.10.3") {
		t.Fatal("проверка версии")
	}
}
