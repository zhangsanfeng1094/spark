package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"spark/internal/config"
)

type Pi struct{}

func (p *Pi) String() string { return "Pi" }

func (p *Pi) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".pi", "agent", "models.json"),
		filepath.Join(home, ".pi", "agent", "settings.json"),
	}
}

func (p *Pi) Models() []string { return nil }

func (p *Pi) Edit(profile *config.Profile, models []string) error {
	model, err := firstModel(models)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	if err := ensureDir(modelsPath); err != nil {
		return err
	}
	cfg := readMap(modelsPath)
	providers, _ := cfg["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	var entries []any
	for _, mdl := range models {
		entries = append(entries, map[string]any{"id": mdl, "_spark": true, "input": []string{"text"}})
	}
	providers["spark"] = map[string]any{
		"baseUrl": profileBase(profile),
		"api":     "openai-completions",
		"apiKey":  profileKey(profile),
		"models":  entries,
	}
	cfg["providers"] = providers
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(modelsPath, data, 0o644); err != nil {
		return err
	}

	settingsPath := filepath.Join(home, ".pi", "agent", "settings.json")
	if err := ensureDir(settingsPath); err != nil {
		return err
	}
	settings := readMap(settingsPath)
	settings["defaultProvider"] = "spark"
	settings["defaultModel"] = model
	settingData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, settingData, 0o644)
}

func (p *Pi) Run(profile *config.Profile, model string, args []string) error {
	if _, err := exec.LookPath("pi"); err != nil {
		return fmt.Errorf("pi is not installed, install with: npm install -g @mariozechner/pi-coding-agent")
	}
	if err := p.Edit(profile, []string{model}); err != nil {
		return err
	}
	return runCmd("pi", args, nil)
}
