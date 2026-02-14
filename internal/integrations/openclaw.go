package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"spark/internal/config"
)

type Openclaw struct{}

func (o *Openclaw) String() string { return "OpenClaw" }

func (o *Openclaw) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".openclaw", "openclaw.json")}
}

func (o *Openclaw) Models() []string { return nil }

func (o *Openclaw) Edit(profile *config.Profile, models []string) error {
	model, err := firstModel(models)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".openclaw", "openclaw.json")
	if err := ensureDir(path); err != nil {
		return err
	}
	cfg := readMap(path)
	modelsSection, _ := cfg["models"].(map[string]any)
	if modelsSection == nil {
		modelsSection = map[string]any{}
	}
	providers, _ := modelsSection["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	var list []any
	for _, mdl := range models {
		list = append(list, map[string]any{"id": mdl, "name": mdl})
	}
	providers["agentlaunch"] = map[string]any{
		"baseUrl": profileBase(profile),
		"apiKey":  profileKey(profile),
		"api":     "openai-completions",
		"models":  list,
	}
	modelsSection["providers"] = providers
	cfg["models"] = modelsSection

	agents, _ := cfg["agents"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
	}
	defaults, _ := agents["defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	defaults["model"] = map[string]any{"primary": "agentlaunch/" + model}
	agents["defaults"] = defaults
	cfg["agents"] = agents

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (o *Openclaw) Run(profile *config.Profile, model string, args []string) error {
	bin := "openclaw"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "clawdbot"
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("openclaw is not installed, install from https://docs.openclaw.ai")
		}
	}
	if err := o.Edit(profile, []string{model}); err != nil {
		return err
	}
	return runCmd(bin, append([]string{"gateway"}, args...), nil)
}
