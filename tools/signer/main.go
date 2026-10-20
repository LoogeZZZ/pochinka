// signer — подпись релизов Починки (Ed25519).
//
//	go run ./tools/signer keygen          — создать ключ (один раз)
//	go run ./tools/signer sign file.exe   — создать file.exe.sig
//	go run ./tools/signer verify file.exe — проверить подпись вшитым ключом
//
// Закрытый ключ лежит вне репозитория: %USERPROFILE%\.pochinka-release\signing.key.
// Его нельзя терять и нельзя никому отдавать: кто им владеет, тот может
// выпустить «обновление», которое Починка запустит с правами администратора.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func keyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pochinka-release", "signing.key")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("использование: signer keygen | sign <файл> | verify <файл> <публичный ключ base64>")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen()
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		err = errors.New("неизвестная команда")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func keygen() error {
	p := keyPath()
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("ключ уже есть: %s (новый сделает старые подписи бесполезными)", p)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println("закрытый ключ:", p)
	fmt.Println("СДЕЛАЙТЕ РЕЗЕРВНУЮ КОПИЮ этого файла (без него новые версии не подписать).")
	fmt.Println("публичный ключ (вшить в internal/selfupdate/selfupdate.go):", base64.StdEncoding.EncodeToString(pub))
	return nil
}

func loadKey() (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(keyPath())
	if err != nil {
		return nil, fmt.Errorf("нет ключа %s — сначала keygen", keyPath())
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("ключ повреждён")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func sign(args []string) error {
	if len(args) != 1 {
		return errors.New("укажите файл")
	}
	key, err := loadKey()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig := ed25519.Sign(key, data)
	out := args[0] + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("подпись:", out)
	fmt.Println("публичный ключ:", base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
	return nil
}

func verify(args []string) error {
	if len(args) != 2 {
		return errors.New("укажите файл и публичный ключ")
	}
	pub, err := base64.StdEncoding.DecodeString(args[1])
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("неверный публичный ключ")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sb, err := os.ReadFile(args[0] + ".sig")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sb)))
	if err != nil || !ed25519.Verify(pub, data, sig) {
		return errors.New("ПОДПИСЬ НЕ СХОДИТСЯ")
	}
	fmt.Println("подпись верна")
	return nil
}
