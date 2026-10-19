package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Подпись утилитой signer проверяется вшитым ключом; подменённый файл — нет.
func TestVerifyUpdate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "x.exe")
	os.WriteFile(exe, []byte("pretend exe"), 0o644)
	if out, err := exec.Command("go", "run", "./tools/signer", "sign", exe).CombinedOutput(); err != nil {
		t.Skipf("нет ключа подписи на этой машине: %s", out)
	}
	sig, _ := os.ReadFile(exe + ".sig")
	if err := verifyUpdate(exe, sig); err != nil {
		t.Fatalf("верная подпись не прошла: %v", err)
	}
	os.WriteFile(exe, []byte("tampered exe"), 0o644)
	if err := verifyUpdate(exe, sig); err == nil {
		t.Fatal("подменённый файл прошёл проверку")
	}
	// Подпись чужим ключом.
	_, other, _ := ed25519.GenerateKey(nil)
	bad := base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte("tampered exe")))
	if err := verifyUpdate(exe, []byte(bad)); err == nil {
		t.Fatal("чужая подпись прошла проверку")
	}
	if err := verifyUpdate(exe, []byte("мусор")); err == nil {
		t.Fatal("мусор вместо подписи прошёл")
	}
}

func TestSafeExePath(t *testing.T) {
	if !safeExePath(`C:\Program Files\Pochinka\Pochinka.exe`) {
		t.Fatal("нормальный путь отклонён")
	}
	for _, p := range []string{`C:\x\evil.exe`, `C:\a" & calc & "\Pochinka.exe`} {
		if safeExePath(p) {
			t.Fatalf("пропущен %s", p)
		}
	}
}
