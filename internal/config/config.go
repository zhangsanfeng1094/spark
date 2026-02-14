package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const currentVersion = 1

type Profile struct {
	OpenAIBaseURL      string   `json:"openai_base_url"`
	OpenAIAPIKey       string   `json:"openai_api_key"`
	OpenAIOrg          string   `json:"openai_org,omitempty"`
	OpenAIProject      string   `json:"openai_project,omitempty"`
	AnthropicBaseURL   string   `json:"anthropic_base_url,omitempty"`
	AnthropicAuthToken string   `json:"anthropic_auth_token,omitempty"`
	Models             []string `json:"models,omitempty"`
	DefaultModel       string   `json:"default_model,omitempty"`
}

type IntegrationConfig struct {
	Profile string            `json:"profile,omitempty"`
	Aliases map[string]string `json:"aliases,omitempty"`
}

type History struct {
	LastSelection  string   `json:"last_selection,omitempty"`
	LastModelInput string   `json:"last_model_input,omitempty"`
	ModelInputs    []string `json:"model_inputs,omitempty"`
}

type RootConfig struct {
	Version        int                           `json:"version"`
	DefaultProfile string                        `json:"default_profile"`
	Profiles       map[string]*Profile           `json:"profiles"`
	Integrations   map[string]*IntegrationConfig `json:"integrations"`
	History        History                       `json:"history,omitempty"`
}

func defaultConfig() *RootConfig {
	return &RootConfig{
		Version:        currentVersion,
		DefaultProfile: "default",
		Profiles: map[string]*Profile{
			"default": {
				OpenAIBaseURL: "https://api.openai.com/v1",
			},
		},
		Integrations: map[string]*IntegrationConfig{},
	}
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".spark"), nil
}

func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (*RootConfig, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			if migrated, merr := tryMigrateFromOllama(cfg); merr == nil && migrated {
				_ = Save(cfg)
			}
			return cfg, nil
		}
		return nil, err
	}

	var cfg RootConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	normalize(&cfg)
	return &cfg, nil
}

func normalize(cfg *RootConfig) {
	if cfg.Version == 0 {
		cfg.Version = currentVersion
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	if _, ok := cfg.Profiles[cfg.DefaultProfile]; !ok {
		cfg.Profiles[cfg.DefaultProfile] = &Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
	}
	if cfg.Integrations == nil {
		cfg.Integrations = map[string]*IntegrationConfig{}
	}
	for _, ic := range cfg.Integrations {
		if ic == nil {
			continue
		}
		if ic.Profile == "" {
			ic.Profile = cfg.DefaultProfile
		}
	}
}

func Save(cfg *RootConfig) error {
	normalize(cfg)
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeWithBackup(path, data)
}

func (c *RootConfig) Integration(name string) *IntegrationConfig {
	key := strings.ToLower(name)
	if c.Integrations[key] == nil {
		c.Integrations[key] = &IntegrationConfig{Profile: c.DefaultProfile}
	}
	return c.Integrations[key]
}

func (c *RootConfig) ProfileByName(name string) (*Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	p := c.Profiles[name]
	if p == nil {
		return nil, fmt.Errorf("profile not found: %s", name)
	}
	return p, nil
}

func (c *RootConfig) UpsertModelHistory(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	c.History.LastModelInput = model
	out := []string{model}
	for _, m := range c.History.ModelInputs {
		if m != model {
			out = append(out, m)
		}
		if len(out) >= 20 {
			break
		}
	}
	c.History.ModelInputs = out
}

func (c *RootConfig) SetDefaultProfile(name string) error {
	if _, ok := c.Profiles[name]; !ok {
		return errors.New("profile does not exist")
	}
	c.DefaultProfile = name
	return nil
}
