package integrations

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"spark/internal/config"
)

const (
	grokSparkModelPrefix = "spark-"
	// grokSparkAPIKeyEnv is the env var name written as env_key on spark-managed
	// models. The secret is injected into the child process at Run.
	grokSparkAPIKeyEnv = "SPARK_GROK_API_KEY"

	grokBackendResponses       = "responses"
	grokBackendChatCompletions = "chat_completions"
	grokBackendMessages        = "messages"

	grokAnthropicVersion = "2023-06-01"
)

// grokLaunchRoute is the direct-connect decision for a Spark-managed Grok model.
// Grok Build speaks responses / chat_completions / messages natively, so Spark
// picks api_backend + base_url instead of starting a local compat proxy.
type grokLaunchRoute struct {
	APIBackend     string
	BaseURL        string
	// ExtraHeaders are static request headers (no secrets).
	ExtraHeaders map[string]string
	// EnvHTTPHeaders maps header name → env var name (secrets stay out of TOML).
	EnvHTTPHeaders map[string]string
	Reason         string
	Degraded       bool
}

type Grok struct{}

func (g *Grok) String() string { return "Grok Build" }

func (g *Grok) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".grok", "config.toml")}
}

func (g *Grok) Models() []string { return nil }

func (g *Grok) Edit(profile *config.Profile, models []string) error {
	normalized := normalizeGrokModels(models)
	if len(normalized) == 0 {
		return fmt.Errorf("no models selected")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".grok", "config.toml")
	if err := ensureDir(path); err != nil {
		return err
	}

	root, err := loadGrokConfigMap(path)
	if err != nil {
		return err
	}

	modelTable, _ := root["model"].(map[string]any)
	if modelTable == nil {
		modelTable = map[string]any{}
	}
	// Drop previous spark-managed model entries only.
	for key := range modelTable {
		if strings.HasPrefix(key, grokSparkModelPrefix) {
			delete(modelTable, key)
		}
	}

	route := grokLaunchRouteForProfile(profile)
	for _, mdl := range normalized {
		key := grokModelKey(mdl)
		modelTable[key] = grokSparkModelEntry(mdl, route)
	}
	root["model"] = modelTable

	// Never make a spark-managed model the persistent default: with an active
	// grok login session, Grok Build may send OAuth JWT to the custom base_url
	// and fail with 401. Keep OAuth on built-in defaults for plain `grok`.
	if modelsSection, ok := root["models"].(map[string]any); ok {
		if def, _ := modelsSection["default"].(string); strings.HasPrefix(strings.TrimSpace(def), grokSparkModelPrefix) {
			modelsSection["default"] = "grok-4.5"
			root["models"] = modelsSection
		}
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(root); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func (g *Grok) Run(profile *config.Profile, model string, args []string) error {
	bin, err := findGrokBinary()
	if err != nil {
		return err
	}
	modelID := normalizeGrokModelID(model)
	if modelID == "" {
		return fmt.Errorf("model cannot be empty")
	}
	// Keep the user's ~/.grok catalog in sync (and fix a spark-* default if any).
	if err := g.Edit(profile, []string{modelID}); err != nil {
		return err
	}

	// Launch in an isolated GROK_HOME without auth.json. When ~/.grok/auth.json
	// is present, Grok Build's agent harness often prefers the OAuth session JWT
	// over model api_key/env_key and then 401s against third-party gateways.
	// Isolation preserves plain `grok` OAuth while making Spark launches BYOK-only.
	//
	// The launch home mirrors the real ~/.grok (skills, MCP credentials, plugins,
	// sessions, marketplace, …) via symlinks, and reuses the real config.toml
	// with only the spark model default overridden for this process.
	launchHome, err := os.MkdirTemp("", "spark-grok-home-*")
	if err != nil {
		return fmt.Errorf("create grok launch home: %w", err)
	}
	defer func() { _ = os.RemoveAll(launchHome) }()

	modelKey := grokModelKey(modelID)
	if err := writeGrokLaunchHome(launchHome, profile, modelID, modelKey); err != nil {
		return err
	}

	route := grokLaunchRouteForProfile(profile)
	logGrokLaunchRoute(profile, route)

	cmdArgs := append([]string{}, args...)
	if !cliArgsHasModelFlag(cmdArgs) {
		cmdArgs = append([]string{"--model", modelKey}, cmdArgs...)
	}
	env := append(grokEnv(profile), "GROK_HOME="+launchHome)
	return runCmd(bin, cmdArgs, env)
}

// writeGrokLaunchHome builds a launch-time GROK_HOME that:
//   - reuses the user's real ~/.grok assets (MCP, skills, plugins, sessions, …)
//   - never links auth.json (forces BYOK / env_key for the spark model)
//   - writes a config.toml cloned from the real one with the spark model as default
func writeGrokLaunchHome(home string, profile *config.Profile, modelID, modelKey string) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	realHome, err := realGrokHome()
	if err != nil {
		return err
	}
	if err := mirrorGrokHome(realHome, home); err != nil {
		return err
	}
	return writeGrokLaunchConfig(home, realHome, profile, modelID, modelKey)
}

func realGrokHome() (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".grok"), nil
}

