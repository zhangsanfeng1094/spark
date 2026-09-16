package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const currentVersion = 1

const (
	OpenAIAPITypeResponses             = "responses"
	OpenAIAPITypeChatCompletions       = "chat_completions"
	OpenAIAPITypeGeminiGenerateContent = "gemini_generate_content"
	OpenAIAPITypeAnthropicMessages     = "anthropic_messages"
	DefaultOpenAIAPIType               = OpenAIAPITypeResponses + "," + OpenAIAPITypeChatCompletions
)

// APIProtocol represents the client-upstream wire protocol.
type APIProtocol string

const (
	ProtocolOpenAIChat      APIProtocol = "openai_chat_completions"
	ProtocolOpenAIResponses APIProtocol = "openai_responses"
	ProtocolAnthropic       APIProtocol = "anthropic_messages"
	ProtocolGemini          APIProtocol = "gemini_generate_content"
)

// CredentialMode defines how upstream authorization credentials are resolved.
type CredentialMode string

const (
	CredentialModeAuto   CredentialMode = "auto"
	CredentialModeAuth   CredentialMode = "auth"
	CredentialModeAPIKey CredentialMode = "api_key"
)

// CredentialConfig holds explicit authentication binding for a profile.
type CredentialConfig struct {
	Mode    CredentialMode `json:"mode,omitempty"`
	AuthRef string         `json:"auth_ref,omitempty"`
	APIKey  string         `json:"api_key,omitempty"`
}

// ThinkingConfig describes Spark's request-level thinking policy for a Profile.
// An omitted policy leaves the client request untouched.
type ThinkingConfig struct {
	Mode         string `json:"mode,omitempty"`          // client | auto | force | off
	Effort       string `json:"effort,omitempty"`        // minimal | low | medium | high | xhigh | max
	BudgetTokens *int   `json:"budget_tokens,omitempty"` // takes precedence over Effort
}

type Profile struct {
	// Runtime fields are set by the launcher only and are never persisted.
	RuntimeName     string          `json:"-"`
	SessionThinking *ThinkingConfig `json:"-"`

	// Canonical v2 fields
	Endpoint   string           `json:"endpoint,omitempty"`
	Protocol   APIProtocol      `json:"protocol,omitempty"`
	Credential CredentialConfig `json:"credential,omitempty"`
	Thinking   *ThinkingConfig  `json:"thinking,omitempty"`

	// Legacy & compatibility fields
	OpenAIBaseURL      string   `json:"openai_base_url,omitempty"`
	APIKey             string   `json:"api_key,omitempty"`
	OpenAIAPIKey       string   `json:"-"`
	OpenAIAPIType      string   `json:"openai_api_type,omitempty"`
	OpenAIOrg          string   `json:"openai_org,omitempty"`
	OpenAIProject      string   `json:"openai_project,omitempty"`
	ModelListURL       string   `json:"model_list_url,omitempty"`
	AnthropicBaseURL   string   `json:"anthropic_base_url,omitempty"`
	AnthropicAuthToken string   `json:"-"`
	Models             []string `json:"models,omitempty"`
	DefaultModel       string   `json:"default_model,omitempty"`
	AuthProvider       string   `json:"auth_provider,omitempty"`
	AuthRef            string   `json:"auth_ref,omitempty"`
}

func (p *Profile) EffectiveEndpoint() string {
	if p == nil {
		return ""
	}
	if ep := strings.TrimSpace(p.Endpoint); ep != "" {
		return ep
	}
	if bu := strings.TrimSpace(p.OpenAIBaseURL); bu != "" {
		return bu
	}
	return strings.TrimSpace(p.AnthropicBaseURL)
}

func (p *Profile) EffectiveProtocol() APIProtocol {
	if p == nil {
		return ProtocolOpenAIChat
	}
	if p.Protocol != "" {
		return NormalizeAPIProtocol(string(p.Protocol))
	}
	if p.OpenAIAPIType != "" {
		return NormalizeAPIProtocol(p.OpenAIAPIType)
	}
	return ProtocolOpenAIChat
}

