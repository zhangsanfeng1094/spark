package integrations

import (
	"path/filepath"
	"testing"

	"spark/internal/config"
)

func TestOpenclawEditWritesPrimaryAndAllowlist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oc := &Openclaw{}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:8317/v1",
		OpenAIAPIKey:  "sk-test",
	}
	if err := oc.Edit(profile, []string{"custom-127-0-0-1-8317/gpt-5.3-codex"}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	cfgPath := filepath.Join(home, ".openclaw", "openclaw.json")
	cfg := readMap(cfgPath)
	agents := cfg["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)
	if got := model["primary"]; got != "agentlaunch/gpt-5.3-codex" {
		t.Fatalf("unexpected primary: %v", got)
	}
	allowlist := defaults["models"].(map[string]any)
	if _, ok := allowlist["agentlaunch/gpt-5.3-codex"]; !ok {
		t.Fatalf("allowlist missing primary model: %#v", allowlist)
	}

	models := cfg["models"].(map[string]any)
	if got := models["mode"]; got != "replace" {
		t.Fatalf("unexpected models.mode: %v", got)
	}
	providers := models["providers"].(map[string]any)
	agentlaunch := providers["agentlaunch"].(map[string]any)
	if got := agentlaunch["apiKey"]; got != "sk-test" {
		t.Fatalf("unexpected apiKey: %v", got)
	}
	entries := agentlaunch["models"].([]any)
	first := entries[0].(map[string]any)
	if got := first["id"]; got != "gpt-5.3-codex" {
		t.Fatalf("unexpected model id: %v", got)
	}
}

func TestNormalizeOpenclawModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "gpt-4o", want: "gpt-4o"},
		{in: "agentlaunch/gpt-4o", want: "gpt-4o"},
		{in: "custom/foo/bar", want: "foo/bar"},
		{in: "  ", want: ""},
	}
	for _, tc := range tests {
		if got := normalizeOpenclawModelID(tc.in); got != tc.want {
			t.Fatalf("normalizeOpenclawModelID(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}