// mirrorGrokHome symlinks every entry from the real GROK home into the launch
// home, except auth artifacts and config.toml (which is rewritten for BYOK).
func mirrorGrokHome(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read grok home: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkipGrokMirror(name) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		// Prefer absolute symlink targets so the launch home is relocatable.
		absSrc, err := filepath.Abs(srcPath)
		if err != nil {
			absSrc = srcPath
		}
		if err := os.Symlink(absSrc, dstPath); err != nil {
			return fmt.Errorf("link %s: %w", name, err)
		}
	}
	return nil
}

func shouldSkipGrokMirror(name string) bool {
	switch name {
	case "config.toml", "auth.json", "auth.json.lock":
		return true
	}
	// Any other auth.* lock/backup must stay out of the BYOK launch home.
	return strings.HasPrefix(name, "auth.json")
}

// writeGrokLaunchConfig clones the real config.toml, injects the spark model
// entry, and sets models.default to that entry for this launch only.
func writeGrokLaunchConfig(launchHome, realHome string, profile *config.Profile, modelID, modelKey string) error {
	cfgPath := filepath.Join(launchHome, "config.toml")
	root, err := loadGrokConfigMap(filepath.Join(realHome, "config.toml"))
	if err != nil {
		return err
	}
	if root == nil {
		root = map[string]any{}
	}

	modelTable, _ := root["model"].(map[string]any)
	if modelTable == nil {
		modelTable = map[string]any{}
	}
	modelTable[modelKey] = grokSparkModelEntry(modelID, grokLaunchRouteForProfile(profile))
	root["model"] = modelTable

	modelsSection, _ := root["models"].(map[string]any)
	if modelsSection == nil {
		modelsSection = map[string]any{}
	}
	modelsSection["default"] = modelKey
	modelsSection["web_search"] = modelKey
	root["models"] = modelsSection

	uiSection, _ := root["ui"].(map[string]any)
	if uiSection == nil {
		uiSection = map[string]any{}
	}
	uiSection["fork_secondary_model"] = modelKey
	root["ui"] = uiSection

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(root); err != nil {
		return err
	}
	return os.WriteFile(cfgPath, buf.Bytes(), 0o644)
}

// grokEnv injects the profile API key for spark-managed models (env_key).
func grokEnv(profile *config.Profile) []string {
	key := strings.TrimSpace(profileKey(profile))
	if key == "" {
		return []string{}
	}
	return []string{grokSparkAPIKeyEnv + "=" + key}
}

func findGrokBinary() (string, error) {
	if p, err := exec.LookPath("grok"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("grok is not installed, install from https://x.ai/cli")
	}
	for _, candidate := range []string{
		filepath.Join(home, ".local", "bin", "grok"),
		filepath.Join(home, ".grok", "bin", "grok"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("grok is not installed, install from https://x.ai/cli")
}

func loadGrokConfigMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	root := map[string]any{}
	if _, err := toml.Decode(string(data), &root); err != nil {
		return nil, fmt.Errorf("parse grok config: %w", err)
	}
	return root, nil
}

// grokLaunchRouteForProfile picks the best native Grok api_backend for direct
// connect. Priority: responses > chat_completions > anthropic messages.
// Multi-type profiles that include responses (including the default auto type)
// use responses. No local compat proxy is started — Grok speaks all three.
func grokLaunchRouteForProfile(profile *config.Profile) grokLaunchRoute {
	apiType := profileOpenAIAPIType(profile)
	supportsResponses := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses)
	supportsChat := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions)
	supportsAnthropic := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages)
	supportsGemini := config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeGeminiGenerateContent)

	switch {
	case supportsResponses:
		reason := "prefer_responses"
		if !supportsChat && !supportsAnthropic && !supportsGemini {
			reason = "responses_only"
		}
		return grokLaunchRoute{
			APIBackend: grokBackendResponses,
			BaseURL:    profileBase(profile),
			Reason:     reason,
		}
	case supportsChat:
		return grokLaunchRoute{
			APIBackend: grokBackendChatCompletions,
			BaseURL:    profileBase(profile),
			Reason:     "chat_only",
		}
	case supportsAnthropic:
		return grokAnthropicMessagesRoute(profile)
	case supportsGemini:
		return grokLaunchRoute{
			APIBackend: grokBackendChatCompletions,
			BaseURL:    profileBase(profile),
			Reason:     "gemini_unsupported_fallback_chat",
			Degraded:   true,
		}
	default:
		return grokLaunchRoute{
			APIBackend: grokBackendChatCompletions,
			BaseURL:    profileBase(profile),
			Reason:     "unknown_fallback_chat",
			Degraded:   true,
		}
	}
}

