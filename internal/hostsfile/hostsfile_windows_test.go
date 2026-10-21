package hostsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Из скачанного берутся только разрешённые домены: подменить адрес банка
// через этот файл нельзя.
func TestParse(t *testing.T) {
	got, err := Parse("# комментарий\r\n" +
		"149.154.167.220 web.telegram.org t.me # хвост\n" +
		"162.159.138.232 discord.com\n" +
		"162.159.138.232 discord.com\n" +
		"1.2.3.4 sberbank.ru\n" +
		"1.2.3.4 evil-discord.com\n" +
		"0.0.0.0 discord.gg\n" +
		"мусор\n" +
		"2606:50c0:8000::154 raw.githubusercontent.com\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"149.154.167.220 web.telegram.org", "149.154.167.220 t.me", "162.159.138.232 discord.com", "2606:50c0:8000::154 raw.githubusercontent.com"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if _, err := Parse("1.2.3.4 example.com\n"); err == nil {
		t.Fatal("файл без разрешённых доменов принят")
	}
}

// Блок ставится, заменяется и убирается, не трогая чужие строки и не
// дублируя уже прописанные руками.
func TestWriteBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	orig := "# Windows hosts\r\n127.0.0.1 my.local\r\n162.159.138.232 discord.com\r\n"
	os.WriteFile(p, []byte(orig), 0o644)

	if err := writeTo(p, []string{"162.159.138.232 discord.com", "149.154.167.220 t.me"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "127.0.0.1 my.local") || strings.Count(s, "discord.com") != 1 || !strings.Contains(s, beginMark+"\r\n149.154.167.220 t.me\r\n"+endMark) {
		t.Fatalf("после записи:\n%s", s)
	}

	if err := writeTo(p, []string{"149.154.167.221 t.me"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if strings.Count(string(b), beginMark) != 1 || strings.Contains(string(b), "149.154.167.220") {
		t.Fatalf("блок не заменился:\n%s", b)
	}

	if err := writeTo(p, nil); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != orig {
		t.Fatalf("после удаления блока hosts не вернулся:\n%q", b)
	}
}

// LIVE_HOSTS=1 — настоящий файл из репозитория разбирается (hosts не трогаем).
func TestLiveParse(t *testing.T) {
	if os.Getenv("LIVE_HOSTS") == "" {
		t.Skip("LIVE_HOSTS=1")
	}
	text, err := download(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("записей: %d, первая: %s", len(got), got[0])
}
