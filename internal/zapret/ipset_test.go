package zapret

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseIPSet(t *testing.T) {
	got, n, err := parseIPSet("\xef\xbb\xbf# список\r\n1.2.3.0/24\r\n\r\n5.6.7.8\n2a00:1450::/32\n")
	if err != nil || n != 3 || got != "1.2.3.0/24\n5.6.7.8\n2a00:1450::/32\n" {
		t.Fatalf("%q %d %v", got, n, err)
	}
	for _, bad := range []string{"", "# пусто\n", "1.2.3.0/24\n--wf-tcp=80\n", "<html>404</html>"} {
		if _, _, err := parseIPSet(bad); err == nil {
			t.Fatalf("принят %q", bad)
		}
	}
}

// Режимы фильтра: заглушка, пустой файл («все адреса») и возврат к списку
// из релиза — он сохраняется как .orig при первой правке.
func TestPrepareIPSet(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "ipset-all.txt")
	os.WriteFile(cur, []byte("10.0.0.0/8\n"), 0o644)
	read := func() string { b, _ := os.ReadFile(cur); return string(b) }

	if err := prepareIPSet(dir, IPSetNone); err != nil || read() != ipsetPlaceholder+"\n" {
		t.Fatalf("none: %q %v", read(), err)
	}
	if err := prepareIPSet(dir, IPSetAny); err != nil || read() != "" {
		t.Fatalf("any: %q %v", read(), err)
	}
	if _, err := os.Stat(ipsetFile()); err == nil {
		t.Skip("на машине уже есть скачанный список — режим «по списку» возьмёт его")
	}
	if err := prepareIPSet(dir, IPSetList); err != nil || read() != "10.0.0.0/8\n" {
		t.Fatalf("list: %q %v", read(), err)
	}
}

func TestHasEntries(t *testing.T) {
	if hasEntries("# только комментарий\n\n  \n") || !hasEntries("# a\nexample.com\n") {
		t.Fatal("hasEntries")
	}
}

// LIVE_IPSET=1 — настоящий список из репозитория проходит проверку.
func TestLiveIPSet(t *testing.T) {
	if os.Getenv("LIVE_IPSET") == "" {
		t.Skip("LIVE_IPSET=1")
	}
	resp, err := httpClient.Get(ipsetURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	_, n, err := parseIPSet(string(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("записей: %d", n)
}
