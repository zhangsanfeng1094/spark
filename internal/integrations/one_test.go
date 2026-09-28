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

func TestOneNeedsEditSkipsMatchingSparkProvider(t *testing.T) {
	home := oneTempHome(t)
	one := &One{}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:8317/v1",
		OpenAIAPIKey:  "sk-first",
		OpenAIAPIType: config.OpenAIAPITypeResponses,
	}
	models := []string{"spark/glm-5.3"}

	needs, err := one.NeedsEdit(profile, models)
	if err != nil {
		t.Fatalf("NeedsEdit before edit: %v", err)
	}
	if !needs {
		t.Fatal("missing spark provider must require edit")
	}

	if err := one.Edit(profile, models); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	needs, err = one.NeedsEdit(profile, models)
	if err != nil {
		t.Fatalf("NeedsEdit after edit: %v", err)
	}
	if needs {
		t.Fatal("matching spark provider should skip edit confirmation")
	}

	// The real models.json intentionally contains no API key. A rotated key is
	// injected into the isolated launch HOME and must not force a config rewrite.
	profile.OpenAIAPIKey = "sk-rotated"
	needs, err = one.NeedsEdit(profile, models)
	if err != nil {
		t.Fatalf("NeedsEdit after key rotation: %v", err)
	}
	if needs {
		t.Fatal("API key rotation alone must not require editing real models.json")
	}

	profile.OpenAIBaseURL = "http://127.0.0.1:9999/v1"
	needs, err = one.NeedsEdit(profile, models)
	if err != nil {
		t.Fatalf("NeedsEdit after endpoint change: %v", err)
	}
	if !needs {
		t.Fatal("endpoint change must require edit")
	}

	// Restore endpoint and change the managed model set.
	profile.OpenAIBaseURL = "http://127.0.0.1:8317/v1"
	needs, err = one.NeedsEdit(profile, []string{"glm-5.4"})
	if err != nil {
		t.Fatalf("NeedsEdit after model change: %v", err)
	}
	if !needs {
		t.Fatal("model change must require edit")
	}

	if _, err := os.Stat(oneModelsPath(home)); err != nil {
		t.Fatalf("models.json disappeared: %v", err)
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

func TestOneLaunchArgsAndEnv(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "http://gw.example/v1",
		OpenAIAPIKey:  "sk-test",
		OpenAIAPIType: "chat_completions",
	}

	args := oneLaunchArgs(profile, "mock-model", []string{"--resume"})
	wantArgs := []string{
		"--provider", "openai",
		"--openai-api", "openai-completions",
		"--base-url", "http://gw.example/v1",
		"--model", "mock-model",
		"--resume",
	}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("oneLaunchArgs = %v, want %v", args, wantArgs)
	}

	env := oneLaunchEnv(profile)
	for _, want := range []string{
		"ONE_OPENAI_API=openai-completions",
		"OPENAI_BASE_URL=http://gw.example/v1",
		"OPENAI_API_KEY=sk-test",
	} {
		if !containsEnvEntry(env, want) {
			t.Fatalf("expected %q in env %v", want, env)
		}
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
