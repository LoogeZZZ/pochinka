package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LIVE_UPDATE=1 — настоящий GitHub.
func TestCheckAppUpdateLive(t *testing.T) {
	if os.Getenv("LIVE_UPDATE") == "" {
		t.Skip("LIVE_UPDATE=1")
	}
	tag, page, err := latestAppRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("последний релиз: %s %s", tag, page)
	u, err := checkAppUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("для версии %s: %+v", appVersion, u)
}

// LIVE_UPDATE=1 — скачать последний релиз по тем же адресам, что и
// «Обновить», и проверить подпись (без установки).
func TestDownloadAndVerifyLive(t *testing.T) {
	if os.Getenv("LIVE_UPDATE") == "" {
		t.Skip("LIVE_UPDATE=1")
	}
	tag, _, err := latestAppRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ver := strings.TrimPrefix(tag, "v")
	base := "https://github.com/" + appRepo + "/releases/download/" + tag + "/Pochinka-" + ver + ".exe"
	sig, err := fetch(context.Background(), base+".sig", 4<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "new.exe")
	f, _ := os.Create(p)
	_, err = fetch(context.Background(), base, maxUpdateSize, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyUpdate(p, sig); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	t.Logf("%s: %d байт, подпись верна", tag, st.Size())
}
