package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	compatproxy "spark/internal/compat/proxy"
	"spark/internal/config"
)

type Codex struct{}

func (c *Codex) String() string { return "Codex" }

const codexProviderName = "spark"

func (c *Codex) args(model, baseURL string, extra []string) []string {
	return c.argsWithConfigAndPrompt(model, baseURL, nil, extra, nil)
}

func (c *Codex) argsWithPrompt(model, baseURL string, extra []string, prompt *config.PromptInjection) []string {
	return c.argsWithConfigAndPrompt(model, baseURL, nil, extra, prompt)
}

func (c *Codex) argsWithConfigAndPrompt(model, baseURL string, integration *config.IntegrationConfig, extra []string, prompt *config.PromptInjection) []string {
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
	cmdArgs = append(cmdArgs, codexPromptArgs(prompt)...)
	cmdArgs = append(cmdArgs, extra...)
	return cmdArgs
}

func (c *Codex) Run(profile *config.Profile, model string, args []string) error {
	return c.RunWithPrompt(profile, model, args, nil)
}

func (c *Codex) RunWithPrompt(profile *config.Profile, model string, args []string, prompt *config.PromptInjection) error {
	return c.RunWithConfigAndPrompt(profile, nil, model, args, prompt)
}

func (c *Codex) RunWithConfigAndPrompt(profile *config.Profile, integration *config.IntegrationConfig, model string, args []string, prompt *config.PromptInjection) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("codex is not installed, install with: npm install -g @openai/codex")
	}

	// For append mode, combine original prompt with custom content and write to temp file.
	if prompt != nil && prompt.Mode == config.PromptModeAppend {
		resolved, err := resolveCodexAppendPrompt(prompt)
		if err != nil {
			return err
		}
		prompt = resolved
		defer os.Remove(prompt.Path)
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
	quietCompatStderr := shouldQuietCompatStderr()

	envBaseURL := baseURL
	envKey := resolvedUpstreamKey

	if proxyMode, useProxy := codexProxyModeForAPIType(apiType); useProxy {
		proxy, err := compatproxy.StartResponsesProxy(baseURL, resolvedUpstreamKey, quietCompatStderr, proxyMode, model)
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

	cmdArgs := c.argsWithConfigAndPrompt(model, envBaseURL, integration, args, prompt)
	return runCmd("codex", cmdArgs, codexEnv(profile, envKey))
}

// codexPromptArgs returns the CLI args for file-based prompt injection.
// All modes use model_instructions_file with a file path.
func codexPromptArgs(prompt *config.PromptInjection) []string {
	if prompt == nil {
		return nil
	}
	if prompt.Path == "" {
		return nil
	}
	return []string{"-c", fmt.Sprintf("model_instructions_file=%s", prompt.Path)}
}

// resolveCodexAppendPrompt handles append mode by fetching the original
// Codex developer prompt and combining it with the custom content.
// Returns a new PromptInjection with mode=replace pointing to a temp file.
// Caller must remove the temp file when done.
func resolveCodexAppendPrompt(prompt *config.PromptInjection) (*config.PromptInjection, error) {
	original, err := fetchCodexDefaultPrompt()
	if err != nil {
		return nil, fmt.Errorf("fetch codex default prompt: %w", err)
	}
	combined := original
	if prompt.Content != "" {
		combined = original + "\n\n" + prompt.Content
	}
	tmpPath, err := writePromptTempFile(combined)
	if err != nil {
		return nil, err
	}
	return &config.PromptInjection{
		Mode:    config.PromptModeReplace,
		Path:    tmpPath,
		Content: combined,
	}, nil
}

// CodexModelInfo represents the model metadata from Codex model catalog.
type CodexModelInfo struct {
	Slug             string `json:"slug"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	BaseInstructions string `json:"base_instructions"`
	ContextWindow    int    `json:"context_window"`
}

// CodexModelCache represents the structure of models_cache.json.
type CodexModelCache struct {
	FetchedAt     string           `json:"fetched_at"`
	Etag          string           `json:"etag"`
	ClientVersion string           `json:"client_version"`
	Models        []CodexModelInfo `json:"models"`
}

// getCodexHome returns the Codex home directory path.
// Defaults to ~/.codex unless CODEX_HOME is set.
func getCodexHome() (string, error) {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".codex"), nil
}

// fetchCodexModelCatalog reads the models_cache.json file and returns all models.
func fetchCodexModelCatalog() ([]CodexModelInfo, error) {
	codexHome, err := getCodexHome()
	if err != nil {
		return nil, fmt.Errorf("get codex home: %w", err)
	}

	cachePath := filepath.Join(codexHome, "models_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("read models cache: %w", err)
	}

	var cache CodexModelCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse models cache: %w", err)
	}

	return cache.Models, nil
}

// fetchCodexModelInstructions returns the base_instructions for a specific model slug.
// If modelSlug is empty, returns the first model's instructions as default.
func fetchCodexModelInstructions(modelSlug string) (string, error) {
	models, err := fetchCodexModelCatalog()
	if err != nil {
		return "", err
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no models found in catalog")
	}

	// If no model slug specified, return first model's instructions
	if strings.TrimSpace(modelSlug) == "" {
		return models[0].BaseInstructions, nil
	}

	// Find exact match or prefix match
	for _, model := range models {
		if model.Slug == modelSlug || strings.HasPrefix(model.Slug, modelSlug) {
			return model.BaseInstructions, nil
		}
	}

	return "", fmt.Errorf("model not found: %s", modelSlug)
}

// fetchCodexDefaultPrompt reads the default model's base_instructions from models_cache.json.
// Falls back to the legacy `codex debug prompt-input` method if the cache file is not available.
func fetchCodexDefaultPrompt() (string, error) {
	// Try reading from models_cache.json first
	instructions, err := fetchCodexModelInstructions("")
	if err == nil {
		return instructions, nil
	}

	// Fallback to legacy method if cache file doesn't exist or is invalid
	cmd := exec.Command("codex", "debug", "prompt-input")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("codex debug prompt-input: %w", err)
	}
	return extractDeveloperPrompt(out)
}

// extractDeveloperPrompt parses the JSON output of `codex debug prompt-input`
// and returns the concatenated text of the developer message content.
func extractDeveloperPrompt(jsonOut []byte) (string, error) {
	var messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(jsonOut, &messages); err != nil {
		return "", fmt.Errorf("parse prompt-input JSON: %w", err)
	}
	for _, msg := range messages {
		if msg.Role == "developer" {
			var parts []string
			for _, c := range msg.Content {
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
			return strings.Join(parts, "\n"), nil
		}
	}
	return "", fmt.Errorf("no developer message found in prompt-input output")
}

func resolveOpenAIAPIKey(profileKey string) (key string, source string) {
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return k, "env.OPENAI_API_KEY"
	}
	if k := strings.TrimSpace(profileKey); k != "" {
		return k, "profile.api_key"
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
