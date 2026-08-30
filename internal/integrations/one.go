package integrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// one resolves ~/.one from $HOME and has no per-launch provider override
	// (custom models.json providers are only selectable via settings.json), so
	// launch in an isolated HOME: mirror the real home via symlinks, rewrite
	// .one/agent provider/selection files for this process only, and drop the
	// temp home when the agent exits. The real ~/.one stays untouched.
	launchHome, err := os.MkdirTemp("", "spark-one-home-*")
	if err != nil {
		return fmt.Errorf("create one launch home: %w", err)
	}
	defer func() { _ = os.RemoveAll(launchHome) }()

	if err := writeOneLaunchHome(launchHome, profile, modelID); err != nil {
		return err
	}

	logOneLaunchRoute(profile)

	cmdArgs := append([]string{}, args...)
	if !cliArgsHasModelFlag(cmdArgs) {
		cmdArgs = append([]string{"--model", modelID}, cmdArgs...)
	}
	env := []string{"HOME=" + launchHome}
	return runCmd(bin, cmdArgs, env)
}

// writeOneLaunchHome builds a launch-time $HOME that:
//   - mirrors the real home (dotfiles, .ssh, .npm, .cache, …) via symlinks so
//     git/ssh and MCP tooling keep working
//   - rebuilds .one/agent with the real assets (mcp.json, sessions, skills, …)
//     except auth artifacts and the provider/selection JSON files
//   - writes a models.json clone carrying the spark provider; the apiKey lives
//     only in this temp copy (never in ~/.one), removed when the agent exits
//   - writes settings.json/preferences.json clones selecting the spark model
func writeOneLaunchHome(home string, profile *config.Profile, modelID string) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := symlinkEntries(userHome, home, func(name string) bool {
		return name == ".one"
	}); err != nil {
		return err
	}
	agentDir := filepath.Join(home, ".one", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	realAgent := filepath.Join(userHome, ".one", "agent")
	if err := symlinkEntries(realAgent, agentDir, shouldSkipOneAgentMirror); err != nil {
		return err
	}
	return writeOneLaunchConfigs(agentDir, realAgent, profile, modelID)
}

// shouldSkipOneAgentMirror keeps auth artifacts and provider/selection state
// out of the BYOK launch home. Without auth.json no OAuth token can be sent
// to the spark base_url (same rule as the Grok launch home).
func shouldSkipOneAgentMirror(name string) bool {
	switch name {
	case "models.json", "settings.json", "preferences.json":
		return true
	}
	return strings.HasPrefix(name, "auth.json")
}

// symlinkEntries mirrors every entry from src into dst as absolute symlinks so
// the launch home stays relocatable and writes reach the real assets.
func symlinkEntries(src, dst string, skip func(name string) bool) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if skip != nil && skip(name) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
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

// writeOneLaunchConfigs clones the real provider catalog and selection state,
// injecting the spark provider (with apiKey — temp copy only) and selecting it
// for this launch.
func writeOneLaunchConfigs(agentDir, realAgent string, profile *config.Profile, modelID string) error {
	root := readMap(filepath.Join(realAgent, "models.json"))
	providers, _ := root["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	providers[oneProviderID] = oneSparkProviderEntry(profile, []string{modelID}, true)
	root["providers"] = providers
	if err := writeJSON(filepath.Join(agentDir, "models.json"), root); err != nil {
		return err
	}

	for _, name := range []string{"settings.json", "preferences.json"} {
		sel := readMap(filepath.Join(realAgent, name))
		sel["provider"] = oneProviderID
		sel["model"] = modelID
		if err := writeJSON(filepath.Join(agentDir, name), sel); err != nil {
			return err
		}
	}
	return nil
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
