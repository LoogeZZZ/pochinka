package app

import "testing"

func TestValidPalette(t *testing.T) {
	ok := map[string]string{"scheme": "dark", "bg": "#000000", "surface": "#111111", "text": "#ffffff", "muted": "#888888",
		"accent": "#ff8800", "on": "#00ff00", "bad": "#ff0000", "warn": "#ffaa00"}
	if !validPalette(ok) {
		t.Fatal("нормальная палитра отклонена")
	}
	bad := []map[string]string{
		nil,
		{"scheme": "dark"},
		func() map[string]string { m := copyMap(ok); m["accent"] = "red"; return m }(),
		func() map[string]string { m := copyMap(ok); m["bg"] = "#000;}body{background:url(x)"; return m }(),
		func() map[string]string { m := copyMap(ok); m["evil"] = "#000000"; return m }(),
		func() map[string]string { m := copyMap(ok); m["scheme"] = "neon"; return m }(),
	}
	for i, p := range bad {
		if validPalette(p) {
			t.Errorf("палитра %d пропущена: %v", i, p)
		}
	}
	for _, n := range []string{"system", "light", "dark", "dracula", "nord", "ubuntu", "github", "iceberg", "monokai", "custom"} {
		if !validTheme(n) {
			t.Errorf("тема %s не принята", n)
		}
	}
	if validTheme("solarized") || validTheme("") {
		t.Error("неизвестная тема принята")
	}
}

func copyMap(m map[string]string) map[string]string {
	c := map[string]string{}
	for k, v := range m {
		c[k] = v
	}
	return c
}
