package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"spark/internal/auth"
	"spark/internal/compat/daemon"
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
	if profile != nil {
		if profile.AnthropicBaseURL != "" {
			return profile.AnthropicBaseURL
		}
		if ep := profile.EffectiveEndpoint(); ep != "" {
			return ep
		}
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

func resolveClaudeDirectToken(profile *config.Profile) (string, string) {
	if k := strings.TrimSpace(profileKey(profile)); k != "" {
		return k, "profile.api_key"
	}
	if store, err := auth.DefaultStore(); err == nil && store != nil {
		if profile != nil && profile.EffectiveAuthRef() != "" {
			if rec, err := store.GetByRef(profile.EffectiveAuthRef()); err == nil && rec != nil && rec.AccessToken != "" && !rec.Expired(0) {
				return rec.AccessToken, "auth_ref." + profile.EffectiveAuthRef()
			}
		}
		if a, err := store.Get(auth.ProviderClaude); err == nil && a != nil && a.AccessToken != "" && !a.Expired(0) {
			return a.AccessToken, "oauth.claude"
		}
	}
	return "", "none"
}

func resolveClaudeCompatToken(usingCompatProxy bool) (string, string) {
	if usingCompatProxy {
		return "ollama", "compat.default"
	}
	return "", "none"
}

func claudeShouldUseCompatProxy(profile *config.Profile) bool {
	if profile == nil {
		return true
	}
	return !config.SupportsOpenAIAPIType(profileOpenAIAPIType(profile), config.OpenAIAPITypeAnthropicMessages)
}

func selectClaudeDirectAuth(token, tokenSource string) (string, string, string, string) {
	token = strings.TrimSpace(token)
	if tokenSource == "default" || tokenSource == "compat.default" {
		return "", "none", "", "none"
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

	mcpPath, err := writeClaudeTempMCP()
	if err != nil {
		return err
	}
	if mcpPath != "" {
		defer func() { _ = os.Remove(mcpPath) }()
	}

	cmdArgs := []string{}
	if effectiveModel != "" {
		cmdArgs = append(cmdArgs, "--model", effectiveModel)
	}
	if mcpPath != "" && !cliArgsHasMcpConfigFlag(args) {
		cmdArgs = append(cmdArgs, "--mcp-config", mcpPath)
	}
	cmdArgs = append(cmdArgs, args...)
	baseURL := anthropicBaseURL(profile)
	apiKey := ""
	apiKeySource := "none"
	token := ""
	tokenSource := "none"
	if os.Getenv("SPARK_DIRECT") != "1" {
		dInfo, err := daemon.EnsureDaemon(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("start shared Spark daemon: %w", err)
		}
		if dInfo == nil {
			return fmt.Errorf("start shared Spark daemon: no daemon information returned")
		}
		baseURL = dInfo.BaseURL
		apiKey = ""
		token = daemonProfileToken(profile)
		tokenSource = "spark-profile"
		routeLine := fmt.Sprintf("[route] mode=shared-daemon integration=claude upstream=%s proxy=%s daemon_pid=%d", profileBase(profile), baseURL, dInfo.PID)
		if routeLogPath := appendLaunchRouteLog(routeLine); routeLogPath != "" {
			routeLine += " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	} else {
		token, tokenSource = resolveClaudeDirectToken(profile)
		apiKey, apiKeySource, token, tokenSource = selectClaudeDirectAuth(token, tokenSource)
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

// writeClaudeTempMCP writes a temporary MCP config file combining enabled user
// Claude MCP servers and enabled Spark MCP servers for --mcp-config injection.
func writeClaudeTempMCP() (string, error) {
	userServers, _ := config.LoadClaudeUserMcpServers("")
	sparkCfg, _ := config.Load()

	merged := make(map[string]*config.McpServerConfig)
	for name, srv := range userServers {
		if srv != nil && srv.Enabled {
			merged[name] = srv
		}
	}
	if sparkCfg != nil {
		for name, srv := range sparkCfg.McpServersForAgent("claude") {
			merged[name] = srv
		}
	}

	if len(merged) == 0 {
		return "", nil
	}

	encodedServers := make(map[string]any, len(merged))
	for name, srv := range merged {
		encodedServers[name] = config.EncodeClaudeServerMap(srv)
	}
	root := map[string]any{
		"mcpServers": encodedServers,
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "spark-claude-mcp-*.json")
	if err != nil {
		return "", err
	}
	mcpPath := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(mcpPath)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(mcpPath)
		return "", err
	}
	return mcpPath, nil
}

func cliArgsHasMcpConfigFlag(args []string) bool {
	for _, a := range args {
		if a == "--mcp-config" || strings.HasPrefix(a, "--mcp-config=") {
			return true
		}
	}
	return false
}
