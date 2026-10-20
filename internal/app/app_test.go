package app

import "testing"

func TestPrettyStrategy(t *testing.T) {
	for in, want := range map[string]string{"general": "Стандартная", "general (ALT2)": "ALT2", "general (FAKE TLS AUTO)": "FAKE TLS AUTO", "discord": "discord"} {
		if got := prettyStrategy(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestRankPicks(t *testing.T) {
	in := []PickResult{
		{Name: "a", Discord: true, YouTube: false, Latency: 50},
		{Name: "b", Discord: true, YouTube: true, Latency: 900},
		{Name: "c", Discord: true, YouTube: true, Latency: 300},
		{Name: "d", Discord: false, YouTube: true, Latency: 10},
	}
	got := rankPicks(in)
	order := ""
	for _, r := range got {
		order += r.Name
	}
	if order != "cbda" || countFull(got) != 2 {
		t.Fatalf("порядок %s, полных %d", order, countFull(got))
	}
}
