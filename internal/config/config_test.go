package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := defaultConfig()
	cfg.DefaultProfile = "work"
	cfg.Profiles["work"] = &Profile{
		OpenAIBaseURL: "https://example.com/v1",
		OpenAIAPIKey:  "token",
		ModelListURL:  "https://example.com/custom/models",
		Models:        []string{"gpt-4.1-mini", "gpt-4.1"},
		DefaultModel:  "gpt-4.1",
	}
	cfg.UpsertModelHistory("gpt-4.1-mini")
	cfg.SetMcpServer("docs", &McpServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-docs"},
		Enabled: true,
	})

	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got.DefaultProfile != "work" {
		t.Fatalf("DefaultProfile mismatch, got %q", got.DefaultProfile)
	}
	if got.Profiles["work"] == nil || got.Profiles["work"].OpenAIAPIKey != "token" {
		t.Fatalf("work profile not persisted correctly: %#v", got.Profiles["work"])
	}
	if got.Profiles["work"].DefaultModel != "gpt-4.1" {
		t.Fatalf("work profile default model mismatch: %q", got.Profiles["work"].DefaultModel)
	}
	if got.Profiles["work"].ModelListURL != "https://example.com/custom/models" {
		t.Fatalf("work profile model list url mismatch: %q", got.Profiles["work"].ModelListURL)
	}
	if got.Integration("codex").Profile != "work" {
		t.Fatalf("integration profile mismatch, got %q", got.Integration("codex").Profile)
	}
	if !reflect.DeepEqual(got.Profiles["work"].Models, []string{"gpt-4.1-mini", "gpt-4.1"}) {
		t.Fatalf("profile models mismatch: %#v", got.Profiles["work"].Models)
	}
	if got.History.LastModelInput != "gpt-4.1-mini" {
		t.Fatalf("history last model mismatch, got %q", got.History.LastModelInput)
	}
	if got.GetMcpServer("docs") == nil || got.GetMcpServer("docs").Command != "npx" {
		t.Fatalf("mcp server not persisted correctly: %#v", got.GetMcpServer("docs"))
	}
}

func TestLoadCreatesDefaultConfigWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.DefaultProfile != "default" {
		t.Fatalf("DefaultProfile mismatch, got %q", got.DefaultProfile)
	}
	if got.Profiles["default"] == nil {
		t.Fatalf("default profile should exist")
	}

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath failed: %v", err)
	}
	wantPrefix := filepath.Join(homeDirFromTest(t), ".spark")
	if filepath.Dir(path) != wantPrefix {
		t.Fatalf("config dir mismatch, got %q want %q", filepath.Dir(path), wantPrefix)
	}
	if filepath.Base(path) != "config.json" {
		t.Fatalf("unexpected config path: %q", path)
	}
}

func TestUpsertModelHistoryDedupAndLimit(t *testing.T) {
	cfg := defaultConfig()
	for i := 0; i < 30; i++ {
		cfg.UpsertModelHistory(fmt.Sprintf("model-%02d", i))
	}
	cfg.UpsertModelHistory("model-29")

	if len(cfg.History.ModelInputs) > 20 {
		t.Fatalf("history length should be <= 20, got %d", len(cfg.History.ModelInputs))
	}
	if cfg.History.ModelInputs[0] != "model-29" {
		t.Fatalf("latest history item should be first, got %q", cfg.History.ModelInputs[0])
	}
}

func TestNormalizeModelStripsNUL(t *testing.T) {
	got := NormalizeModel(" glm-5\x00 ")
	if got != "glm-5" {
		t.Fatalf("NormalizeModel returned %q", got)
	}
}

func TestNormalizeSanitizesStoredModels(t *testing.T) {
	cfg := &RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				Models:       []string{"glm-5\x00", " glm-5 ", "other"},
				DefaultModel: " glm-5\x00 ",
			},
		},
		History: History{
			LastModelInput: " glm-5\x00 ",
			ModelInputs:    []string{" glm-5\x00 ", "other"},
		},
	}

	Normalize(cfg)

	if cfg.Profiles["default"].DefaultModel != "glm-5" {
		t.Fatalf("DefaultModel=%q", cfg.Profiles["default"].DefaultModel)
	}
	if !reflect.DeepEqual(cfg.Profiles["default"].Models, []string{"glm-5", "other"}) {
		t.Fatalf("Models=%#v", cfg.Profiles["default"].Models)
	}
	if cfg.History.LastModelInput != "glm-5" {
		t.Fatalf("LastModelInput=%q", cfg.History.LastModelInput)
	}
	if !reflect.DeepEqual(cfg.History.ModelInputs, []string{"glm-5", "other"}) {
		t.Fatalf("ModelInputs=%#v", cfg.History.ModelInputs)
	}
}

