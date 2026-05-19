package integrations

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	compatproxy "spark/internal/compat/proxy"
	"spark/internal/config"
)

type Codex struct{}

func (c *Codex) String() string { return "Codex" }

const codexProviderName = "spark"

func (c *Codex) args(model, baseURL string, extra []string) []string {
	cmdArgs := []string{
		"-c", fmt.Sprintf(`model_providers.%s.name="Spark"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.base_url="%s"`, codexProviderName, baseURL),
		"-c", fmt.Sprintf(`model_providers.%s.env_key="OPENAI_API_KEY"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.wire_api="responses"`, codexProviderName),
		"-c", fmt.Sprintf(`model_providers.%s.requires_openai_auth=false`, codexProviderName),
		"-c", fmt.Sprintf(`model_provider="%s"`, codexProviderName),
	}
	if model != "" {
		cmdArgs = append(cmdArgs, "-m", model)
	}
	cmdArgs = append(cmdArgs, extra...)
	return cmdArgs
}

func (c *Codex) Run(profile *config.Profile, model string, args []string) error {
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
	if config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages) &&
		!config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses) &&
		!config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions) {
		resolvedUpstreamKey, upstreamKeySource = resolveAnthropicAPIKey(profile)
	}
	quietCompatStderr := shouldQuietCompatStderr()

	envBaseURL := baseURL
	envKey := resolvedUpstreamKey

	if proxyMode, useProxy := codexProxyModeForAPIType(apiType); useProxy {
		proxy, err := compatproxy.StartResponsesProxy(baseURL, resolvedUpstreamKey, quietCompatStderr, proxyMode)
		if err != nil {
			return err
		}
		defer proxy.Close()
		envBaseURL = proxy.BaseURL()
		envKey = "spark-compat"
		routeLine := fmt.Sprintf("[route] mode=compat proxy_mode=%s upstream=%s proxy=%s log=%s api_type=%s upstream_key_source=%s upstream_key_set=%t", proxyMode, baseURL, envBaseURL, proxy.LogPath(), apiType, upstreamKeySource, strings.TrimSpace(resolvedUpstreamKey) != "")
		routeLogPath := appendLaunchRouteLog(routeLine)
		if routeLogPath != "" {
			routeLine = routeLine + " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
		if !quietCompatStderr {
			fmt.Fprintf(os.Stderr, "Using compatibility adapter: %s -> %s\n", envBaseURL, baseURL)
			fmt.Fprintf(os.Stderr, "Compatibility adapter log file: %s\n", proxy.LogPath())
		}
	} else {
		routeLine := fmt.Sprintf("[route] mode=direct upstream=%s api_type=%s upstream_key_source=%s upstream_key_set=%t", baseURL, apiType, upstreamKeySource, strings.TrimSpace(resolvedUpstreamKey) != "")
		routeLogPath := appendLaunchRouteLog(routeLine)
		if routeLogPath != "" {
			routeLine = routeLine + " route_log=" + routeLogPath
		}
		fmt.Fprintln(os.Stderr, routeLine)
	}

	cmdArgs := c.args(model, envBaseURL, args)
	return runCmd("codex", cmdArgs, codexEnv(profile, envKey))
}

func resolveOpenAIAPIKey(profileKey string) (key string, source string) {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k, "env.OPENAI_API_KEY"
	}
	if k := strings.TrimSpace(profileKey); k != "" {
		return k, "profile.openai_api_key"
	}
	return "", "none"
}

func codexProxyModeForAPIType(apiType string) (compatproxy.ResponsesProxyMode, bool) {
	supportsResponses := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses)
	supportsChatCompletions := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions)
	supportsAnthropicMessages := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages)
	if supportsResponses && supportsChatCompletions {
		return compatproxy.ResponsesProxyModePreferResponses, true
	}
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
