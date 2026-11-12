//go:build windows

package hotkey

import "testing"

func TestParseCombo(t *testing.T) {
	check := func(combo string, wantMods, wantVK uint32) {
		t.Helper()
		mods, vk, err := ParseCombo(combo)
		if err != nil {
			t.Errorf("%s: %v", combo, err)
			return
		}
		if mods != wantMods || vk != wantVK {
			t.Errorf("%s: mods=%#x vk=%#x, хотим mods=%#x vk=%#x", combo, mods, vk, wantMods, wantVK)
		}
	}
	check("Ctrl+Alt+P", modControl|modAlt, 'P')
	check("ctrl+alt+p", modControl|modAlt, 'P')
	check("Shift+F5", modShift, 0x70+4)
	check("Win+1", modWin, '1')
	check("Alt+7", modAlt, '7')
	check("Ctrl+Shift+F12", modControl|modShift, 0x70+11)

	bad := []string{"", "P", "P+Q", "Ctrl+", "Ctrl+P+Q", "Ctrl+Foo", "Ctrl+F0", "Ctrl+F25", "Hyper+P", "Ctrl+ "}
	for _, combo := range bad {
		if _, _, err := ParseCombo(combo); err == nil {
			t.Errorf("%q: ожидали ошибку, её нет", combo)
		}
	}
}

// TestManagerRoundtrip регистрирует, проверяет и снимает клавиши через
// Manager. Если сочетание занято другой программой - тест пропускается.
func TestManagerRoundtrip(t *testing.T) {
	m := New()
	defer m.Close()

	if err := m.Set("zapret", "Ctrl+Alt+P", func() {}); err != nil {
		t.Skipf("сочетание Ctrl+Alt+P занято другой программой: %v", err)
	}
	if got := m.Combo("zapret"); got != "Ctrl+Alt+P" {
		t.Fatalf("Combo() = %q", got)
	}
	if err := m.Set("vpn", "Ctrl+Alt+9", func() {}); err != nil {
		t.Fatalf("вторая клавиша: %v", err)
	}
	// Та же клавиша для другого действия - ошибка занятости в рамках
	// системы? Не обязательно: RegisterHotKey вернёт «already registered»,
	// если то же сочетание уже зарегистрировано нашим же потоком.
	if err := m.Set("toggle", "Ctrl+Alt+P", func() {}); err == nil {
		t.Error("ожидали ошибку при повторном занятии того же сочетания")
	}
	// Смена комбинации снимает старую.
	if err := m.Set("zapret", "Ctrl+Alt+Q", func() {}); err != nil {
		t.Fatalf("смена комбинации: %v", err)
	}
	if got := m.Combo("zapret"); got != "Ctrl+Alt+Q" {
		t.Fatalf("Combo() после смены = %q", got)
	}
	// Снятие.
	if err := m.Set("zapret", "", nil); err != nil {
		t.Fatalf("снятие: %v", err)
	}
	if got := m.Combo("zapret"); got != "" {
		t.Fatalf("Combo() после снятия = %q", got)
	}
}
