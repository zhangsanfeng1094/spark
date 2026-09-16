package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spark/internal/config"
)

func TestWriteAgyLaunchHomeSelectsGeminiAndMergesMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realCLI := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(filepath.Join(realCLI, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realCLI, "settings.json"), []byte(`{"trustedWorkspaces":["/work"],"modelProvider":"oauth"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realCLI, "antigravity-oauth-token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realCLI, "skills", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	realConfig := filepath.Join(home, ".gemini", "config")
	if err := os.MkdirAll(realConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realConfig, "mcp_config.json"), []byte(`{"mcpServers":{"user":{"command":"user-mcp","disabled":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	sparkConfigPath := filepath.Join(home, ".spark", "config.json")
	if err := os.MkdirAll(filepath.Dir(sparkConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	sparkConfig := map[string]any{
		"version":      1,
		"profiles":     map[string]any{},
		"integrations": map[string]any{},
		"mcp_servers": map[string]any{
			"stdio": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "server"},
				"env":     map[string]string{"TOKEN": "value"},
				"enabled": true,
			},
			"remote": map[string]any{
				"url":     "https://example.com/mcp",
				"enabled": true,
			},
			"off": map[string]any{
				"command": "disabled",
				"enabled": false,
			},
		},
	}
	data, err := json.Marshal(sparkConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sparkConfigPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	launchHome := t.TempDir()
	if err := writeAgyLaunchHome(launchHome); err != nil {
		t.Fatalf("writeAgyLaunchHome: %v", err)
	}

	settings := readMap(filepath.Join(launchHome, ".gemini", "antigravity-cli", "settings.json"))
	if got := settings["modelProvider"]; got != agyModelProvider {
		t.Fatalf("modelProvider=%v", got)
	}
	if _, ok := settings["trustedWorkspaces"]; !ok {
		t.Fatalf("trustedWorkspaces not preserved: %#v", settings)
	}
	if _, err := os.Lstat(filepath.Join(launchHome, ".gemini", "antigravity-cli", "antigravity-oauth-token")); !os.IsNotExist(err) {
		t.Fatal("OAuth token must not be linked into the launch home")
	}
	if _, err := os.Stat(filepath.Join(launchHome, ".gemini", "antigravity-cli", "skills", "keep.txt")); err != nil {
		t.Fatalf("skills not preserved: %v", err)
	}

	mcp := readMap(filepath.Join(launchHome, ".gemini", "config", "mcp_config.json"))
	servers := mcp["mcpServers"].(map[string]any)
	if _, ok := servers["user"]; !ok {
		t.Fatalf("user MCP missing: %#v", servers)
	}
	stdio := servers["stdio"].(map[string]any)
	if got := stdio["command"]; got != "npx" {
		t.Fatalf("stdio command=%v", got)
	}
	if got := stdio["disabled"]; got != false {
		t.Fatalf("stdio disabled=%v", got)
	}
	remote := servers["remote"].(map[string]any)
	if got := remote["serverUrl"]; got != "https://example.com/mcp" {
		t.Fatalf("remote serverUrl=%v", got)
	}
	if _, ok := servers["off"]; ok {
		t.Fatalf("disabled Spark MCP should not be injected: %#v", servers["off"])
	}
}

func TestEncodeAgyMCPServer(t *testing.T) {
	got := encodeAgyMCPServer(&config.McpServerConfig{
		Command: "node",
		Args:    []string{"server.js"},
		Env:     map[string]string{"A": "B"},
		Enabled: true,
	})
	if got["command"] != "node" || got["disabled"] != false {
		t.Fatalf("unexpected server: %#v", got)
	}
	args := got["args"].([]string)
	if len(args) != 1 || args[0] != "server.js" {
		t.Fatalf("args=%#v", args)
	}
}

func TestAgyRunInjectsGeminiProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPARK_DIRECT", "1")
	binDir := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	script := filepath.Join(binDir, "agy")
	body := "#!/bin/sh\nprintf '%s\\n' \"$HOME\" \"$GEMINI_API_KEY\" \"$GOOGLE_GEMINI_BASE_URL\" \"$@\" > \"$AGY_CAPTURE\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGY_CAPTURE", capture)

	a := &Agy{}
	profile := &config.Profile{
		OpenAIBaseURL: "https://gemini.example/v1beta/",
		OpenAIAPIKey:  "secret-key",
		OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
	}
	if err := a.Run(profile, "gemini-3.7-flash", []string{"--print", "hello"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 7 {
		t.Fatalf("capture=%q", string(data))
	}
	if lines[0] == home || !strings.Contains(lines[0], "spark-agy-home-") {
		t.Fatalf("HOME=%q", lines[0])
	}
	if lines[1] != "secret-key" {
		t.Fatalf("GEMINI_API_KEY=%q", lines[1])
	}
	if lines[2] != "https://gemini.example/v1beta" {
		t.Fatalf("GOOGLE_GEMINI_BASE_URL=%q", lines[2])
	}
	wantArgs := []string{"--model", "gemini-3.7-flash", "--print", "hello"}
	for i, want := range wantArgs {
		if lines[i+3] != want {
			t.Fatalf("arg[%d]=%q want %q", i, lines[i+3], want)
		}
	}
}

func TestAgyShouldUseCompatProxy(t *testing.T) {
	t.Parallel()
	if !agyShouldUseCompatProxy(nil) {
		t.Fatal("nil profile should use compat")
	}
	if !agyShouldUseCompatProxy(&config.Profile{OpenAIAPIType: config.OpenAIAPITypeResponses}) {
		t.Fatal("responses profile should use compat")
	}
	if !agyShouldUseCompatProxy(&config.Profile{OpenAIAPIType: config.OpenAIAPITypeChatCompletions}) {
		t.Fatal("chat completions profile should use compat")
	}
	if agyShouldUseCompatProxy(&config.Profile{OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent}) {
		t.Fatal("gemini generateContent profile should use direct mode")
	}
}

func TestAgyRunDirectEscapeHatchForNonGeminiProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPARK_DIRECT", "1")
	binDir := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture.txt")
	script := filepath.Join(binDir, "agy")
	body := "#!/bin/sh\nprintf '%s\\n' \"$HOME\" \"$GEMINI_API_KEY\" \"$GOOGLE_GEMINI_BASE_URL\" \"$@\" > \"$AGY_CAPTURE\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGY_CAPTURE", capture)

	a := &Agy{}
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		OpenAIAPIKey:  "secret-key",
		OpenAIAPIType: config.OpenAIAPITypeChatCompletions,
	}
	if err := a.Run(profile, "gpt-4o", []string{"--print", "hello"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("capture=%q", string(data))
	}
	if lines[1] != "secret-key" {
		t.Fatalf("GEMINI_API_KEY=%q", lines[1])
	}
	if lines[2] != "https://api.openai.com/v1" {
		t.Fatalf("GOOGLE_GEMINI_BASE_URL=%q", lines[2])
	}
}
