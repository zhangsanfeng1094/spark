package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type legacyRoot struct {
	Integrations map[string]struct {
		Models []string `json:"models"`
	} `json:"integrations"`
}

func tryMigrateFromOllama(cfg *RootConfig) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	path := filepath.Join(home, ".ollama", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var old legacyRoot
	if err := json.Unmarshal(data, &old); err != nil {
		return false, nil
	}
	if len(old.Integrations) == 0 {
		return false, nil
	}
	def := cfg.Profiles[cfg.DefaultProfile]
	if def == nil {
		def = &Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
		cfg.Profiles[cfg.DefaultProfile] = def
	}
	seen := map[string]struct{}{}
	for _, m := range def.Models {
		seen[strings.TrimSpace(m)] = struct{}{}
	}
	changed := false
	for _, ic := range old.Integrations {
		if len(ic.Models) == 0 {
			continue
		}
		for _, model := range ic.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			def.Models = append(def.Models, model)
			seen[model] = struct{}{}
			changed = true
		}
	}
	return changed, nil
}
