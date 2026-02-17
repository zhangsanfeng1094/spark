package integrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"spark/internal/config"
)

type Claude struct{}

func (c *Claude) String() string { return "Claude Code" }

func (c *Claude) findPath() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	fallback := filepath.Join(home, ".claude", "local", name)
	if _, err := os.Stat(fallback); err != nil {
		return "", err
	}
	return fallback, nil
}

func anthropicBaseURL(profile *config.Profile) string {
	if profile != nil && profile.AnthropicBaseURL != "" {
		return profile.AnthropicBaseURL
	}
	return "http://localhost:11434"
}

func resolveClaudeModel(profile *config.Profile, model string) string {
	m := strings.TrimSpace(model)
	if m != "" {
		return m
	}
	if profile == nil {
		return ""
	}
	if strings.TrimSpace(profile.DefaultModel) != "" {
		return strings.TrimSpace(profile.DefaultModel)
	}
	if len(profile.Models) > 0 {
		return strings.TrimSpace(profile.Models[0])
	}
	return ""
}

func (c *Claude) Run(profile *config.Profile, model string, args []string) error {
	claudePath, err := c.findPath()
	if err != nil {
		return fmt.Errorf("claude is not installed, install from https://code.claude.com/docs/en/quickstart")
	}
	effectiveModel := resolveClaudeModel(profile, model)
	if effectiveModel == "" {
		return fmt.Errorf("claude model is empty: configure profile default_model or pass --model")
	}
	cmdArgs := []string{}
	if effectiveModel != "" {
		cmdArgs = append(cmdArgs, "--model", effectiveModel)
	}
	cmdArgs = append(cmdArgs, args...)
	baseURL := anthropicBaseURL(profile)
	apiKey := profileKey(profile)
	token := ""
	usingCompatProxy := false
	quietCompatStderr := shouldQuietCompatStderr()

	// If user explicitly configured Anthropic endpoint, respect it.
	// Otherwise, use OpenAI profile config via local Anthropic->OpenAI proxy.
	if profile == nil || profile.AnthropicBaseURL == "" {
		proxy, err := startAnthropicCompatProxy(profileBase(profile), profileKey(profile), effectiveModel)
		if err != nil {
			return err
		}
		defer proxy.Close()
		baseURL = proxy.BaseURL()
		// Match Ollama's Claude launch behavior: key is required by client but ignored by backend.
		apiKey = ""
		token = "ollama"
		usingCompatProxy = true
		if !quietCompatStderr {
			fmt.Fprintf(os.Stderr, "Using anthropic compatibility adapter: %s -> %s\n", baseURL, profileBase(profile))
			fmt.Fprintf(os.Stderr, "Anthropic compatibility adapter log file: %s\n", proxy.LogPath())
		}
	}
	if profile != nil && profile.AnthropicAuthToken != "" {
		token = profile.AnthropicAuthToken
	}
	if !usingCompatProxy && token == "" {
		token = "ollama"
	}
	env := []string{
		"ANTHROPIC_BASE_URL=" + baseURL,
		"ANTHROPIC_API_KEY=" + apiKey,
		"ANTHROPIC_AUTH_TOKEN=" + token,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + effectiveModel,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + effectiveModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + effectiveModel,
		"CLAUDE_CODE_SUBAGENT_MODEL=" + effectiveModel,
	}
	return runCmd(claudePath, cmdArgs, env)
}
