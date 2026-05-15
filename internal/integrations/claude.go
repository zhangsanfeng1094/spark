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
	m := config.NormalizeModel(model)
	if m != "" {
		return m
	}
	if profile == nil {
		return ""
	}
	if m = config.NormalizeModel(profile.DefaultModel); m != "" {
		return m
	}
	if len(profile.Models) > 0 {
		return config.NormalizeModel(profile.Models[0])
	}
	return ""
}

func resolveAnthropicAPIKey(profile *config.Profile) (string, string) {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return k, "env.ANTHROPIC_API_KEY"
	}
	if k := strings.TrimSpace(profileKey(profile)); k != "" {
		return k, "profile.openai_api_key"
	}
	return "", "none"
}

func resolveAnthropicAuthToken(profile *config.Profile, usingCompatProxy bool) (string, string) {
	if t := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); t != "" {
		return t, "env.ANTHROPIC_AUTH_TOKEN"
	}
	if profile != nil && strings.TrimSpace(profile.AnthropicAuthToken) != "" {
		return strings.TrimSpace(profile.AnthropicAuthToken), "profile.anthropic_auth_token"
	}
	if usingCompatProxy {
		return "ollama", "compat.default"
	}
	return "ollama", "default"
}

func claudeShouldUseCompatProxy(profile *config.Profile) bool {
	if profile == nil {
		return true
	}
	return !config.SupportsOpenAIAPIType(profileOpenAIAPIType(profile), config.OpenAIAPITypeAnthropicMessages)
}

func selectClaudeDirectAuth(apiKey, apiKeySource, token, tokenSource string) (string, string, string, string) {
	apiKey = strings.TrimSpace(apiKey)
	token = strings.TrimSpace(token)
	if apiKey == "" || token == "" {
		return apiKey, apiKeySource, token, tokenSource
	}
	if tokenSource == "default" || tokenSource == "compat.default" {
		return "", "none", apiKey, apiKeySource
	}
	return "", "none", token, tokenSource
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
	apiKey, apiKeySource := resolveAnthropicAPIKey(profile)
	token := ""
	tokenSource := "none"
	usingCompatProxy := false
	quietCompatStderr := shouldQuietCompatStderr()
	upstreamKey, upstreamKeySource := resolveOpenAIAPIKey(profileKey(profile))

	// Use direct mode when the selected API type is Anthropic Messages.
	if claudeShouldUseCompatProxy(profile) {
		proxy, err := startAnthropicCompatProxy(profileBase(profile), upstreamKey, effectiveModel)
		if err != nil {
			return err
		}
		defer proxy.Close()
		baseURL = proxy.BaseURL()
		// Match Ollama's Claude launch behavior: key is required by client but ignored by backend.
		apiKey = ""
		usingCompatProxy = true
		token, tokenSource = resolveAnthropicAuthToken(profile, usingCompatProxy)
		routeLine := fmt.Sprintf("[route] mode=compat integration=claude upstream=%s proxy=%s log=%s upstream_key_source=%s upstream_key_set=%t", profileBase(profile), baseURL, proxy.LogPath(), upstreamKeySource, strings.TrimSpace(upstreamKey) != "")
		routeLogPath := appendLaunchRouteLog(routeLine)
		if routeLogPath != "" {
			routeLine = routeLine + " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
		if !quietCompatStderr {
			fmt.Fprintf(os.Stderr, "Using anthropic compatibility adapter: %s -> %s\n", baseURL, profileBase(profile))
			fmt.Fprintf(os.Stderr, "Anthropic compatibility adapter log file: %s\n", proxy.LogPath())
		}
	} else {
		token, tokenSource = resolveAnthropicAuthToken(profile, usingCompatProxy)
		apiKey, apiKeySource, token, tokenSource = selectClaudeDirectAuth(apiKey, apiKeySource, token, tokenSource)
		routeLine := fmt.Sprintf("[route] mode=direct integration=claude upstream=%s api_key_source=%s api_key_set=%t token_source=%s token_set=%t", baseURL, apiKeySource, strings.TrimSpace(apiKey) != "", tokenSource, strings.TrimSpace(token) != "")
		routeLogPath := appendLaunchRouteLog(routeLine)
		if routeLogPath != "" {
			routeLine = routeLine + " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	}
	env := []string{
		"ANTHROPIC_BASE_URL=" + baseURL,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + effectiveModel,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + effectiveModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + effectiveModel,
		"CLAUDE_CODE_SUBAGENT_MODEL=" + effectiveModel,
		"CLAUDE_CODE_ATTRIBUTION_HEADER=0",
	}
	if strings.TrimSpace(token) != "" {
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+token)
	}
	if strings.TrimSpace(apiKey) != "" {
		env = append(env, "ANTHROPIC_API_KEY="+apiKey)
	}
	return runCmd(claudePath, cmdArgs, env)
}
