package main

import (
	"regexp"
	"strconv"

	"github.com/wailsapp/wails/v2/pkg/options"

	"pochinka/internal/config"
)

// Темы: system | light | dark (style.css), готовые (frontend/dist/themes.js)
// и custom — своя палитра из редактора. Здесь только то, что нужно Go:
// какие имена допустимы и каким цветом залить окно до загрузки страницы.

// themeBackgrounds — фон готовых тем (совпадает с bg в themes.js).
var themeBackgrounds = map[string]string{
	"dracula": "#21222c",
	"nord":    "#2e3440",
	"ubuntu":  "#2c001e",
	"github":  "#f6f8fa",
	"iceberg": "#161821",
	"monokai": "#1e1f1c",
}

var themeKeys = []string{"bg", "surface", "text", "muted", "accent", "on", "bad", "warn"}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validTheme(name string) bool {
	switch name {
	case "system", "light", "dark", "custom":
		return true
	}
	_, ok := themeBackgrounds[name]
	return ok
}

// validPalette — своя тема: ровно известные ключи, цвета только #rrggbb
// (значения попадают в CSS — никаких выражений и url()).
func validPalette(p map[string]string) bool {
	if p == nil || (p["scheme"] != "light" && p["scheme"] != "dark") {
		return false
	}
	if len(p) != len(themeKeys)+1 {
		return false
	}
	for _, k := range themeKeys {
		if !hexColor.MatchString(p[k]) {
			return false
		}
	}
	return true
}

// SetTheme: system | light | dark | dracula | nord | ubuntu | github |
// iceberg | monokai | custom.
func (a *App) SetTheme(theme string) {
	if !validTheme(theme) {
		return
	}
	_, _ = config.Update(func(c *config.Config) { c.Theme = theme })
	a.emitState()
}

// SetCustomTheme сохраняет свою палитру и включает её.
func (a *App) SetCustomTheme(p map[string]string) string {
	if !validPalette(p) {
		return "неверные цвета темы"
	}
	cp := make(map[string]string, len(p))
	for k, v := range p {
		cp[k] = v
	}
	_, _ = config.Update(func(c *config.Config) { c.CustomTheme, c.Theme = cp, "custom" })
	a.emitState()
	return ""
}

func hexToRGBA(h string) *options.RGBA {
	if !hexColor.MatchString(h) {
		return nil
	}
	v, _ := strconv.ParseUint(h[1:], 16, 32)
	return &options.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

// themeBackground — фон выбранной темы; nil — тема светлая/тёмная/системная.
func themeBackground(c config.Config) *options.RGBA {
	if bg, ok := themeBackgrounds[c.Theme]; ok {
		return hexToRGBA(bg)
	}
	if c.Theme == "custom" && validPalette(c.CustomTheme) {
		return hexToRGBA(c.CustomTheme["bg"])
	}
	return nil
}