func TestSetDefaultProfileRequiresExistingProfile(t *testing.T) {
	cfg := defaultConfig()
	if err := cfg.SetDefaultProfile("missing"); err == nil {
		t.Fatalf("expected error when setting missing profile")
	}
}

func TestNormalizeOpenAIAPIType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "responses", want: OpenAIAPITypeResponses},
		{in: "response", want: OpenAIAPITypeResponses},
		{in: "openai-responses", want: OpenAIAPITypeResponses},
		{in: "auto", want: OpenAIAPITypeAuto},
		{in: "prefer_responses", want: OpenAIAPITypeAuto},
		{in: "chat_completions", want: OpenAIAPITypeChatCompletions},
		{in: "chat/completions", want: OpenAIAPITypeChatCompletions},
		{in: "openai-completions", want: OpenAIAPITypeChatCompletions},
		{in: "gemini_generate_content", want: OpenAIAPITypeGeminiGenerateContent},
		{in: "generateContent", want: OpenAIAPITypeGeminiGenerateContent},
		{in: "anthropic", want: OpenAIAPITypeAnthropicMessages},
		{in: "anthropic_messages", want: OpenAIAPITypeAnthropicMessages},
		{in: "messages", want: OpenAIAPITypeAnthropicMessages},
		{in: "unknown", want: ""},
	}
	for _, tt := range tests {
		if got := NormalizeOpenAIAPIType(tt.in); got != tt.want {
			t.Fatalf("NormalizeOpenAIAPIType(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseOpenAIAPITypes(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{in: "", want: nil},
		{in: "responses", want: []string{OpenAIAPITypeResponses}},
		{in: "chat_completions", want: []string{OpenAIAPITypeChatCompletions}},
		{in: "gemini_generate_content", want: []string{OpenAIAPITypeGeminiGenerateContent}},
		{in: "auto", want: []string{OpenAIAPITypeAuto}},
		{in: "responses,chat_completions", want: []string{OpenAIAPITypeResponses, OpenAIAPITypeChatCompletions}},
		{in: "responses,gemini_generate_content", want: []string{OpenAIAPITypeResponses, OpenAIAPITypeGeminiGenerateContent}},
		{in: "chat_completions,responses", want: []string{OpenAIAPITypeResponses, OpenAIAPITypeChatCompletions}},
		{in: "responses|chat/completions", want: []string{OpenAIAPITypeResponses, OpenAIAPITypeChatCompletions}},
		{in: "anthropic", want: []string{OpenAIAPITypeAnthropicMessages}},
		{in: "anthropic_messages,chat/completions", want: []string{OpenAIAPITypeChatCompletions, OpenAIAPITypeAnthropicMessages}},
		{in: "responses,unknown", want: []string{OpenAIAPITypeResponses}},
		{in: "unknown", want: nil},
	}
	for _, tt := range tests {
		if got := ParseOpenAIAPITypes(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("ParseOpenAIAPITypes(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}

func TestCanonicalizeOpenAIAPITypes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "responses", want: OpenAIAPITypeResponses},
		{in: "chat_completions,responses", want: "responses,chat_completions"},
		{in: "gemini_generate_content,responses", want: "responses,gemini_generate_content"},
		{in: "responses|chat_completions", want: "responses,chat_completions"},
		{in: "anthropic,chat_completions", want: "chat_completions,anthropic_messages"},
		{in: "auto,responses", want: OpenAIAPITypeAuto},
	}
	for _, tt := range tests {
		if got := CanonicalizeOpenAIAPITypes(tt.in); got != tt.want {
			t.Fatalf("CanonicalizeOpenAIAPITypes(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestSupportsOpenAIAPIType(t *testing.T) {
	if !SupportsOpenAIAPIType("responses,chat_completions", OpenAIAPITypeResponses) {
		t.Fatalf("expected responses support")
	}
	if !SupportsOpenAIAPIType("responses,chat_completions", OpenAIAPITypeChatCompletions) {
		t.Fatalf("expected chat support")
	}
	if SupportsOpenAIAPIType("responses", OpenAIAPITypeChatCompletions) {
		t.Fatalf("did not expect chat support")
	}
	if !SupportsOpenAIAPIType("responses,gemini_generate_content", OpenAIAPITypeGeminiGenerateContent) {
		t.Fatalf("expected gemini support")
	}
}

func homeDirFromTest(t *testing.T) string {
	t.Helper()
	return os.Getenv("HOME")
}