func grokAnthropicMessagesRoute(profile *config.Profile) grokLaunchRoute {
	baseURL := profileBase(profile)
	reason := "anthropic_messages"
	if profile != nil && strings.TrimSpace(profile.AnthropicBaseURL) != "" {
		baseURL = strings.TrimSpace(profile.AnthropicBaseURL)
		reason = "anthropic_messages_anthropic_base"
	}
	return grokLaunchRoute{
		APIBackend: grokBackendMessages,
		BaseURL:    baseURL,
		// Static version header; secret key rides env_http_headers so it never
		// lands in config.toml (Grok docs: prefer env_http_headers for secrets).
		ExtraHeaders: map[string]string{
			"anthropic-version": grokAnthropicVersion,
		},
		EnvHTTPHeaders: map[string]string{
			"x-api-key": grokSparkAPIKeyEnv,
		},
		Reason: reason,
	}
}

// grokAPIBackend returns the api_backend string for a profile (test/helper).
func grokAPIBackend(profile *config.Profile) string {
	return grokLaunchRouteForProfile(profile).APIBackend
}

// grokSparkModelEntry builds a [model.*] TOML entry for a spark-managed model.
// Prefer env_key / env_http_headers over api_key so secrets stay out of disk.
func grokSparkModelEntry(modelID string, route grokLaunchRoute) map[string]any {
	entry := map[string]any{
		"model":       modelID,
		"base_url":    route.BaseURL,
		"name":        modelID + " (Spark)",
		"api_backend": route.APIBackend,
		"env_key":     grokSparkAPIKeyEnv,
	}
	if len(route.ExtraHeaders) > 0 {
		headers := make(map[string]any, len(route.ExtraHeaders))
		for k, v := range route.ExtraHeaders {
			headers[k] = v
		}
		entry["extra_headers"] = headers
	}
	if len(route.EnvHTTPHeaders) > 0 {
		headers := make(map[string]any, len(route.EnvHTTPHeaders))
		for k, v := range route.EnvHTTPHeaders {
			headers[k] = v
		}
		entry["env_http_headers"] = headers
	}
	return entry
}

func logGrokLaunchRoute(profile *config.Profile, route grokLaunchRoute) {
	apiType := profileOpenAIAPIType(profile)
	keySet := strings.TrimSpace(profileKey(profile)) != ""
	routeLine := fmt.Sprintf(
		"[route] mode=direct integration=grok api_backend=%s upstream=%s api_type=%s reason=%s degraded=%t key_set=%t",
		route.APIBackend, route.BaseURL, apiType, route.Reason, route.Degraded, keySet,
	)
	if path := appendLaunchRouteLog(routeLine); path != "" {
		routeLine = routeLine + " route_log=" + path
	}
	fmt.Fprintln(os.Stderr, routeLine)
}

func normalizeGrokModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, mdl := range models {
		id := normalizeGrokModelID(mdl)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizeGrokModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	// Strip spark-managed config key if a user pastes it back in.
	if strings.HasPrefix(model, grokSparkModelPrefix) {
		rest := strings.TrimPrefix(model, grokSparkModelPrefix)
		if rest != "" {
			return rest
		}
	}
	// Accept spark/<model> form for consistency with OpenCode-style IDs.
	if i := strings.Index(model, "/"); i >= 0 {
		provider := strings.TrimSpace(model[:i])
		id := strings.TrimSpace(model[i+1:])
		if strings.EqualFold(provider, "spark") && id != "" {
			return id
		}
	}
	return model
}

func grokModelKey(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return grokSparkModelPrefix + "model"
	}
	var b strings.Builder
	b.WriteString(grokSparkModelPrefix)
	lastDash := false
	for _, r := range modelID {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	key := strings.Trim(b.String(), "-")
	if key == "" || key == strings.TrimSuffix(grokSparkModelPrefix, "-") {
		return grokSparkModelPrefix + "model"
	}
	// Ensure prefix is present after trimming.
	if !strings.HasPrefix(key, grokSparkModelPrefix) {
		key = grokSparkModelPrefix + key
	}
	return key
}
