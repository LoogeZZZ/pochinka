package selfupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Подпись утилитой signer проверяется вшитым ключом; подменённый файл — нет.
func TestVerify(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "x.exe")
	os.WriteFile(exe, []byte("pretend exe"), 0o644)
	if out, err := exec.Command("go", "run", "../../tools/signer", "sign", exe).CombinedOutput(); err != nil {
		t.Skipf("нет ключа подписи на этой машине: %s", out)
	}
	sig, _ := os.ReadFile(exe + ".sig")
	if err := Verify(exe, sig); err != nil {
		t.Fatalf("верная подпись не прошла: %v", err)
	}
	os.WriteFile(exe, []byte("tampered exe"), 0o644)
	if err := Verify(exe, sig); err == nil {
		t.Fatal("подменённый файл прошёл проверку")
	}
	_, other, _ := ed25519.GenerateKey(nil)
	bad := base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte("tampered exe")))
	if err := Verify(exe, []byte(bad)); err == nil {
		t.Fatal("чужая подпись прошла проверку")
	}
	if err := Verify(exe, []byte("мусор")); err == nil {
		t.Fatal("мусор вместо подписи прошёл")
	}
}

// LIVE_UPDATE=1 — настоящий GitHub: проверка версии, скачивание и подпись.
func TestLive(t *testing.T) {
	if os.Getenv("LIVE_UPDATE") == "" {
		t.Skip("LIVE_UPDATE=1")
	}
	r, err := Check(context.Background(), "0.0.1")
	if err != nil || r.Tag == "" {
		t.Fatalf("%+v %v", r, err)
	}
	if none, _ := Check(context.Background(), "999.0.0"); none.Version != "" {
		t.Fatalf("для будущей версии нашлось обновление: %+v", none)
	}
	exe, err := Download(context.Background(), r.Tag, t.TempDir(), "Pochinka/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(exe)
	t.Logf("%s: %d байт, подпись верна", r.Tag, st.Size())
}
