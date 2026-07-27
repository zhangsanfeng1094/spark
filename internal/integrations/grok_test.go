package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"spark/internal/config"
)

func TestGrokEditWritesSparkModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Preserve non-spark config on rewrite, including the user's OAuth default.
	cfgPath := filepath.Join(home, ".grok", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := "" +
		"[cli]\n" +
		"installer = \"internal\"\n" +
		"\n" +
		"[model.keep-me]\n" +
		"model = \"keep\"\n" +
		"\n" +
		"[models]\n" +
		"default = \"grok-4.5\"\n" +
		"default_reasoning_effort = \"high\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	g := &Grok{}
	profile := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:8317/v1",
		OpenAIAPIKey:  "sk-test",
		OpenAIAPIType: "chat_completions",
	}
	if err := g.Edit(profile, []string{"gpt-4o", "spark/claude-sonnet", "org/custom"}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	root := map[string]any{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		t.Fatalf("decode: %v", err)
	}

	cli := root["cli"].(map[string]any)
	if got := cli["installer"]; got != "internal" {
		t.Fatalf("expected preserved cli.installer, got %v", got)
	}
	models := root["models"].(map[string]any)
	if got := models["default"]; got != "grok-4.5" {
		t.Fatalf("expected models.default left alone for OAuth, got %v", got)
	}
	if got := models["default_reasoning_effort"]; got != "high" {
		t.Fatalf("expected models.default_reasoning_effort preserved, got %v", got)
	}
	modelTable := root["model"].(map[string]any)
	if _, ok := modelTable["keep-me"]; !ok {
		t.Fatalf("expected non-spark model preserved: %#v", modelTable)
	}
	sparkGPT := modelTable["spark-gpt-4o"].(map[string]any)
	if got := sparkGPT["model"]; got != "gpt-4o" {
		t.Fatalf("unexpected model id: %v", got)
	}
	if got := sparkGPT["base_url"]; got != "http://127.0.0.1:8317/v1" {
		t.Fatalf("unexpected base_url: %v", got)
	}
	if _, ok := sparkGPT["api_key"]; ok {
		t.Fatalf("api_key must not be written to disk, got %#v", sparkGPT["api_key"])
	}
	if got := sparkGPT["env_key"]; got != grokSparkAPIKeyEnv {
		t.Fatalf("unexpected env_key: %v", got)
	}
	if got := sparkGPT["api_backend"]; got != "chat_completions" {
		t.Fatalf("unexpected api_backend: %v", got)
	}
	sparkClaude := modelTable["spark-claude-sonnet"].(map[string]any)
	if got := sparkClaude["model"]; got != "claude-sonnet" {
		t.Fatalf("unexpected spark/ stripped model: %v", got)
	}
	sparkOrg := modelTable["spark-org-custom"].(map[string]any)
	if got := sparkOrg["model"]; got != "org/custom" {
		t.Fatalf("unexpected org model: %v", got)
	}
}

func TestGrokEditResetsSparkDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgPath := filepath.Join(home, ".grok", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := "" +
		"[model.\"spark-old\"]\n" +
		"model = \"old\"\n" +
		"api_key = \"sk-legacy\"\n" +
		"\n" +
		"[models]\n" +
		"default = \"spark-old\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	g := &Grok{}
	profile := &config.Profile{
		OpenAIBaseURL: "https://gw.example/v1",
		OpenAIAPIKey:  "sk-new",
		OpenAIAPIType: "responses",
	}
	if err := g.Edit(profile, []string{"grok-4.5"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	root := map[string]any{}
	data, _ := os.ReadFile(cfgPath)
	if _, err := toml.Decode(string(data), &root); err != nil {
		t.Fatalf("decode: %v", err)
	}
	models := root["models"].(map[string]any)
	if got := models["default"]; got != "grok-4.5" {
		t.Fatalf("spark default should be reset for OAuth, got %v", got)
	}
	entry := root["model"].(map[string]any)["spark-grok-4.5"].(map[string]any)
	if _, ok := entry["api_key"]; ok {
		t.Fatalf("legacy api_key should be gone")
	}
	if got := entry["env_key"]; got != grokSparkAPIKeyEnv {
		t.Fatalf("env_key=%v", got)
	}
	// responses-only profile uses native responses (direct connect).
	if got := entry["api_backend"]; got != "responses" {
		t.Fatalf("api_backend=%v want responses", got)
	}
}

func TestWriteGrokLaunchHome(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	realGrok := filepath.Join(userHome, ".grok")
	if err := os.MkdirAll(filepath.Join(realGrok, "skills", "help"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realGrok, "skills", "help", "SKILL.md"), []byte("# help\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	// Auth must exist in the real home but never appear in the launch home.
	if err := os.WriteFile(filepath.Join(realGrok, "auth.json"), []byte(`{"token":"oauth"}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realGrok, "auth.json.lock"), []byte("lock"), 0o644); err != nil {
		t.Fatalf("write auth lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realGrok, "mcp_credentials.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write mcp creds: %v", err)
	}
	// Real config carries MCP servers and marketplace that must survive isolation.
	seed := "" +
		"[cli]\n" +
		"installer = \"internal\"\n" +
		"\n" +
		"[marketplace]\n" +
		"official_marketplace_auto_installed = true\n" +
		"\n" +
		"[mcp_servers.codegraph]\n" +
		"command = \"codegraph-mcp\"\n" +
		"enabled = true\n" +
		"\n" +
		"[mcp_servers.deepwiki]\n" +
		"url = \"https://mcp.deepwiki.com/mcp\"\n" +
		"enabled = true\n" +
		"\n" +
		"[models]\n" +
		"default = \"grok-4.5\"\n" +
		"default_reasoning_effort = \"high\"\n"
	if err := os.WriteFile(filepath.Join(realGrok, "config.toml"), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	dir := t.TempDir()
	profile := &config.Profile{
		OpenAIBaseURL: "http://gw.example/v1",
		OpenAIAPIKey:  "sk-test",
		OpenAIAPIType: "responses,chat_completions",
	}
	if err := writeGrokLaunchHome(dir, profile, "grok-4.5", "spark-grok-4.5"); err != nil {
		t.Fatalf("write: %v", err)
	}

	// No auth.json — isolation from OAuth.
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("launch home must not create/link auth.json")
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json.lock")); !os.IsNotExist(err) {
		t.Fatalf("launch home must not link auth.json.lock")
	}

	// Skills / MCP credentials are shared from the real home.
	skillTarget, err := os.Readlink(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("skills should be symlinked: %v", err)
	}
	if skillTarget != filepath.Join(realGrok, "skills") {
		t.Fatalf("skills link target=%q", skillTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "help", "SKILL.md")); err != nil {
		t.Fatalf("skills content not reachable: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "mcp_credentials.json")); err != nil {
		t.Fatalf("mcp_credentials.json should be linked: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	root := map[string]any{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Launch default points at the spark model, but other config is preserved.
	models := root["models"].(map[string]any)
	if got := models["default"]; got != "spark-grok-4.5" {
		t.Fatalf("launch default=%v", got)
	}
	if got := models["default_reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning effort not preserved: %v", got)
	}
	cli := root["cli"].(map[string]any)
	if got := cli["installer"]; got != "internal" {
		t.Fatalf("cli section not preserved: %v", got)
	}
	mcpServers := root["mcp_servers"].(map[string]any)
	if _, ok := mcpServers["codegraph"]; !ok {
		t.Fatalf("mcp_servers.codegraph missing: %#v", mcpServers)
	}
	if _, ok := mcpServers["deepwiki"]; !ok {
		t.Fatalf("mcp_servers.deepwiki missing: %#v", mcpServers)
	}
	entry := root["model"].(map[string]any)["spark-grok-4.5"].(map[string]any)
	if got := entry["base_url"]; got != "http://gw.example/v1" {
		t.Fatalf("base_url=%v", got)
	}
	// Default multi-type profile prefers responses for direct connect.
	if got := entry["api_backend"]; got != "responses" {
		t.Fatalf("api_backend=%v want responses", got)
	}
	if got := entry["env_key"]; got != grokSparkAPIKeyEnv {
		t.Fatalf("env_key=%v", got)
	}
	if strings.Contains(string(data), "sk-test") {
		t.Fatalf("launch config must not embed api key")
	}
	if strings.Contains(string(data), "oauth") {
		t.Fatalf("launch config must not embed oauth token")
	}
}

func TestShouldSkipGrokMirror(t *testing.T) {
	for _, name := range []string{"config.toml", "auth.json", "auth.json.lock", "auth.json.bak"} {
		if !shouldSkipGrokMirror(name) {
			t.Fatalf("expected skip %q", name)
		}
	}
	for _, name := range []string{"skills", "mcp_credentials.json", "bundled", "sessions", "trusted_folders.toml"} {
		if shouldSkipGrokMirror(name) {
			t.Fatalf("did not expect skip %q", name)
		}
	}
}

func TestGrokEnv(t *testing.T) {
	if got := grokEnv(&config.Profile{}); len(got) != 0 {
		t.Fatalf("empty key should yield no env, got %v", got)
	}
	got := grokEnv(&config.Profile{OpenAIAPIKey: "sk-test"})
	if len(got) != 1 || got[0] != grokSparkAPIKeyEnv+"=sk-test" {
		t.Fatalf("unexpected env: %v", got)
	}
}

func TestGrokAPIBackendMapping(t *testing.T) {
	tests := []struct {
		apiType string
		want    string
	}{
		{apiType: "chat_completions", want: "chat_completions"},
		{apiType: "responses", want: "responses"},
		{apiType: "anthropic_messages", want: "messages"},
		{apiType: "responses,chat_completions", want: "responses"},
		{apiType: "", want: "responses"}, // default profile type includes responses
		{apiType: "gemini_generate_content", want: "chat_completions"},
		{apiType: "chat_completions,anthropic_messages", want: "chat_completions"},
	}
	for _, tc := range tests {
		p := &config.Profile{OpenAIAPIType: tc.apiType}
		if got := grokAPIBackend(p); got != tc.want {
			t.Fatalf("grokAPIBackend(%q)=%q want %q", tc.apiType, got, tc.want)
		}
	}
}

func TestGrokLaunchRouteForProfile(t *testing.T) {
	tests := []struct {
		name           string
		profile        *config.Profile
		wantBackend    string
		wantBase       string
		wantReason     string
		wantDegraded   bool
		wantEnvHTTPKey bool // expect x-api-key via env_http_headers
	}{
		{
			name: "responses only",
			profile: &config.Profile{
				OpenAIBaseURL: "https://gw.example/v1",
				OpenAIAPIType: "responses",
			},
			wantBackend: "responses",
			wantBase:    "https://gw.example/v1",
			wantReason:  "responses_only",
		},
		{
			name: "default auto prefers responses",
			profile: &config.Profile{
				OpenAIBaseURL: "https://gw.example/v1",
				OpenAIAPIType: "responses,chat_completions",
			},
			wantBackend: "responses",
			wantBase:    "https://gw.example/v1",
			wantReason:  "prefer_responses",
		},
		{
			name: "empty api type uses default → responses",
			profile: &config.Profile{
				OpenAIBaseURL: "https://gw.example/v1",
			},
			wantBackend: "responses",
			wantBase:    "https://gw.example/v1",
			wantReason:  "prefer_responses",
		},
		{
			name: "chat only",
			profile: &config.Profile{
				OpenAIBaseURL: "https://chat.example/v1",
				OpenAIAPIType: "chat_completions",
			},
			wantBackend: "chat_completions",
			wantBase:    "https://chat.example/v1",
			wantReason:  "chat_only",
		},
		{
			name: "anthropic with AnthropicBaseURL",
			profile: &config.Profile{
				OpenAIBaseURL:    "https://openai-side.example/v1",
				AnthropicBaseURL: "https://anthropic.example/v1",
				OpenAIAPIType:    "anthropic_messages",
			},
			wantBackend:    "messages",
			wantBase:       "https://anthropic.example/v1",
			wantReason:     "anthropic_messages_anthropic_base",
			wantEnvHTTPKey: true,
		},
		{
			name: "anthropic falls back to OpenAI base",
			profile: &config.Profile{
				OpenAIBaseURL: "https://openai-side.example/v1",
				OpenAIAPIType: "anthropic_messages",
			},
			wantBackend:    "messages",
			wantBase:       "https://openai-side.example/v1",
			wantReason:     "anthropic_messages",
			wantEnvHTTPKey: true,
		},
		{
			name: "chat wins over anthropic when both listed without responses",
			profile: &config.Profile{
				OpenAIBaseURL:    "https://chat.example/v1",
				AnthropicBaseURL: "https://anthropic.example/v1",
				OpenAIAPIType:    "chat_completions,anthropic_messages",
			},
			wantBackend: "chat_completions",
			wantBase:    "https://chat.example/v1",
			wantReason:  "chat_only",
		},
		{
			name: "gemini degraded to chat",
			profile: &config.Profile{
				OpenAIBaseURL: "https://gw.example/v1",
				OpenAIAPIType: "gemini_generate_content",
			},
			wantBackend:  "chat_completions",
			wantBase:     "https://gw.example/v1",
			wantReason:   "gemini_unsupported_fallback_chat",
			wantDegraded: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := grokLaunchRouteForProfile(tc.profile)
			if got.APIBackend != tc.wantBackend {
				t.Fatalf("APIBackend=%q want %q", got.APIBackend, tc.wantBackend)
			}
			if got.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL=%q want %q", got.BaseURL, tc.wantBase)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("Reason=%q want %q", got.Reason, tc.wantReason)
			}
			if got.Degraded != tc.wantDegraded {
				t.Fatalf("Degraded=%v want %v", got.Degraded, tc.wantDegraded)
			}
			if tc.wantEnvHTTPKey {
				if got.EnvHTTPHeaders["x-api-key"] != grokSparkAPIKeyEnv {
					t.Fatalf("EnvHTTPHeaders x-api-key=%v want %s", got.EnvHTTPHeaders, grokSparkAPIKeyEnv)
				}
				if got.ExtraHeaders["anthropic-version"] != grokAnthropicVersion {
					t.Fatalf("ExtraHeaders anthropic-version=%v", got.ExtraHeaders)
				}
			} else if len(got.EnvHTTPHeaders) != 0 {
				t.Fatalf("unexpected EnvHTTPHeaders: %#v", got.EnvHTTPHeaders)
			}
		})
	}
}

func TestGrokEditAnthropicMessagesWritesHeadersAndBase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".grok", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	g := &Grok{}
	profile := &config.Profile{
		OpenAIBaseURL:    "https://openai.example/v1",
		AnthropicBaseURL: "https://anthropic.example/v1",
		OpenAIAPIKey:     "sk-ant-secret",
		OpenAIAPIType:    "anthropic_messages",
	}
	if err := g.Edit(profile, []string{"claude-opus"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "sk-ant-secret") {
		t.Fatalf("secret must not be written to config.toml")
	}
	root := map[string]any{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entry := root["model"].(map[string]any)["spark-claude-opus"].(map[string]any)
	if got := entry["api_backend"]; got != "messages" {
		t.Fatalf("api_backend=%v", got)
	}
	if got := entry["base_url"]; got != "https://anthropic.example/v1" {
		t.Fatalf("base_url=%v", got)
	}
	if got := entry["env_key"]; got != grokSparkAPIKeyEnv {
		t.Fatalf("env_key=%v", got)
	}
	extra, _ := entry["extra_headers"].(map[string]any)
	if got := extra["anthropic-version"]; got != grokAnthropicVersion {
		t.Fatalf("extra_headers anthropic-version=%v", got)
	}
	envHeaders, _ := entry["env_http_headers"].(map[string]any)
	if got := envHeaders["x-api-key"]; got != grokSparkAPIKeyEnv {
		t.Fatalf("env_http_headers x-api-key=%v", got)
	}
}

func TestGrokSparkModelEntryNoSecret(t *testing.T) {
	route := grokLaunchRoute{
		APIBackend: "messages",
		BaseURL:    "https://anthropic.example/v1",
		ExtraHeaders: map[string]string{
			"anthropic-version": grokAnthropicVersion,
		},
		EnvHTTPHeaders: map[string]string{
			"x-api-key": grokSparkAPIKeyEnv,
		},
		Reason: "anthropic_messages_anthropic_base",
	}
	entry := grokSparkModelEntry("claude-opus", route)
	if _, ok := entry["api_key"]; ok {
		t.Fatalf("api_key must not be present")
	}
	if entry["api_backend"] != "messages" {
		t.Fatalf("api_backend=%v", entry["api_backend"])
	}
}

func TestGrokModelKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "gpt-4o", want: "spark-gpt-4o"},
		{in: "org/model", want: "spark-org-model"},
		{in: "a b", want: "spark-a-b"},
	}
	for _, tc := range tests {
		if got := grokModelKey(tc.in); got != tc.want {
			t.Fatalf("grokModelKey(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeGrokModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "spark-gpt-4o", want: "gpt-4o"},
		{in: "gpt-4o", want: "gpt-4o"},
		{in: "spark/claude-sonnet", want: "claude-sonnet"},
		{in: "org/custom", want: "org/custom"},
	}
	for _, tc := range tests {
		if got := normalizeGrokModelID(tc.in); got != tc.want {
			t.Fatalf("normalizeGrokModelID(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}
