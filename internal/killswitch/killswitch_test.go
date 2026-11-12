//go:build windows

package killswitch

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestKillSwitchRoundtrip включает и выключает Kill Switch. Требует прав
// администратора (WFP без них не отдаётся); фильтры сессии динамические и
// исчезнут в любом случае.
func TestKillSwitchRoundtrip(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("нужны права администратора")
	}
	if Active() {
		t.Fatal("Kill Switch уже включён - сначала выключите его")
	}
	if err := Enable(0); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !Active() {
		t.Fatal("после Enable Active() == false")
	}
	// Повторное включение - не ошибка (идемпотентность).
	if err := Enable(0); err != nil {
		t.Fatalf("повторный Enable: %v", err)
	}
	Disable()
	if Active() {
		t.Fatal("после Disable Active() == true")
	}
	// Повторное выключение безопасно.
	Disable()
}