func (p *Profile) EffectiveAuthRef() string {
	if p == nil {
		return ""
	}
	if ref := strings.TrimSpace(p.Credential.AuthRef); ref != "" {
		if !strings.Contains(ref, ":") {
			return ref + ":default"
		}
		return ref
	}
	if ref := strings.TrimSpace(p.AuthRef); ref != "" {
		if !strings.Contains(ref, ":") {
			return ref + ":default"
		}
		return ref
	}
	if prov := strings.TrimSpace(p.AuthProvider); prov != "" {
		if !strings.Contains(prov, ":") {
			return prov + ":default"
		}
		return prov
	}
	return ""
}

func (p *Profile) EffectiveCredentialMode() CredentialMode {
	if p == nil {
		return CredentialModeAuto
	}
	if p.Credential.Mode != "" {
		return p.Credential.Mode
	}
	if p.EffectiveAuthRef() != "" && p.EffectiveAPIKey() == "" {
		return CredentialModeAuth
	}
	if p.EffectiveAPIKey() != "" && p.EffectiveAuthRef() == "" {
		return CredentialModeAPIKey
	}
	return CredentialModeAuto
}

func (p *Profile) EffectiveAPIKey() string {
	if p == nil {
		return ""
	}
	if k := strings.TrimSpace(p.Credential.APIKey); k != "" {
		return k
	}
	if k := strings.TrimSpace(p.APIKey); k != "" {
		return k
	}
	if k := strings.TrimSpace(p.OpenAIAPIKey); k != "" {
		return k
	}
	return strings.TrimSpace(p.AnthropicAuthToken)
}

func (p *Profile) NormalizeAPIKey() {
	if p == nil {
		return
	}
	key := p.EffectiveAPIKey()
	p.APIKey = key
	p.OpenAIAPIKey = key
	p.Credential.APIKey = key
	p.AnthropicAuthToken = ""
}

func (p *Profile) SetAPIKey(key string) {
	if p == nil {
		return
	}
	key = strings.TrimSpace(key)
	p.APIKey = key
	p.OpenAIAPIKey = key
	p.Credential.APIKey = key
	p.AnthropicAuthToken = ""
}

func (p *Profile) UnmarshalJSON(data []byte) error {
	type profileJSON struct {
		Endpoint           string           `json:"endpoint"`
		Protocol           APIProtocol      `json:"protocol"`
		Credential         CredentialConfig `json:"credential"`
		Thinking           *ThinkingConfig  `json:"thinking,omitempty"`
		OpenAIBaseURL      string           `json:"openai_base_url"`
		APIKey             string           `json:"api_key,omitempty"`
		OpenAIAPIKey       string           `json:"openai_api_key,omitempty"`
		OpenAIAPIType      string           `json:"openai_api_type,omitempty"`
		OpenAIOrg          string           `json:"openai_org,omitempty"`
		OpenAIProject      string           `json:"openai_project,omitempty"`
		ModelListURL       string           `json:"model_list_url,omitempty"`
		AnthropicBaseURL   string           `json:"anthropic_base_url,omitempty"`
		AnthropicAuthToken string           `json:"anthropic_auth_token,omitempty"`
		Models             []string         `json:"models,omitempty"`
		DefaultModel       string           `json:"default_model,omitempty"`
		AuthProvider       string           `json:"auth_provider,omitempty"`
		AuthRef            string           `json:"auth_ref,omitempty"`
	}
	var raw profileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Profile{
		Endpoint:           raw.Endpoint,
		Protocol:           raw.Protocol,
		Credential:         raw.Credential,
		Thinking:           raw.Thinking,
		OpenAIBaseURL:      raw.OpenAIBaseURL,
		APIKey:             raw.APIKey,
		OpenAIAPIKey:       raw.OpenAIAPIKey,
		OpenAIAPIType:      raw.OpenAIAPIType,
		OpenAIOrg:          raw.OpenAIOrg,
		OpenAIProject:      raw.OpenAIProject,
		ModelListURL:       raw.ModelListURL,
		AnthropicBaseURL:   raw.AnthropicBaseURL,
		AnthropicAuthToken: raw.AnthropicAuthToken,
		Models:             raw.Models,
		DefaultModel:       raw.DefaultModel,
		AuthProvider:       raw.AuthProvider,
		AuthRef:            raw.AuthRef,
	}
	if p.Endpoint == "" {
		p.Endpoint = p.EffectiveEndpoint()
	}
	if p.Protocol == "" && p.OpenAIAPIType != "" {
		p.Protocol = p.EffectiveProtocol()
	}
	if p.OpenAIBaseURL == "" {
		p.OpenAIBaseURL = p.EffectiveEndpoint()
	}
	if p.OpenAIAPIType == "" {
		p.OpenAIAPIType = string(p.EffectiveProtocol())
	}
	if p.Credential.AuthRef == "" && p.EffectiveAuthRef() != "" {
		p.Credential.AuthRef = p.EffectiveAuthRef()
	}
	if p.Credential.APIKey == "" && p.EffectiveAPIKey() != "" {
		p.Credential.APIKey = p.EffectiveAPIKey()
	}
	if p.Credential.Mode == "" {
		p.Credential.Mode = p.EffectiveCredentialMode()
	}
	p.NormalizeAPIKey()
	return nil
}

