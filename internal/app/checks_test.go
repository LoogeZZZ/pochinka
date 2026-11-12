package app

import (
	"strings"
	"testing"
)

func TestSafeBackupName(t *testing.T) {
	good := []string{
		"config.json",
		"vpn-subs/a1.json",
		"vpn-subs/подписка-2.json",
		"user/list.txt",
	}
	bad := []string{
		"",
		"config.json2",
		"config.json/..",
		"../config.json",
		"vpn-subs/",
		"vpn-subs/a/b.json",
		`user\a\b.txt`,
		"logs/pochinka.log",
		"vpn-subs/../config.json",
		"C:/x.json",
		"user/..",
	}
	for _, name := range good {
		if !safeBackupName(name) {
			t.Errorf("должно пройти: %q", name)
		}
	}
	for _, name := range bad {
		if safeBackupName(name) {
			t.Errorf("должно отбраковаться: %q", name)
		}
	}
}

func TestCheckDomain(t *testing.T) {
	good := []string{
		"example.com",
		"twitch.tv",
		"xn--e1afmkfd.xn--p1ai",
		"img.sub.example-site.ru",
		"site_with_underscore.com",
	}
	bad := []string{
		"",
		"example",
		"a..b",
		"https://x.com",
		"x.com/path",
		"space in.name",
		"-.com",
		"x.com?q=1",
		strings.Repeat("a", 254) + ".com",
	}
	for _, d := range good {
		if !checkDomain(d) {
			t.Errorf("домен должен пройти: %q", d)
		}
	}
	for _, d := range bad {
		if checkDomain(d) {
			t.Errorf("домен должен отбраковаться: %q", d)
		}
	}
}
