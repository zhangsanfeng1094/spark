package integrations

import (
	"path/filepath"
	"testing"

	"spark/internal/config"
)

func TestOpenCodeEditWritesProviderAndDefaultModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oc := &OpenCode{}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:8317/v1",
		OpenAIAPIKey:  "sk-test",
	}
	if err := oc.Edit(profile, []string{"spark/gpt-4o", "claude-sonnet"}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	cfgPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	cfg := readMap(cfgPath)
	if got := cfg["model"]; got != "spark/gpt-4o" {
		t.Fatalf("unexpected default model: %v", got)
	}
	provider := cfg["provider"].(map[string]any)
	spark := provider["spark"].(map[string]any)
	if got := spark["npm"]; got != "@ai-sdk/openai-compatible" {
		t.Fatalf("unexpected npm: %v", got)
	}
	options := spark["options"].(map[string]any)
	if got := options["baseURL"]; got != "http://127.0.0.1:8317/v1" {
		t.Fatalf("unexpected baseURL: %v", got)
	}
	if got := options["apiKey"]; got != "sk-test" {
		t.Fatalf("unexpected apiKey: %v", got)
	}
	models := spark["models"].(map[string]any)
	if _, ok := models["gpt-4o"]; !ok {
		t.Fatalf("missing gpt-4o model entry: %#v", models)
	}
	if _, ok := models["claude-sonnet"]; !ok {
		t.Fatalf("missing claude-sonnet model entry: %#v", models)
	}

	statePath := filepath.Join(home, ".local", "state", "opencode", "model.json")
	state := readMap(statePath)
	recent := state["recent"].([]any)
	if len(recent) != 2 {
		t.Fatalf("unexpected recent count: %d", len(recent))
	}
	first := recent[0].(map[string]any)
	if first["providerID"] != "spark" || first["modelID"] != "gpt-4o" {
		t.Fatalf("unexpected recent[0]: %#v", first)
	}
}

func TestNormalizeOpenCodeModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "gpt-4o", want: "gpt-4o"},
		{in: "spark/gpt-4o", want: "gpt-4o"},
		{in: "openai/gpt-4o", want: "openai/gpt-4o"},
		{in: "  ", want: ""},
	}
	for _, tc := range tests {
		if got := normalizeOpenCodeModelID(tc.in); got != tc.want {
			t.Fatalf("normalizeOpenCodeModelID(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestCLIArgsHasModelFlag(t *testing.T) {
	if cliArgsHasModelFlag([]string{"run", "hello"}) {
		t.Fatal("expected false")
	}
	if !cliArgsHasModelFlag([]string{"--model", "spark/gpt-4o"}) {
		t.Fatal("expected --model true")
	}
	if !cliArgsHasModelFlag([]string{"-m", "x"}) {
		t.Fatal("expected -m true")
	}
	if !cliArgsHasModelFlag([]string{"--model=x"}) {
		t.Fatal("expected --model= true")
	}
}