func (p Profile) MarshalJSON() ([]byte, error) {
	type profileJSON struct {
		Endpoint         string           `json:"endpoint,omitempty"`
		Protocol         APIProtocol      `json:"protocol,omitempty"`
		Credential       CredentialConfig `json:"credential,omitempty"`
		Thinking         *ThinkingConfig  `json:"thinking,omitempty"`
		OpenAIBaseURL    string           `json:"openai_base_url,omitempty"`
		APIKey           string           `json:"api_key,omitempty"`
		OpenAIAPIType    string           `json:"openai_api_type,omitempty"`
		OpenAIOrg        string           `json:"openai_org,omitempty"`
		OpenAIProject    string           `json:"openai_project,omitempty"`
		ModelListURL     string           `json:"model_list_url,omitempty"`
		AnthropicBaseURL string           `json:"anthropic_base_url,omitempty"`
		Models           []string         `json:"models,omitempty"`
		DefaultModel     string           `json:"default_model,omitempty"`
		AuthProvider     string           `json:"auth_provider,omitempty"`
		AuthRef          string           `json:"auth_ref,omitempty"`
	}
	cred := p.Credential
	if cred.Mode == "" {
		cred.Mode = p.EffectiveCredentialMode()
	}
	if cred.AuthRef == "" {
		cred.AuthRef = p.EffectiveAuthRef()
	}
	if cred.APIKey == "" {
		cred.APIKey = p.EffectiveAPIKey()
	}
	return json.Marshal(profileJSON{
		Endpoint:         p.EffectiveEndpoint(),
		Protocol:         p.EffectiveProtocol(),
		Credential:       cred,
		Thinking:         p.Thinking,
		OpenAIBaseURL:    p.EffectiveEndpoint(),
		APIKey:           p.EffectiveAPIKey(),
		OpenAIAPIType:    p.OpenAIAPIType,
		OpenAIOrg:        p.OpenAIOrg,
		OpenAIProject:    p.OpenAIProject,
		ModelListURL:     p.ModelListURL,
		AnthropicBaseURL: p.AnthropicBaseURL,
		Models:           p.Models,
		DefaultModel:     p.DefaultModel,
		AuthProvider:     p.AuthProvider,
		AuthRef:          p.EffectiveAuthRef(),
	})
}

func NormalizeAPIProtocol(v string) APIProtocol {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case string(ProtocolOpenAIResponses), "response", "openai-responses", "openai_response":
		return ProtocolOpenAIResponses
	case string(ProtocolOpenAIChat), "chat_completions", "chat-completions", "chat/completions", "openai-completions", "openai_chat":
		return ProtocolOpenAIChat
	case string(ProtocolGemini), "gemini", "generatecontent", "generate_content", "gemini-generate-content":
		return ProtocolGemini
	case string(ProtocolAnthropic), "anthropic", "messages", "anthropic-messages", "anthropic/messages":
		return ProtocolAnthropic
	default:
		return ProtocolOpenAIChat
	}
}

func NormalizeOpenAIAPIType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case OpenAIAPITypeResponses, "response", "openai-responses", "openai_response":
		return OpenAIAPITypeResponses
	case OpenAIAPITypeChatCompletions, "chat-completions", "chat/completions", "openai-completions", "openai_chat_completions":
		return OpenAIAPITypeChatCompletions
	case OpenAIAPITypeGeminiGenerateContent, "gemini", "generatecontent", "generate_content", "gemini-generate-content":
		return OpenAIAPITypeGeminiGenerateContent
	case OpenAIAPITypeAnthropicMessages, "anthropic", "messages", "anthropic-messages", "anthropic/messages":
		return OpenAIAPITypeAnthropicMessages
	default:
		return ""
	}
}

