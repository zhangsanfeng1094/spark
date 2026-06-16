package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PromptModeAppend  = "append"
	PromptModeReplace = "replace"

	PromptIntegrationCodex  = "codex"
	PromptIntegrationClaude = "claude"
)

type PromptConfig struct {
	Enabled  *bool                    `json:"enabled,omitempty"`
	Presets  map[string]*PromptPreset `json:"presets,omitempty"`
	Bindings []PromptBinding          `json:"bindings,omitempty"`
}

type PromptPreset struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	File        string `json:"file"`
	Mode        string `json:"mode,omitempty"`
}

type PromptBinding struct {
	Integration string `json:"integration"`
	Model       string `json:"model"`
	Preset      string `json:"preset"`
	Mode        string `json:"mode,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type PromptInjection struct {
	Mode    string
	Path    string
	Content string
}

type PromptValidationSeverity string

const (
	PromptValidationError   PromptValidationSeverity = "error"
	PromptValidationWarning PromptValidationSeverity = "warning"
)

type PromptValidationIssue struct {
	Severity PromptValidationSeverity
	Active   bool
	Message  string
}

func normalizePromptConfig(prompts *PromptConfig) {
	if prompts.Enabled == nil {
		prompts.Enabled = boolPtr(false)
	}
	if prompts.Presets == nil {
		prompts.Presets = map[string]*PromptPreset{}
	}
	normalizedPresets := map[string]*PromptPreset{}
	for key, preset := range prompts.Presets {
		if preset == nil {
			continue
		}
		name := strings.TrimSpace(preset.Name)
		if name == "" {
			name = strings.TrimSpace(key)
		}
		if name == "" {
			continue
		}
		preset.Name = name
		preset.Description = strings.TrimSpace(preset.Description)
		preset.File = strings.TrimSpace(preset.File)
		preset.Mode = normalizePromptModeForStorage(preset.Mode)
		normalizedPresets[name] = preset
	}
	prompts.Presets = normalizedPresets

	bindings := make([]PromptBinding, 0, len(prompts.Bindings))
	for _, binding := range prompts.Bindings {
		binding.Integration = NormalizePromptIntegration(binding.Integration)
		binding.Model = NormalizePromptBindingModel(binding.Model)
		binding.Preset = strings.TrimSpace(binding.Preset)
		binding.Mode = normalizePromptModeForStorage(binding.Mode)
		if binding.Enabled == nil {
			binding.Enabled = boolPtr(true)
		}
		if binding.Integration == "" || binding.Model == "" || binding.Preset == "" {
			continue
		}
		bindings = append(bindings, binding)
	}
	prompts.Bindings = bindings
	migratePromptModes(prompts)
}

func NormalizePromptIntegration(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case PromptIntegrationCodex:
		return PromptIntegrationCodex
	case PromptIntegrationClaude:
		return PromptIntegrationClaude
	default:
		return ""
	}
}

func NormalizePromptMode(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", PromptModeAppend:
		return PromptModeAppend
	case PromptModeReplace:
		return PromptModeReplace
	default:
		return ""
	}
}

func NormalizePromptBindingModel(in string) string {
	if strings.TrimSpace(in) == "*" {
		return "*"
	}
	return NormalizeModel(in)
}

func EffectivePromptMode(binding PromptBinding, preset *PromptPreset) string {
	if strings.TrimSpace(binding.Mode) != "" {
		return NormalizePromptMode(binding.Mode)
	}
	if preset == nil {
		return ""
	}
	return NormalizePromptMode(preset.Mode)
}

func normalizePromptModeForStorage(in string) string {
	trimmed := strings.ToLower(strings.TrimSpace(in))
	if trimmed == "" {
		return ""
	}
	if mode := NormalizePromptMode(trimmed); mode != "" {
		return mode
	}
	return trimmed
}

func migratePromptModes(prompts *PromptConfig) {
	if prompts == nil || len(prompts.Presets) == 0 {
		return
	}
	bindingsByPreset := map[string][]int{}
	for i, binding := range prompts.Bindings {
		bindingsByPreset[binding.Preset] = append(bindingsByPreset[binding.Preset], i)
	}
	for name, preset := range prompts.Presets {
		if preset == nil {
			continue
		}
		rawPresetMode := strings.TrimSpace(preset.Mode)
		presetMode := NormalizePromptMode(rawPresetMode)
		if rawPresetMode == "" {
			presetMode = PromptModeAppend
			modeFromBindings := ""
			conflict := false
			for _, idx := range bindingsByPreset[name] {
				bindingModeRaw := strings.TrimSpace(prompts.Bindings[idx].Mode)
				bindingMode := NormalizePromptMode(bindingModeRaw)
				if bindingModeRaw == "" || bindingMode == "" {
					continue
				}
				if modeFromBindings == "" {
					modeFromBindings = bindingMode
				} else if modeFromBindings != bindingMode {
					conflict = true
				}
			}
			if modeFromBindings != "" && !conflict {
				presetMode = modeFromBindings
			}
		} else if presetMode == "" {
			presetMode = rawPresetMode
		}
		preset.Mode = presetMode
		for _, idx := range bindingsByPreset[name] {
			if NormalizePromptMode(prompts.Bindings[idx].Mode) == presetMode {
				prompts.Bindings[idx].Mode = ""
			}
		}
	}
}

func (c *RootConfig) ResolvePromptBinding(integration, model string) (*PromptBinding, *PromptPreset, error) {
	if c == nil {
		return nil, nil, nil
	}
	integration = NormalizePromptIntegration(integration)
	model = NormalizeModel(model)
	if integration == "" || model == "" {
		return nil, nil, nil
	}
	var wildcard *PromptBinding
	for _, binding := range c.Prompts.Bindings {
		if binding.Integration != integration {
			continue
		}
		if binding.Model == model {
			return c.resolvePromptBinding(binding)
		}
		if binding.Model == "*" && wildcard == nil {
			bindingCopy := binding
			wildcard = &bindingCopy
		}
	}
	if wildcard != nil {
		return c.resolvePromptBinding(*wildcard)
	}
	return nil, nil, nil
}

func (c *RootConfig) resolvePromptBinding(binding PromptBinding) (*PromptBinding, *PromptPreset, error) {
	preset := c.Prompts.Presets[binding.Preset]
	if preset == nil {
		return nil, nil, fmt.Errorf("prompt binding %s/%s references missing preset: %s", binding.Integration, binding.Model, binding.Preset)
	}
	mode := EffectivePromptMode(binding, preset)
	if mode == "" {
		return nil, nil, fmt.Errorf("prompt binding %s/%s has invalid mode", binding.Integration, binding.Model)
	}
	binding.Mode = mode
	return &binding, preset, nil
}

func (c *RootConfig) ResolvePromptInjection(integration, model string) (*PromptInjection, error) {
	if c == nil || !c.Prompts.IsEnabled() {
		return nil, nil
	}
	integration = NormalizePromptIntegration(integration)
	model = NormalizeModel(model)
	if integration == "" || model == "" {
		return nil, nil
	}
	binding, preset, err := c.ResolvePromptBinding(integration, model)
	if err != nil || binding == nil {
		return nil, err
	}
	if !binding.IsEnabled() {
		return nil, nil
	}
	path, content, err := ResolvePromptPresetFile(preset)
	if err != nil {
		return nil, err
	}
	return &PromptInjection{Mode: binding.Mode, Path: path, Content: content}, nil
}

func (p PromptConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return false
	}
	return *p.Enabled
}

func (p *PromptConfig) SetEnabled(enabled bool) {
	if p == nil {
		return
	}
	p.Enabled = boolPtr(enabled)
}

func (b PromptBinding) IsEnabled() bool {
	if b.Enabled == nil {
		return true
	}
	return *b.Enabled
}

func (b *PromptBinding) SetEnabled(enabled bool) {
	if b == nil {
		return
	}
	b.Enabled = boolPtr(enabled)
}

func ResolvePromptPresetFile(preset *PromptPreset) (string, string, error) {
	if preset == nil {
		return "", "", fmt.Errorf("prompt preset is missing")
	}
	path, err := ResolvePromptPath(preset.File)
	if err != nil {
		return "", "", fmt.Errorf("prompt preset %q: %w", preset.Name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("prompt preset %q: read %s: %w", preset.Name, path, err)
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", "", fmt.Errorf("prompt preset %q file is empty: %s", preset.Name, path)
	}
	return path, content, nil
}

func ResolvePromptPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("file path is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configDir, err := configDir()
	if err != nil {
		return "", err
	}
	allowed := "relative paths must stay under " + configDir + "; absolute and ~/ paths must stay under " + home
	if path == "~" || strings.HasPrefix(path, "~/") {
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
		path = filepath.Clean(path)
		if !pathWithin(path, home) {
			return "", fmt.Errorf("file path escapes allowed prompt locations (%s): %s", allowed, path)
		}
	} else if !filepath.IsAbs(path) {
		path = filepath.Clean(filepath.Join(configDir, path))
		if !pathWithin(path, configDir) {
			return "", fmt.Errorf("file path escapes allowed prompt locations (%s): %s", allowed, path)
		}
	} else {
		path = filepath.Clean(path)
		if !pathWithin(path, home) {
			return "", fmt.Errorf("file path escapes allowed prompt locations (%s): %s", allowed, path)
		}
	}
	return path, nil
}

func (c *RootConfig) ValidatePrompts() error {
	for _, issue := range c.CheckPrompts() {
		if issue.Active && issue.Severity == PromptValidationError {
			return fmt.Errorf("%s", issue.Message)
		}
	}
	return nil
}

func (c *RootConfig) ValidatePromptConfigStrict() error {
	for _, issue := range c.CheckPrompts() {
		return fmt.Errorf("%s", issue.Message)
	}
	return nil
}

func (c *RootConfig) CheckPrompts() []PromptValidationIssue {
	if c == nil {
		return nil
	}
	issues := []PromptValidationIssue{}
	globalActive := c.Prompts.IsEnabled()
	seenActive := map[string]bool{}
	for _, binding := range c.Prompts.Bindings {
		active := globalActive && binding.IsEnabled()
		if NormalizePromptIntegration(binding.Integration) == "" {
			issues = append(issues, promptIssue(active, "prompt binding has invalid integration: %s", binding.Integration))
		}
		if NormalizePromptBindingModel(binding.Model) == "" {
			issues = append(issues, promptIssue(active, "prompt binding for %s has empty model", binding.Integration))
		}
		preset := c.Prompts.Presets[strings.TrimSpace(binding.Preset)]
		if preset == nil {
			issues = append(issues, promptIssue(active, "prompt binding %s/%s references missing preset: %s", binding.Integration, binding.Model, binding.Preset))
		} else if EffectivePromptMode(binding, preset) == "" {
			issues = append(issues, promptIssue(active, "prompt binding %s/%s has invalid mode", binding.Integration, binding.Model))
		}
		key := bindingKey(NormalizePromptIntegration(binding.Integration), NormalizePromptBindingModel(binding.Model))
		if priorActive, ok := seenActive[key]; ok {
			issues = append(issues, promptIssue(active || priorActive, "duplicate prompt binding for %s/%s", binding.Integration, binding.Model))
		}
		seenActive[key] = seenActive[key] || active
	}
	for _, name := range c.PromptPresetNames() {
		preset := c.Prompts.Presets[name]
		active := false
		for _, binding := range c.Prompts.Bindings {
			if strings.TrimSpace(binding.Preset) == name && globalActive && binding.IsEnabled() {
				active = true
				break
			}
		}
		if NormalizePromptMode(preset.Mode) == "" {
			issues = append(issues, promptIssue(active, "prompt preset %q has invalid mode: %s", preset.Name, preset.Mode))
		}
		if _, _, err := ResolvePromptPresetFile(preset); err != nil {
			issues = append(issues, promptIssue(active, "%s", err.Error()))
		}
	}
	return issues
}

func (c *RootConfig) PromptPresetNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.Prompts.Presets))
	for name := range c.Prompts.Presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *RootConfig) PromptPresetInUse(name string) bool {
	name = strings.TrimSpace(name)
	if c == nil || name == "" {
		return false
	}
	for _, binding := range c.Prompts.Bindings {
		if strings.TrimSpace(binding.Preset) == name {
			return true
		}
	}
	return false
}

func (c *RootConfig) RemovePromptPreset(name string) error {
	if c == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if c.PromptPresetInUse(name) {
		return fmt.Errorf("prompt preset %q is used by a binding", name)
	}
	delete(c.Prompts.Presets, name)
	return nil
}

func bindingKey(integration, model string) string {
	return integration + "\x00" + model
}

func promptIssue(active bool, format string, args ...interface{}) PromptValidationIssue {
	severity := PromptValidationWarning
	if active {
		severity = PromptValidationError
	}
	return PromptValidationIssue{Severity: severity, Active: active, Message: fmt.Sprintf(format, args...)}
}

func pathWithin(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)
	if path == base {
		return true
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func boolPtr(v bool) *bool {
	return &v
}
