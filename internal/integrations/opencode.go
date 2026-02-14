package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"spark/internal/config"
)

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
	if len(models) == 0 {
		return nil
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

	provider, _ := cfg["provider"].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
	}
	entry := map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "Spark",
		"options": map[string]any{
			"baseURL": profileBase(profile),
			"apiKey":  profileKey(profile),
		},
		"models": map[string]any{},
	}
	m := map[string]any{}
	for _, mdl := range models {
		m[mdl] = map[string]any{"name": mdl, "_spark": true}
	}
	entry["models"] = m
	provider["spark"] = entry
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
	recent := []any{}
	for _, mdl := range models {
		recent = append(recent, map[string]any{"providerID": "spark", "modelID": mdl})
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
	if err := o.Edit(profile, []string{model}); err != nil {
		return err
	}
	return runCmd("opencode", args, nil)
}
