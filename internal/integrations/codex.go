package integrations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"spark/internal/auth"
	"spark/internal/compat/daemon"
	compatproxy "spark/internal/compat/proxy"
	"spark/internal/config"
)

type Codex struct{}

func (c *Codex) String() string { return "Codex" }

const codexProviderName = "spark"

func (c *Codex) args(model, baseURL string, extra []string) []string {
	return c.argsWithIntegration(model, baseURL, nil, extra)
}

func (c *Codex) argsWithIntegration(model, baseURL string, integration *config.IntegrationConfig, extra []string) []string {
	cmdArgs := []string{
		"-c", fmt.Sprintf(`model_providers.%s.name="Spark"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.base_url="%s"`, codexProviderName, baseURL),
		"-c", fmt.Sprintf(`model_providers.%s.env_key="OPENAI_API_KEY"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.wire_api="responses"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.requires_openai_auth=false`, codexProviderName),
		"-c", fmt.Sprintf(`model_provider="%s"`, codexProviderName),
	}
	if integration != nil && strings.TrimSpace(integration.ModelCatalogJSON) != "" {
		cmdArgs = append(cmdArgs, "-c", fmt.Sprintf("model_catalog_json=%q", strings.TrimSpace(integration.ModelCatalogJSON)))
	}
	if model != "" {
		cmdArgs = append(cmdArgs, "-m", model)
	}
	cmdArgs = append(cmdArgs, extra...)
	return cmdArgs
}

func (c *Codex) Run(profile *config.Profile, model string, args []string) error {
	return c.RunWithIntegration(profile, nil, model, args)
}

func (c *Codex) RunWithIntegration(profile *config.Profile, integration *config.IntegrationConfig, model string, args []string) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("codex is not installed, install with: npm install -g @openai/codex")
	}

	apiType := profileOpenAIAPIType(profile)
	baseURL := profileBase(profile)
	if config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages) &&
		!config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses) &&
		!config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions) &&
		profile != nil && strings.TrimSpace(profile.AnthropicBaseURL) != "" {
		baseURL = anthropicBaseURL(profile)
	}
	apiKey := profileKey(profile)
	resolvedUpstreamKey, upstreamKeySource := resolveOpenAIAPIKey(apiKey)
	envBaseURL := baseURL
	envKey := resolvedUpstreamKey
	if os.Getenv("SPARK_DIRECT") == "1" {
		routeLine := fmt.Sprintf("[route] mode=direct upstream=%s api_type=%s upstream_key_source=%s upstream_key_set=%t", baseURL, apiType, upstreamKeySource, strings.TrimSpace(resolvedUpstreamKey) != "")
		routeLogPath := appendLaunchRouteLog(routeLine)
		if routeLogPath != "" {
			routeLine = routeLine + " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	} else {
		dInfo, err := daemon.EnsureDaemon(context.Background(), nil)
		if err != nil || dInfo == nil {
			if err != nil {
				return fmt.Errorf("start shared Spark daemon: %w", err)
			}
			return fmt.Errorf("start shared Spark daemon: no daemon information returned")
		}
		envBaseURL = dInfo.BaseURL + "/v1"
		envKey = daemonProfileToken(profile)
		routeLine := fmt.Sprintf("[route] mode=shared-daemon upstream=%s proxy=%s api_type=%s daemon_pid=%d", baseURL, envBaseURL, apiType, dInfo.PID)
		if routeLogPath := appendLaunchRouteLog(routeLine); routeLogPath != "" {
			routeLine += " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	}

	cmdArgs := c.argsWithIntegration(model, envBaseURL, integration, nil)
	if sparkCfg, _ := config.Load(); sparkCfg != nil {
		cmdArgs = append(cmdArgs, codexMCPArgs(sparkCfg.McpServersForAgent("codex"))...)
	}
	cmdArgs = append(cmdArgs, args...)
	return runCmd("codex", cmdArgs, codexEnv(profile, envKey))
}

// codexMCPArgs encodes the codex-enabled MCP servers (binding overrides
// applied) as Codex `-c` config overrides so Codex receives them per-process
// without rewriting ~/.codex/config.toml.
func codexMCPArgs(servers map[string]*config.McpServerConfig) []string {
	if len(servers) == 0 {
		return nil
	}
	names := make([]string, 0, len(servers))
	for name, srv := range servers {
		if srv != nil && srv.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		srv := servers[name]
		prefix := "mcp_servers." + config.McpServerName(name)
		if srv.Command != "" {
			out = append(out, "-c", fmt.Sprintf("%s.command=%s", prefix, strconv.Quote(srv.Command)))
		}
		if len(srv.Args) > 0 {
			quoted := make([]string, len(srv.Args))
			for i, a := range srv.Args {
				quoted[i] = strconv.Quote(a)
			}
			out = append(out, "-c", fmt.Sprintf("%s.args=[%s]", prefix, strings.Join(quoted, ", ")))
		}
		if len(srv.Env) > 0 {
			envKeys := make([]string, 0, len(srv.Env))
			for k := range srv.Env {
				envKeys = append(envKeys, k)
			}
			sort.Strings(envKeys)
			for _, k := range envKeys {
				out = append(out, "-c", fmt.Sprintf("%s.env.%s=%s", prefix, k, strconv.Quote(srv.Env[k])))
			}
		}
		if srv.URL != "" {
			out = append(out, "-c", fmt.Sprintf("%s.url=%s", prefix, strconv.Quote(srv.URL)))
		}
		if srv.StartupTimeout != nil {
			out = append(out, "-c", fmt.Sprintf("%s.startup_timeout_sec=%d", prefix, *srv.StartupTimeout))
		}
		if srv.ToolTimeout != nil {
			out = append(out, "-c", fmt.Sprintf("%s.tool_timeout_sec=%d", prefix, *srv.ToolTimeout))
		}
	}
	return out
}

func resolveOpenAIAPIKey(profileKey string) (key string, source string) {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k, "env.OPENAI_API_KEY"
	}
	if k := strings.TrimSpace(profileKey); k != "" {
		return k, "profile.api_key"
	}
	if store, err := auth.DefaultStore(); err == nil && store != nil {
		if a, err := store.Get(auth.ProviderCodex); err == nil && a != nil && a.AccessToken != "" && !a.Expired(0) {
			return a.AccessToken, "oauth.codex"
		}
	}
	return "", "none"
}

func codexProxyModeForAPIType(apiType string) (compatproxy.ResponsesProxyMode, bool) {
	supportsResponses := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses)
	supportsChatCompletions := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions)
	supportsAnthropicMessages := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages)
	if supportsResponses {
		return "", false
	}
	if supportsAnthropicMessages {
		return compatproxy.ResponsesProxyModeAnthropicMessagesOnly, true
	}
	if supportsChatCompletions {
		return compatproxy.ResponsesProxyModeChatCompletionsOnly, true
	}
	return compatproxy.ResponsesProxyModeChatCompletionsOnly, true
}

func codexEnv(profile *config.Profile, envKey string) []string {
	env := []string{
		"OPENAI_ORG_ID=" + profile.OpenAIOrg,
		"OPENAI_PROJECT_ID=" + profile.OpenAIProject,
	}
	if strings.TrimSpace(envKey) != "" {
		env = append(env,
			"OPENAI_API_KEY="+envKey,
			"CODEX_API_KEY="+envKey,
		)
	}
	return env
}
