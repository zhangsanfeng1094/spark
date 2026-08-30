package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spark/internal/config"
)

func oneTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func oneModelsPath(home string) string {
	return filepath.Join(home, ".one", "agent", "models.json")
}

func oneSettingsPath(home string) string {
	return filepath.Join(home, ".one", "agent", "settings.json")
}

func TestOneEditWritesSparkProviderWithoutKey(t *testing.T) {
	home := oneTempHome(t)

	// Pre-existing user config must survive the edit.
	if err := ensureDir(oneModelsPath(home)); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	writeJSON(oneModelsPath(home), map[string]any{
		"includeDefaults": false,
		"providers": map[string]any{
			"cpa": map[string]any{
				"api":     "openai-responses",
				"baseUrl": "http://example.invalid/v1",
				"models":  []any{map[string]any{"id": "gpt-5.5"}},
			},
		},
	})

	one := &One{}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:8317/v1",
		OpenAIAPIKey:  "sk-test",
	}
	if err := one.Edit(profile, []string{"spark/gpt-4o", "claude-sonnet", "spark:gpt-4o"}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	root := readMap(oneModelsPath(home))
	if root["includeDefaults"] != false {
		t.Fatalf("includeDefaults not preserved: %v", root["includeDefaults"])
	}
	providers := root["providers"].(map[string]any)
	if _, ok := providers["cpa"]; !ok {
		t.Fatalf("existing provider cpa was dropped: %#v", providers)
	}
	spark := providers["spark"].(map[string]any)
	if got := spark["api"]; got != "openai-responses" {
		t.Fatalf("unexpected api: %v", got)
	}
	if got := spark["providerType"]; got != "openai-responses" {
		t.Fatalf("unexpected providerType: %v", got)
	}
	if got := spark["baseUrl"]; got != "http://127.0.0.1:8317/v1" {
		t.Fatalf("unexpected baseUrl: %v", got)
	}
	if _, ok := spark["apiKey"]; ok {
		t.Fatalf("apiKey must not be written to the real models.json: %#v", spark["apiKey"])
	}
	models := spark["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("expected 2 models after dedup, got %d: %#v", len(models), models)
	}
	if got := models[0].(map[string]any)["id"]; got != "gpt-4o" {
		t.Fatalf("unexpected first model id: %v", got)
	}

	// Provider selection happens in the isolated launch home only.
	if _, err := os.Stat(oneSettingsPath(home)); err == nil {
		t.Fatal("Edit must not create or modify settings.json")
	}
}

func TestOneEditWireMapping(t *testing.T) {
	home := oneTempHome(t)
	one := &One{}

	cases := []struct {
		name     string
		profile  *config.Profile
		wantWire string
		wantBase string
	}{
		{
			name:     "default prefers responses",
			profile:  &config.Profile{OpenAIBaseURL: "http://upstream/v1"},
			wantWire: "openai-responses",
			wantBase: "http://upstream/v1",
		},
		{
			name: "chat only",
			profile: &config.Profile{
				OpenAIBaseURL: "http://upstream/v1",
				OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
			},
			wantWire: "openai-completions",
			wantBase: "http://upstream/v1",
		},
		{
			name: "anthropic only uses anthropic base url",
			profile: &config.Profile{
				OpenAIBaseURL:    "http://upstream/v1",
				OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
				AnthropicBaseURL: "http://anthropic-upstream",
			},
			wantWire: "anthropic-messages",
			wantBase: "http://anthropic-upstream",
		},
		{
			name: "gemini only",
			profile: &config.Profile{
				OpenAIBaseURL: "http://upstream/v1",
				OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
			},
			wantWire: "gemini-generate-content",
			wantBase: "http://upstream/v1",
		},
		{
			name: "mixed picks responses",
			profile: &config.Profile{
				OpenAIBaseURL: "http://upstream/v1",
				OpenAIAPIType: config.OpenAIAPITypeAnthropicMessages + "," + config.OpenAIAPITypeResponses,
			},
			wantWire: "openai-responses",
			wantBase: "http://upstream/v1",
		},
	}
	for _, tc := range cases {
		if err := one.Edit(tc.profile, []string{"m"}); err != nil {
			t.Fatalf("case %s: edit failed: %v", tc.name, err)
		}
		root := readMap(oneModelsPath(home))
		spark := root["providers"].(map[string]any)["spark"].(map[string]any)
		if got := spark["api"]; got != tc.wantWire {
			t.Fatalf("case %s: wire = %v, want %v", tc.name, got, tc.wantWire)
		}
		if got := spark["baseUrl"]; got != tc.wantBase {
			t.Fatalf("case %s: baseUrl = %v, want %v", tc.name, got, tc.wantBase)
		}
	}
}

func TestOneEditWithoutModelsFails(t *testing.T) {
	home := oneTempHome(t)
	one := &One{}
	if err := one.Edit(&config.Profile{}, []string{"  "}); err == nil {
		t.Fatal("expected error for empty models")
	}
	if _, err := os.Stat(oneModelsPath(home)); err == nil {
		t.Fatal("models.json should not be written for empty models")
	}
}

