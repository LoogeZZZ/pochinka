package main

import (
	"context"
	"os"
	"testing"
)

// LIVE_UPDATE=1 — настоящий GitHub.
func TestCheckAppUpdateLive(t *testing.T) {
	if os.Getenv("LIVE_UPDATE") == "" {
		t.Skip("LIVE_UPDATE=1")
	}
	tag, page, err := latestAppRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("последний релиз: %s %s", tag, page)
	u, err := checkAppUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("для версии %s: %+v", appVersion, u)
}
