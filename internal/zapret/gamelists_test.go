package zapret

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGameDefaults(t *testing.T) {
	// По умолчанию выключены все группы: без явного выбора ничего не включается.
	r := CollectGameRules(nil, nil)
	if len(r.Include) != 0 || len(r.Exclude) != 0 || len(r.Unexclude) != 0 {
		t.Fatal("по умолчанию все группы должны быть выключены")
	}
	// Явно включённые применяются.
	r = CollectGameRules(nil, []string{"hoyo", "valorant"})
	if !slices.Contains(r.Exclude, "hoyoverse.com") || !slices.Contains(r.IPExclude, "104.160.128.0/19") {
		t.Fatal("явно включённые группы не применились")
	}
	if slices.Contains(r.Include, "riotgames.com") {
		t.Fatal("riot-login должен быть выключен, пока его не включили")
	}
	// Выключение побеждает включение.
	r = CollectGameRules([]string{"hoyo"}, []string{"hoyo", "riot-login"})
	if slices.Contains(r.Exclude, "hoyoverse.com") || !slices.Contains(r.Unexclude, "riotgames.com") {
		t.Fatal("явный выбор пользователя не учтён")
	}
	// SWYF: четыре домена игры, включается явно.
	r = CollectGameRules(nil, []string{"swyf"})
	if !slices.Contains(r.Include, "kolkata-auth.jataterworldwide.com") || !slices.Contains(r.Include, "kolkata-ai.jatater-worldwide.workers.dev") {
		t.Fatal("домены SWYF не применились")
	}
	// Ни одна группа не должна одновременно включать и исключать домен:
	// исключение в zapret побеждает, включение было бы мёртвым.
	for _, g := range GameGroups {
		for _, d := range g.Include {
			if slices.Contains(g.Exclude, d) {
				t.Errorf("%s: %s и в обходе, и в исключениях", g.ID, d)
			}
		}
	}
}

func TestPatchReleaseExclude(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "list-exclude.txt")
	os.WriteFile(cur, []byte("vk.com\r\nriotgames.com\r\nriotcdn.net\r\nya.ru\r\n"), 0o644)

	if err := patchReleaseExclude(dir, []string{"riotgames.com", "RIOTCDN.NET"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cur)
	if strings.Contains(string(b), "riot") || !strings.Contains(string(b), "vk.com") {
		t.Fatalf("после правки: %q", b)
	}
	// Группу выключили - исключения возвращаются из оригинала.
	if err := patchReleaseExclude(dir, nil); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(cur)
	if !strings.Contains(string(b), "riotgames.com") {
		t.Fatalf("оригинал не восстановлен: %q", b)
	}
}
