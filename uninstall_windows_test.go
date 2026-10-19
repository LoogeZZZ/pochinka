package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeToRemove(t *testing.T) {
	ok := []string{`C:\Program Files\Pochinka`, `C:\ProgramData\Pochinka`, `C:\Users\x\AppData\Roaming\Pochinka.exe`}
	bad := []string{`C:\`, `C:\Program Files`, `C:\ProgramData`, `C:\Users\x\AppData\Roaming\Other`, `C:\a\Pochinka" & del C:\`, ``}
	for _, d := range ok {
		if !safeToRemove(d) {
			t.Errorf("должно удаляться: %s", d)
		}
	}
	for _, d := range bad {
		if safeToRemove(d) {
			t.Errorf("нельзя удалять: %s", d)
		}
	}
}

// LIVE_UNINSTALL=1 — настоящий отложенный cmd на временных папках.
func TestRemoveAfterExitLive(t *testing.T) {
	if os.Getenv("LIVE_UNINSTALL") == "" {
		t.Skip("LIVE_UNINSTALL=1")
	}
	base := t.TempDir()
	a := filepath.Join(base, "Pochinka test dir")
	b := filepath.Join(base, "Pochinka2")
	for _, d := range []string{a, b} {
		os.MkdirAll(filepath.Join(d, "sub"), 0o755)
		os.WriteFile(filepath.Join(d, "sub", "f.txt"), []byte("x"), 0o644)
	}
	if err := removeAfterExit([]string{a, b}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(8 * time.Second)
	for _, d := range []string{a, b} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("не удалилась: %s", d)
		}
	}
}

func TestPrettyStrategy(t *testing.T) {
	for in, want := range map[string]string{"general": "Стандартная", "general (ALT2)": "ALT2", "general (FAKE TLS AUTO)": "FAKE TLS AUTO", "discord": "discord"} {
		if got := prettyStrategy(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestRankPicks(t *testing.T) {
	in := []PickResult{
		{Name: "a", Discord: true, YouTube: false, Latency: 50},
		{Name: "b", Discord: true, YouTube: true, Latency: 900},
		{Name: "c", Discord: true, YouTube: true, Latency: 300},
		{Name: "d", Discord: false, YouTube: true, Latency: 10},
	}
	got := rankPicks(in)
	order := ""
	for _, r := range got {
		order += r.Name
	}
	if order != "cbda" || countFull(got) != 2 {
		t.Fatalf("порядок %s, полных %d", order, countFull(got))
	}
}
