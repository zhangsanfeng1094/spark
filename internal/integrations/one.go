package integrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"spark/internal/config"
)

const oneProviderID = "spark"

// Wire protocol names understood by `one` (models.json api / providerType and
// the --openai-api flag). One speaks all four natively, so Spark always
// direct-connects — no local compat proxy is needed.
const (
	oneWireOpenAIResponses = "openai-responses"
	oneWireOpenAIChat      = "openai-completions"
	oneWireAnthropic       = "anthropic-messages"
	oneWireGemini          = "gemini-generate-content"
)

type One struct{}

func (o *One) String() string { return "One" }

func (o *One) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".one", "agent", "models.json")}
}

func (o *One) Models() []string { return nil }

func (o *One) NeedsEdit(profile *config.Profile, models []string) (bool, error) {
	normalized := normalizeOneModels(models)
	if len(normalized) == 0 {
		return false, fmt.Errorf("no models selected")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	root := readMap(filepath.Join(home, ".one", "agent", "models.json"))
	return oneConfigNeedsSparkEdit(root, profile, normalized), nil
}

func oneConfigNeedsSparkEdit(root map[string]any, profile *config.Profile, models []string) bool {
	providers, _ := root["providers"].(map[string]any)
	if providers == nil {
		return true
	}
	existing, _ := providers[oneProviderID].(map[string]any)
	if existing == nil {
		return true
	}
	expected := oneSparkProviderEntry(profile, models, false)
	return !reflect.DeepEqual(existing, expected)
}

// Edit syncs the real ~/.one/agent/models.json with a spark provider entry so
// the models show up in one's own picker. The entry carries no apiKey and
// settings.json is never touched: provider selection happens only in the
// isolated launch home (see Run), so the user's own one defaults stay as they
// are — same policy as the Grok Build integration.
func (o *One) Edit(profile *config.Profile, models []string) error {
	normalized := normalizeOneModels(models)
	if len(normalized) == 0 {
		return fmt.Errorf("no models selected")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	modelsPath := filepath.Join(home, ".one", "agent", "models.json")
	if err := ensureDir(modelsPath); err != nil {
		return err
	}
	root := readMap(modelsPath)
	if !oneConfigNeedsSparkEdit(root, profile, normalized) {
		return nil
	}
	providers, _ := root["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	providers[oneProviderID] = oneSparkProviderEntry(profile, normalized, false)
	root["providers"] = providers
	return writeJSON(modelsPath, root)
}

func (o *One) Run(profile *config.Profile, model string, args []string) error {
	bin, err := findOneBinary()
	if err != nil {
		return err
	}
	modelID := normalizeOneModelID(model)
	if modelID == "" {
		return fmt.Errorf("model cannot be empty")
	}

	if err := syncOneMCP(); err != nil {
		return err
	}

	logOneLaunchRoute(profile)

	cmdArgs := oneLaunchArgs(profile, modelID, args)
	env := oneLaunchEnv(profile)
	return runCmd(bin, cmdArgs, env)
}

func oneLaunchArgs(profile *config.Profile, modelID string, extra []string) []string {
	wire := oneWireAPIForProfile(profile)
	baseURL := oneBaseURLForProfile(profile, wire)
	var cmdArgs []string
	if !cliArgsHasFlag(extra, "--provider") {
		cmdArgs = append(cmdArgs, "--provider", "openai")
	}
	if !cliArgsHasFlag(extra, "--openai-api") {
		cmdArgs = append(cmdArgs, "--openai-api", wire)
	}
	if baseURL != "" && !cliArgsHasFlag(extra, "--base-url") {
		cmdArgs = append(cmdArgs, "--base-url", baseURL)
	}
	if !cliArgsHasModelFlag(extra) {
		cmdArgs = append(cmdArgs, "--model", modelID)
	}
	cmdArgs = append(cmdArgs, extra...)
	return cmdArgs
}

func oneLaunchEnv(profile *config.Profile) []string {
	wire := oneWireAPIForProfile(profile)
	baseURL := oneBaseURLForProfile(profile, wire)
	env := []string{
		"ONE_OPENAI_API=" + wire,
	}
	if baseURL != "" {
		env = append(env, "OPENAI_BASE_URL="+baseURL)
	}
	if key := strings.TrimSpace(profileKey(profile)); key != "" {
		env = append(env, "OPENAI_API_KEY="+key)
	}
	return env
}

func cliArgsHasFlag(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func syncOneMCP() error {
	sparkCfg, _ := config.Load()
	enabled := map[string]*config.McpServerConfig{}
	if sparkCfg != nil {
		enabled = sparkCfg.McpServersForAgent("one")
	}
	if len(enabled) == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	mcpPath := filepath.Join(home, ".one", "agent", "mcp.json")
	if err := ensureDir(mcpPath); err != nil {
		return err
	}
	root := readMap(mcpPath)
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	for name, srv := range enabled {
		servers[name] = config.EncodeClaudeServerMap(srv)
	}
	root["mcpServers"] = servers
	return writeJSON(mcpPath, root)
}

// oneSparkProviderEntry builds the models.json provider entry for spark. With
// includeKey the apiKey is embedded (launch-home clone only); the on-disk
// catalog sync (Edit) omits it so secrets never land in ~/.one.
func oneSparkProviderEntry(profile *config.Profile, models []string, includeKey bool) map[string]any {
	wire := oneWireAPIForProfile(profile)
	modelEntries := make([]any, 0, len(models))
	for _, mdl := range models {
		modelEntries = append(modelEntries, map[string]any{"id": mdl, "name": mdl})
	}
	entry := map[string]any{
		"providerType": wire,
		"api":          wire,
		"baseUrl":      oneBaseURLForProfile(profile, wire),
		"models":       modelEntries,
	}
	if includeKey {
		if key := strings.TrimSpace(profileKey(profile)); key != "" {
			entry["apiKey"] = key
		}
	}
	return entry
}

func findOneBinary() (string, error) {
	if p, err := exec.LookPath("one"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("one is not installed, install from https://github.com/one-coding-agent")
	}
	for _, candidate := range []string{
		filepath.Join(home, ".cargo", "bin", "one"),
		filepath.Join(home, ".local", "bin", "one"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("one is not installed, install from https://github.com/one-coding-agent")
}

// oneWireAPIForProfile maps the profile's upstream API types onto one's wire
// protocol. Priority matches the Grok route: responses > chat > anthropic >
// gemini, defaulting to responses (spark profiles' default type).
func oneWireAPIForProfile(profile *config.Profile) string {
	apiType := profileOpenAIAPIType(profile)
	switch {
	case config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeResponses):
		return oneWireOpenAIResponses
	case config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeChatCompletions):
		return oneWireOpenAIChat
	case config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeAnthropicMessages):
		return oneWireAnthropic
	case config.SupportsOpenAIAPIType(apiType, config.OpenAIAPITypeGeminiGenerateContent):
		return oneWireGemini
	default:
		return oneWireOpenAIResponses
	}
}

// oneBaseURLForProfile picks the endpoint for the chosen wire protocol. The
// anthropic-messages wire posts to {base}/v1/messages, so use the dedicated
// Anthropic base URL when the profile has one (same rule as the Claude/Grok
// direct routes).
func oneBaseURLForProfile(profile *config.Profile, wire string) string {
	if wire == oneWireAnthropic && profile != nil && strings.TrimSpace(profile.AnthropicBaseURL) != "" {
		return strings.TrimSpace(profile.AnthropicBaseURL)
	}
	return profileBase(profile)
}

func normalizeOneModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, mdl := range models {
		id := normalizeOneModelID(mdl)
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

func normalizeOneModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	// one lists models as "provider:model"; strip the spark provider prefix
	// if a user pastes that form back in.
	for _, sep := range []string{":", "/"} {
		if i := strings.Index(model, sep); i > 0 {
			provider := strings.TrimSpace(model[:i])
			id := strings.TrimSpace(model[i+1:])
			if strings.EqualFold(provider, oneProviderID) && id != "" {
				return id
			}
		}
	}
	return model
}

func logOneLaunchRoute(profile *config.Profile) {
	wire := oneWireAPIForProfile(profile)
	routeLine := fmt.Sprintf(
		"[route] mode=direct integration=one wire=%s upstream=%s api_type=%s",
		wire, oneBaseURLForProfile(profile, wire), profileOpenAIAPIType(profile),
	)
	if path := appendLaunchRouteLog(routeLine); path != "" {
		routeLine = routeLine + " route_log=" + path
	}
	fmt.Fprintln(os.Stderr, routeLine)
}
