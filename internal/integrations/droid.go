package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"spark/internal/config"
)

type Droid struct{}

func (d *Droid) String() string { return "Droid" }

func (d *Droid) Paths() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".factory", "settings.json")}
}

func (d *Droid) Models() []string { return nil }

func (d *Droid) Edit(profile *config.Profile, models []string) error {
	if _, err := firstModel(models); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".factory", "settings.json")
	if err := ensureDir(path); err != nil {
		return err
	}
	settings := readMap(path)

	custom, _ := settings["customModels"].([]any)
	var keep []any
	for _, raw := range custom {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["apiKey"] != "spark" {
			keep = append(keep, m)
		}
	}
	for i, mdl := range models {
		keep = append([]any{map[string]any{
			"model":           mdl,
			"displayName":     mdl,
			"baseUrl":         profileBase(profile),
			"apiKey":          "spark",
			"provider":        "generic-chat-completion-api",
			"maxOutputTokens": 64000,
			"supportsImages":  false,
			"id":              fmt.Sprintf("spark-%d", i),
			"index":           i,
		}}, keep...)
	}
	settings["customModels"] = keep
	session, _ := settings["sessionDefaultSettings"].(map[string]any)
	if session == nil {
		session = map[string]any{}
	}
	session["model"] = "spark-0"
	settings["sessionDefaultSettings"] = session

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (d *Droid) Run(profile *config.Profile, model string, args []string) error {
	if _, err := exec.LookPath("droid"); err != nil {
		return fmt.Errorf("droid is not installed, install from https://docs.factory.ai/cli/getting-started/quickstart")
	}
	if err := d.Edit(profile, []string{model}); err != nil {
		return err
	}
	return runCmd("droid", args, nil)
}