func TestWriteOneLaunchHome(t *testing.T) {
	userHome := oneTempHome(t)

	realAgent := filepath.Join(userHome, ".one", "agent")
	if err := os.MkdirAll(filepath.Join(realAgent, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	// Real provider catalog plus the user's own selection state.
	writeJSON(oneModelsPath(userHome), map[string]any{
		"includeDefaults": false,
		"providers": map[string]any{
			"cpa": map[string]any{
				"api":     "openai-responses",
				"baseUrl": "http://example.invalid/v1",
				"models":  []any{map[string]any{"id": "gpt-5.5"}},
			},
		},
	})
	writeJSON(oneSettingsPath(userHome), map[string]any{
		"provider":     "cpa",
		"model":        "gpt-5.5",
		"auto_approve": true,
	})
	// MCP servers and auth must behave differently in the launch home:
	// mcp.json survives, auth.json must never appear.
	if err := os.WriteFile(filepath.Join(realAgent, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatalf("write mcp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realAgent, "auth.json"), []byte(`{"token":"oauth"}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realAgent, "auth.json.lock"), []byte("lock"), 0o644); err != nil {
		t.Fatalf("write auth lock: %v", err)
	}
	// Non-.one home entries (ssh keys, caches) must be mirrored so tooling works.
	if err := os.MkdirAll(filepath.Join(userHome, ".ssh"), 0o700); err != nil {
		t.Fatalf("mkdir ssh: %v", err)
	}

	dir := t.TempDir()
	profile := &config.Profile{
		OpenAIBaseURL: "http://gw.example/v1",
		OpenAIAPIKey:  "sk-test",
		OpenAIAPIType: "chat_completions",
	}
	if err := writeOneLaunchHome(dir, profile, "mock-model"); err != nil {
		t.Fatalf("write launch home: %v", err)
	}

	// models.json: real providers preserved, spark entry carries the key.
	root := readMap(filepath.Join(dir, ".one", "agent", "models.json"))
	providers := root["providers"].(map[string]any)
	if _, ok := providers["cpa"]; !ok {
		t.Fatalf("cpa provider missing from launch models.json: %#v", providers)
	}
	spark := providers["spark"].(map[string]any)
	if got := spark["api"]; got != "openai-completions" {
		t.Fatalf("unexpected launch wire: %v", got)
	}
	if got := spark["baseUrl"]; got != "http://gw.example/v1" {
		t.Fatalf("unexpected launch baseUrl: %v", got)
	}
	if got := spark["apiKey"]; got != "sk-test" {
		t.Fatalf("launch entry must embed the key: %v", got)
	}
	launchModels := spark["models"].([]any)
	if len(launchModels) != 1 || launchModels[0].(map[string]any)["id"] != "mock-model" {
		t.Fatalf("unexpected launch models: %#v", launchModels)
	}

	// settings.json: spark selected, other user prefs preserved.
	settings := readMap(filepath.Join(dir, ".one", "agent", "settings.json"))
	if got := settings["provider"]; got != "spark" {
		t.Fatalf("unexpected launch provider: %v", got)
	}
	if got := settings["model"]; got != "mock-model" {
		t.Fatalf("unexpected launch model: %v", got)
	}
	if settings["auto_approve"] != true {
		t.Fatalf("user settings not preserved: %#v", settings)
	}
	prefs := readMap(filepath.Join(dir, ".one", "agent", "preferences.json"))
	if prefs["provider"] != "spark" || prefs["model"] != "mock-model" {
		t.Fatalf("unexpected preferences: %#v", prefs)
	}

	// Mirrored assets: mcp.json present, auth excluded, home dotfiles linked.
	if _, err := os.Lstat(filepath.Join(dir, ".one", "agent", "mcp.json")); err != nil {
		t.Fatalf("mcp.json not mirrored: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".one", "agent", "sessions")); err != nil {
		t.Fatalf("sessions not mirrored: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".one", "agent", "auth.json")); err == nil {
		t.Fatal("auth.json must not appear in the launch home")
	}
	if _, err := os.Lstat(filepath.Join(dir, ".one", "agent", "auth.json.lock")); err == nil {
		t.Fatal("auth.json.lock must not appear in the launch home")
	}
	if _, err := os.Lstat(filepath.Join(dir, ".ssh")); err != nil {
		t.Fatalf(".ssh not mirrored: %v", err)
	}
	// .one itself must be a rebuilt directory, not a link to the real one.
	if fi, err := os.Lstat(filepath.Join(dir, ".one")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf(".one must be a real dir in the launch home: %v", err)
	}
}

func TestOneNormalizeModelID(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":            "gpt-4o",
		"spark:gpt-4o":      "gpt-4o",
		"spark/gpt-4o":      "gpt-4o",
		" Spark : gpt-4o  ": "gpt-4o",
		"other:model":       "other:model",
		"":                  "",
	}
	for in, want := range cases {
		if got := normalizeOneModelID(in); got != want {
			t.Fatalf("normalizeOneModelID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOneFindBinaryMissing(t *testing.T) {
	oneTempHome(t)
	t.Setenv("PATH", t.TempDir())
	_, err := findOneBinary()
	if err == nil {
		t.Fatal("expected error when one binary is absent")
	}
	if !strings.Contains(err.Error(), "one is not installed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