func ParseOpenAIAPITypes(v string) []string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	if legacy := legacyOpenAIAPITypes(trimmed); len(legacy) > 0 {
		return legacy
	}
	if normalized := NormalizeOpenAIAPIType(trimmed); normalized != "" {
		return []string{normalized}
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		switch r {
		case ',', ';', '|', '+', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, part := range parts {
		if legacy := legacyOpenAIAPITypes(part); len(legacy) > 0 {
			for _, apiType := range legacy {
				seen[apiType] = struct{}{}
			}
			continue
		}
		normalized := NormalizeOpenAIAPIType(part)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
	}

	canonical := make([]string, 0, 2)
	if _, ok := seen[OpenAIAPITypeResponses]; ok {
		canonical = append(canonical, OpenAIAPITypeResponses)
	}
	if _, ok := seen[OpenAIAPITypeChatCompletions]; ok {
		canonical = append(canonical, OpenAIAPITypeChatCompletions)
	}
	if _, ok := seen[OpenAIAPITypeGeminiGenerateContent]; ok {
		canonical = append(canonical, OpenAIAPITypeGeminiGenerateContent)
	}
	if _, ok := seen[OpenAIAPITypeAnthropicMessages]; ok {
		canonical = append(canonical, OpenAIAPITypeAnthropicMessages)
	}
	if len(canonical) > 0 {
		return canonical
	}
	return nil
}

func legacyOpenAIAPITypes(v string) []string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "auto", "prefer_responses", "responses_or_chat_completions":
		return []string{OpenAIAPITypeResponses, OpenAIAPITypeChatCompletions}
	default:
		return nil
	}
}

func CanonicalizeOpenAIAPITypes(v string) string {
	parsed := ParseOpenAIAPITypes(v)
	if len(parsed) == 0 {
		return ""
	}
	if len(parsed) == 1 {
		return parsed[0]
	}
	return strings.Join(parsed, ",")
}

func SupportsOpenAIAPIType(v, target string) bool {
	if target == "" {
		return false
	}
	for _, apiType := range ParseOpenAIAPITypes(v) {
		if apiType == target {
			return true
		}
	}
	return false
}

type IntegrationConfig struct {
	Profile          string            `json:"profile,omitempty"`
	Aliases          map[string]string `json:"aliases,omitempty"`
	ModelCatalogJSON string            `json:"model_catalog_json,omitempty"`
}

type History struct {
	LastSelection  string   `json:"last_selection,omitempty"`
	LastProfile    string   `json:"last_profile,omitempty"`
	LastModel      string   `json:"last_model,omitempty"`
	LastModelInput string   `json:"last_model_input,omitempty"`
	ModelInputs    []string `json:"model_inputs,omitempty"`
}

type RootConfig struct {
	Version            int                           `json:"version"`
	DefaultProfile     string                        `json:"default_profile"`
	DefaultIntegration string                        `json:"default_integration,omitempty"`
	Profiles           map[string]*Profile           `json:"profiles"`
	Integrations       map[string]*IntegrationConfig `json:"integrations"`
	History            History                       `json:"history,omitempty"`
	McpServers         map[string]*McpServerConfig   `json:"mcp_servers,omitempty"`
	Prompts            PromptConfig                  `json:"prompts,omitempty"`
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
		McpServers:   map[string]*McpServerConfig{},
		Prompts: PromptConfig{
			Enabled:  boolPtr(false),
			Presets:  map[string]*PromptPreset{},
			Bindings: []PromptBinding{},
		},
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
	Normalize(&cfg)
	return &cfg, nil
}

