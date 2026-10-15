package vpn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pochinka/internal/config"
	"pochinka/internal/paths"
)

// Изолированная папка данных: paths.DataDir() строится от %ProgramData%.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("ProgramData", t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	// Журнал в NUL: фоновый пинг держит файл открытым, и TempDir не удалить.
	xrayLog = func() string { return os.DevNull }
}

func TestLegacyMigration(t *testing.T) {
	isolate(t)
	link := testLinks["vless-reality-vision"]
	list, err := parseBody([]byte(link))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cache{Info: SubInfo{Title: "Старая"}, Servers: cacheOf(list)})
	_ = os.WriteFile(filepath.Join(paths.DataDir(), "vpn-cache.json"), b, 0o600)
	_, _ = config.Update(func(c *config.Config) { c.VPNSubURL = "https://old.example/sub"; c.VPNSelected = "x" })

	subs := loadSubs()
	if len(subs) != 1 || subs[0].Info.Title != "Старая" || len(subs[0].servers) != 1 {
		t.Fatalf("перенос не удался: %+v", subs)
	}
	cfg := config.Load()
	if cfg.VPNSubURL != "" || cfg.VPNActiveSub != subs[0].ID {
		t.Fatalf("конфиг после переноса: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(paths.DataDir(), "vpn-cache.json")); err == nil {
		t.Fatal("старый кэш не удалён")
	}
	// Повторная загрузка не плодит копии.
	if n := len(loadSubs()); n != 1 {
		t.Fatalf("после второй загрузки %d подписок", n)
	}
}

func TestMultiSubscriptionsRememberState(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	m := NewManager(func() {}, func() []string { return nil }, nil)

	// Подписки «ссылками на серверы» — без сети.
	subA := testLinks["vless-reality-vision"] + "\n" + testLinks["vless-ws"]
	subB := testLinks["trojan"] + "\n" + testLinks["vless-grpc"]
	if err := m.AddSubscription(ctx, subA); err != nil {
		t.Fatal(err)
	}
	idA := m.State().ActiveSub
	if err := m.AddSubscription(ctx, subB); err != nil {
		t.Fatal(err)
	}
	st := m.State()
	idB := st.ActiveSub
	if len(st.Subs) != 2 || idA == idB {
		t.Fatalf("ожидались 2 подписки, активна вторая: %+v", st.Subs)
	}

	// Выбираем разные серверы в каждой.
	pickB := st.Servers[1].ID
	if err := m.Select(ctx, pickB); err != nil {
		t.Fatal(err)
	}
	_ = m.SetActiveSub(ctx, idA)
	pickA := m.State().Servers[0].ID
	_ = m.Select(ctx, pickA)

	// Переключаемся туда-обратно — выбор у каждой свой.
	_ = m.SetActiveSub(ctx, idB)
	if got := m.State().Selected; got != pickB {
		t.Fatalf("подписка B забыла сервер: %q вместо %q", got, pickB)
	}
	_ = m.SetActiveSub(ctx, idA)
	if got := m.State().Selected; got != pickA {
		t.Fatalf("подписка A забыла сервер: %q вместо %q", got, pickA)
	}

	// «Перезапуск приложения»: всё читается с диска.
	m2 := NewManager(func() {}, func() []string { return nil }, nil)
	st2 := m2.State()
	if st2.ActiveSub != idA || st2.Selected != pickA || len(st2.Subs) != 2 {
		t.Fatalf("после перезапуска: active=%s selected=%s subs=%d", st2.ActiveSub, st2.Selected, len(st2.Subs))
	}

	// Повторное добавление той же ссылки не создаёт дубликат.
	_ = m2.AddSubscription(ctx, subB)
	if n := len(m2.State().Subs); n != 2 {
		t.Fatalf("дубликат подписки: %d", n)
	}

	// Удаление активной — активной становится оставшаяся.
	m2.RemoveSubscription(ctx, idB)
	st3 := m2.State()
	if len(st3.Subs) != 1 || st3.ActiveSub != idA {
		t.Fatalf("после удаления: %+v active=%s", st3.Subs, st3.ActiveSub)
	}
	m2.RemoveSubscription(ctx, idA)
	if st := m2.State(); st.HasSub || len(st.Subs) != 0 {
		t.Fatalf("после удаления всех: %+v", st)
	}
}
