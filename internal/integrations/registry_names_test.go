package integrations_test

import (
	"testing"

	"spark/internal/integrations"
)

func TestNamesExcludeRemoved(t *testing.T) {
	names := integrations.Names()
	for _, n := range names {
		if n == "pi" || n == "droid" || n == "openclaw" || n == "clawdbot" || n == "moltbot" {
			t.Fatalf("unexpected %q in Names: %v", n, names)
		}
	}
	for _, want := range []string{"claude", "codex", "grok", "one", "opencode"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, names)
		}
	}
	if _, ok := integrations.Get("pi"); ok {
		t.Fatal("pi still registered")
	}
	if _, ok := integrations.Get("droid"); ok {
		t.Fatal("droid still registered")
	}
	if _, ok := integrations.Get("openclaw"); ok {
		t.Fatal("openclaw still registered")
	}
}
