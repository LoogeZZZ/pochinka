package main

import (
	"encoding/xml"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTaskXMLWellFormed(t *testing.T) {
	x := taskXML(`C:\Program Files\Pochinka\Pochinka.exe & <x>`, "--minimized", "S-1-5-21-1", true)
	var v struct {
		Actions struct {
			Exec struct{ Command, Arguments string }
		}
	}
	// encoding="UTF-16" в заголовке — сам текст тут в UTF-8, парсим без него.
	if err := xml.Unmarshal([]byte(strings.Replace(x, `encoding="UTF-16"`, "", 1)), &v); err != nil {
		t.Fatal(err)
	}
	if v.Actions.Exec.Command != `C:\Program Files\Pochinka\Pochinka.exe & <x>` || v.Actions.Exec.Arguments != "--minimized" {
		t.Fatalf("%+v", v)
	}
}

// Живая регистрация в Планировщике (без админа — с пониженным уровнем).
func TestTaskRegisterLive(t *testing.T) {
	if os.Getenv("LIVE_TASK") == "" {
		t.Skip("LIVE_TASK=1")
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	x := strings.Replace(taskXML(`C:\Windows\System32\cmd.exe`, "/c exit", sid, false), "HighestAvailable", "LeastPrivilege", 1)
	// Права на задачу (только чтение/запуск для пользователя) без админа не создать.
	x = regexp.MustCompile(`(?s)<SecurityDescriptor>.*?</SecurityDescriptor>`).ReplaceAllString(x, "")
	f, _ := os.CreateTemp("", "task-*.xml")
	buf := []byte{0xFF, 0xFE}
	for _, c := range utf16.Encode([]rune(x)) {
		buf = append(buf, byte(c), byte(c>>8))
	}
	f.Write(buf)
	f.Close()
	defer os.Remove(f.Name())
	if out, err := schtasks("/Create", "/TN", "PochinkaT2", "/XML", f.Name(), "/F"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	defer schtasks("/Delete", "/TN", "PochinkaT2", "/F")
	if out, err := schtasks("/Run", "/TN", "PochinkaT2"); err != nil {
		t.Fatalf("run: %v %s", err, out)
	}
	out, err := schtasks("/Query", "/TN", "PochinkaT2", "/XML")
	if err != nil || !strings.Contains(string(out), "PT0S") {
		t.Fatalf("%v %s", err, out)
	}
}
