package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spark/internal/config"
)

const openCodeProviderID = "spark"

type OpenCode struct{}

func (o *OpenCode) String() string { return "OpenCode" }

func (o *OpenCode) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".local", "state", "opencode", "model.json"),
	}
}

func (o *OpenCode) Models() []string { return nil }

func (o *OpenCode) Edit(profile *config.Profile, models []string) error {
	normalized := normalizeOpenCodeModels(models)
	if len(normalized) == 0 {
		return fmt.Errorf("no models selected")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := ensureDir(configPath); err != nil {
		return err
	}
	cfg := readMap(configPath)
	cfg["$schema"] = "https://opencode.ai/config.json"
	cfg["model"] = openCodeProviderID + "/" + normalized[0]

	provider, _ := cfg["provider"].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
	}
	modelEntries := map[string]any{}
	for _, mdl := range normalized {
		modelEntries[mdl] = map[string]any{"name": mdl}
	}
	provider[openCodeProviderID] = map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "Spark",
		"options": map[string]any{
			"baseURL": profileBase(profile),
			"apiKey":  profileKey(profile),
		},
		"models": modelEntries,
	}
	cfg["provider"] = provider

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return err
	}

	statePath := filepath.Join(home, ".local", "state", "opencode", "model.json")
	if err := ensureDir(statePath); err != nil {
		return err
	}
	state := readMap(statePath)
	recent := make([]any, 0, len(normalized))
	for _, mdl := range normalized {
		recent = append(recent, map[string]any{"providerID": openCodeProviderID, "modelID": mdl})
	}
	state["recent"] = recent
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, stateData, 0o644)
}

func (o *OpenCode) Run(profile *config.Profile, model string, args []string) error {
	if _, err := exec.LookPath("opencode"); err != nil {
		return fmt.Errorf("opencode is not installed, install from https://opencode.ai")
	}
	modelID := normalizeOpenCodeModelID(model)
	if modelID == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if err := o.Edit(profile, []string{modelID}); err != nil {
		return err
	}
	cmdArgs := append([]string{}, args...)
	if !cliArgsHasModelFlag(cmdArgs) {
		cmdArgs = append([]string{"--model", openCodeProviderID + "/" + modelID}, cmdArgs...)
	}
	return runCmd("opencode", cmdArgs, nil)
}

func normalizeOpenCodeModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, mdl := range models {
		id := normalizeOpenCodeModelID(mdl)
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

func normalizeOpenCodeModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	// Accept provider/model form from OpenCode CLI.
	if i := strings.Index(model, "/"); i >= 0 {
		provider := strings.TrimSpace(model[:i])
		id := strings.TrimSpace(model[i+1:])
		if strings.EqualFold(provider, openCodeProviderID) && id != "" {
			return id
		}
		// Keep non-spark provider/model strings intact for custom catalogs.
		if id != "" {
			return model
		}
	}
	return model
}

func cliArgsHasModelFlag(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-m", arg == "--model":
			return true
		case strings.HasPrefix(arg, "--model="), strings.HasPrefix(arg, "-m="):
			return true
		}
	}
	return false
}