func Normalize(cfg *RootConfig) {
	if cfg.Version == 0 {
		cfg.Version = currentVersion
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	cfg.DefaultIntegration = strings.ToLower(strings.TrimSpace(cfg.DefaultIntegration))
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	if _, ok := cfg.Profiles[cfg.DefaultProfile]; !ok {
		cfg.Profiles[cfg.DefaultProfile] = &Profile{OpenAIBaseURL: "https://api.openai.com/v1"}
	}
	for _, profile := range cfg.Profiles {
		profile.NormalizeAPIKey()
	}
	if cfg.Integrations == nil {
		cfg.Integrations = map[string]*IntegrationConfig{}
	}
	if cfg.McpServers == nil {
		cfg.McpServers = map[string]*McpServerConfig{}
	}
	normalizePromptConfig(&cfg.Prompts)
	for _, ic := range cfg.Integrations {
		if ic == nil {
			continue
		}
		if ic.Profile == "" {
			ic.Profile = cfg.DefaultProfile
		}
		ic.ModelCatalogJSON = strings.TrimSpace(ic.ModelCatalogJSON)
	}
	for _, p := range cfg.Profiles {
		if p == nil {
			continue
		}
		if p.Endpoint == "" {
			p.Endpoint = p.EffectiveEndpoint()
		}
		if p.Protocol == "" {
			p.Protocol = p.EffectiveProtocol()
		}
		if p.OpenAIBaseURL == "" {
			p.OpenAIBaseURL = p.EffectiveEndpoint()
		}
		if p.OpenAIAPIType == "" {
			p.OpenAIAPIType = string(p.EffectiveProtocol())
		}
		if p.Credential.AuthRef == "" && p.EffectiveAuthRef() != "" {
			p.Credential.AuthRef = p.EffectiveAuthRef()
		}
		if p.Credential.APIKey == "" && p.EffectiveAPIKey() != "" {
			p.Credential.APIKey = p.EffectiveAPIKey()
		}
		if p.Credential.Mode == "" {
			p.Credential.Mode = p.EffectiveCredentialMode()
		}
		p.NormalizeAPIKey()
		p.Models = NormalizeModels(p.Models)
		p.DefaultModel = NormalizeModel(p.DefaultModel)
	}
	if len(cfg.Profiles) > 0 {
		if cfg.DefaultProfile == "" || cfg.Profiles[cfg.DefaultProfile] == nil {
			if _, ok := cfg.Profiles["default"]; ok {
				cfg.DefaultProfile = "default"
			} else {
				var names []string
				for n := range cfg.Profiles {
					names = append(names, n)
				}
				sort.Strings(names)
				cfg.DefaultProfile = names[0]
			}
		}
	}
	if cfg.History.LastProfile != "" && cfg.Profiles[cfg.History.LastProfile] == nil {
		cfg.History.LastProfile = cfg.DefaultProfile
	}
	cfg.History.LastModelInput = NormalizeModel(cfg.History.LastModelInput)
	cfg.History.LastProfile = strings.TrimSpace(cfg.History.LastProfile)
	cfg.History.LastModel = NormalizeModel(cfg.History.LastModel)
	cfg.History.ModelInputs = NormalizeModels(cfg.History.ModelInputs)
}

func NormalizeModels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, m := range in {
		m = NormalizeModel(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func EffectiveProfileModels(profile *Profile) []string {
	if profile == nil {
		return nil
	}
	models := NormalizeModels(profile.Models)
	defaultModel := NormalizeModel(profile.DefaultModel)
	if defaultModel == "" {
		return models
	}
	ordered := make([]string, 0, len(models)+1)
	ordered = append(ordered, defaultModel)
	for _, model := range models {
		if model != defaultModel {
			ordered = append(ordered, model)
		}
	}
	return ordered
}

func NormalizeModel(in string) string {
	return strings.TrimSpace(strings.ReplaceAll(in, "\x00", ""))
}

func ParseModelsCSV(csv string) []string {
	return NormalizeModels(strings.Split(csv, ","))
}

func Save(cfg *RootConfig) error {
	Normalize(cfg)
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
	model = NormalizeModel(model)
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

func (c *RootConfig) RecordLaunch(integration, profile, model string) {
	c.History.LastSelection = strings.ToLower(strings.TrimSpace(integration))
	c.History.LastProfile = strings.TrimSpace(profile)
	c.History.LastModel = NormalizeModel(model)
	c.UpsertModelHistory(model)
}

func (c *RootConfig) SetDefaultProfile(name string) error {
	if _, ok := c.Profiles[name]; !ok {
		return errors.New("profile does not exist")
	}
	c.DefaultProfile = name
	return nil
}

// EnsureProfileForAuthProvider ensures that a Profile matching the auth provider exists.
// Returns the profile name, whether it was newly created, and any error.
func (c *RootConfig) EnsureProfileForAuthProvider(provider string) (string, bool) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return "", false
	}
	if c.Profiles == nil {
		c.Profiles = make(map[string]*Profile)
	}

	// 1. Check if an existing profile already binds to this auth provider
	for name, p := range c.Profiles {
		if strings.EqualFold(p.AuthProvider, provider) || strings.EqualFold(p.EffectiveAuthRef(), provider+":default") || strings.HasPrefix(strings.ToLower(p.EffectiveAuthRef()), provider+":") {
			return name, false
		}
		if provider == "commandcode" && (strings.Contains(p.EffectiveEndpoint(), ":3050") || strings.Contains(strings.ToLower(p.EffectiveEndpoint()), "commandcode")) {
			return name, false
		}
	}

	// 2. Determine default profile name
	profileName := provider
	if provider == "commandcode" {
		profileName = "command-code"
	}
	if _, exists := c.Profiles[profileName]; exists {
		base := profileName
		candidate := base + "-auth"
		idx := 1
		for {
			if _, exists := c.Profiles[candidate]; !exists {
				profileName = candidate
				break
			}
			idx++
			candidate = fmt.Sprintf("%s-auth-%d", base, idx)
		}
	}

	// 3. Create profile template
	switch provider {
	case "commandcode":
		c.Profiles[profileName] = &Profile{
			Endpoint:      "https://api.commandcode.ai/provider/v1",
			Protocol:      ProtocolOpenAIChat,
			Credential:    CredentialConfig{Mode: CredentialModeAuth, AuthRef: "commandcode:default"},
			OpenAIBaseURL: "https://api.commandcode.ai/provider/v1",
			AuthProvider:  "commandcode",
			AuthRef:       "commandcode:default",
			OpenAIAPIType: OpenAIAPITypeChatCompletions,
			ModelListURL:  "https://api.commandcode.ai/provider/v1/models",
			Models:        []string{"claude-sonnet-4-6", "claude-3-7-sonnet", "gpt-4o"},
			DefaultModel:  "claude-sonnet-4-6",
		}
	case "claude":
		c.Profiles[profileName] = &Profile{
			Endpoint:         "https://api.anthropic.com",
			Protocol:         ProtocolAnthropic,
			Credential:       CredentialConfig{Mode: CredentialModeAuth, AuthRef: "claude:default"},
			OpenAIBaseURL:    "https://api.anthropic.com",
			AnthropicBaseURL: "https://api.anthropic.com",
			AuthProvider:     "claude",
			AuthRef:          "claude:default",
			OpenAIAPIType:    OpenAIAPITypeAnthropicMessages,
			Models:           []string{"claude-sonnet-4-20250514"},
			DefaultModel:     "claude-sonnet-4-20250514",
		}
	case "gemini":
		c.Profiles[profileName] = &Profile{
			Endpoint:      "https://generativelanguage.googleapis.com/v1beta",
			Protocol:      ProtocolGemini,
			Credential:    CredentialConfig{Mode: CredentialModeAuth, AuthRef: "gemini:default"},
			OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
			AuthProvider:  "gemini",
			AuthRef:       "gemini:default",
			OpenAIAPIType: OpenAIAPITypeGeminiGenerateContent,
			Models:        []string{"gemini-2.5-flash"},
			DefaultModel:  "gemini-2.5-flash",
		}
	default:
		c.Profiles[profileName] = &Profile{
			Endpoint:      "https://api.openai.com/v1",
			Protocol:      ProtocolOpenAIChat,
			Credential:    CredentialConfig{Mode: CredentialModeAuth, AuthRef: provider + ":default"},
			OpenAIBaseURL: "https://api.openai.com/v1",
			AuthProvider:  provider,
			AuthRef:       provider + ":default",
			OpenAIAPIType: DefaultOpenAIAPIType,
			Models:        []string{"gpt-4.1"},
			DefaultModel:  "gpt-4.1",
		}
	}

	return profileName, true
}
