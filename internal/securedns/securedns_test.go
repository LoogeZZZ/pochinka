package securedns

import "testing"

// Только чтение: какие карты тронули бы и что сейчас на них настроено.
func TestAdaptersReadOnly(t *testing.T) {
	list, err := adapters()
	if err != nil {
		t.Skip(err)
	}
	for _, a := range list {
		t.Logf("%s %s — v4 вручную: %q, v6 вручную: %q", a.Name, a.GUID, staticServers(a.GUID, false), staticServers(a.GUID, true))
	}
	t.Logf("DoH в Windows: %v", DoHSupported())
}

func TestVirtualFilter(t *testing.T) {
	for _, s := range []string{"happ tunnel", "wintun userspace tunnel", "radmin vpn", "hyper-v virtual ethernet", "pochinka"} {
		if !virtual(s) {
			t.Errorf("%q должен считаться виртуальным", s)
		}
	}
	if virtual("realtek pcie gbe family controller ethernet") {
		t.Error("обычная сетевая карта отброшена")
	}
}
