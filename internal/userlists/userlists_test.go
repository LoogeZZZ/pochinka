package userlists

import (
	"os"
	"strings"
	"testing"

	"pochinka/internal/paths"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("ProgramData", t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveDomains(t *testing.T) {
	isolate(t)
	l, _ := Get("vpn-sites")
	n, err := l.Save("# мой список\r\nhttps://Example.COM/path?x=1\n*.sub.site.ru\n^exact.org\n\nметро.рф\n")
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := l.Read()
	want := "# мой список\nexample.com\nsub.site.ru\n^exact.org\n\nметро.рф\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestSaveRejects(t *testing.T) {
	isolate(t)
	l, _ := Get("zapret-include")
	for _, bad := range []string{"rm -rf /", "a..b", "-x.com", "домен с пробелом", "x.com\x00evil"} {
		if _, err := l.Save("ok.com\n" + bad + "\n"); err == nil || !strings.Contains(err.Error(), "строка 2") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	ip, _ := Get("zapret-ipexclude")
	if _, err := ip.Save("1.2.3.4\n10.0.0.1/8\n::1\nexample.com\n"); err == nil || !strings.Contains(err.Error(), "строка 4") {
		t.Fatalf("ip: %v", err)
	}
	if _, err := ip.Save("1.2.3.4\n10.0.0.1/8\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := ip.Read()
	if got != "1.2.3.4\n10.0.0.0/8\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureDefault(t *testing.T) {
	isolate(t)
	l, _ := Get("zapret-exclude")
	txt, err := l.Read()
	if err != nil || !strings.HasPrefix(txt, "#") {
		t.Fatalf("%q %v", txt, err)
	}
	if _, err := os.Stat(l.Path()); err != nil {
		t.Fatal(err)
	}
}

func TestSaveMixed(t *testing.T) {
	isolate(t)
	l, _ := Get("vpn-direct")
	n, err := l.Save("bank.ru\n10.0.0.1/8\nSteam.exe\nmy app (x64).exe\n")
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := l.Read()
	if got != "bank.ru\n10.0.0.0/8\nSteam.exe\nmy app (x64).exe\n" {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{`C:\x\evil.exe`, "a/b.exe", ".exe", "x;y.exe"} {
		if _, err := l.Save(bad + "\n"); err == nil {
			t.Errorf("пропущено %q", bad)
		}
	}
}

func TestPatterns(t *testing.T) {
	isolate(t)
	v, _ := Get("vpn-sites")
	in := "yandex.*\n*.yandex.*\n" + `regexp:^a\d+\.ru$` + "\n" + `/x\.(ru|com)$/` + "\nkeyword:Yandex\n*.mail.ru\n"
	if _, err := v.Save(in); err != nil {
		t.Fatal(err)
	}
	got, _ := v.Read()
	want := "yandex.*\n*.yandex.*\n" + `regexp:^a\d+\.ru$` + "\n" + `regexp:x\.(ru|com)$` + "\nkeyword:yandex\nmail.ru\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"regexp:(", "keyword:", "**", "a b*.ru"} {
		if _, err := v.Save(bad + "\n"); err == nil || !strings.Contains(err.Error(), "строка 1") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	z, _ := Get("zapret-include")
	if _, err := z.Save("yandex.*\n"); err == nil || !strings.Contains(err.Error(), "zapret") {
		t.Fatalf("в списке обхода шаблон должен отклоняться с пояснением: %v", err)
	}
	if _, err := z.Save("*.yandex.ru\n"); err != nil {
		t.Fatalf("«*.» в начале в списке обхода можно: %v", err)
	}
}
