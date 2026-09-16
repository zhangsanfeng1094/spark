package auth

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ProviderSpec describes the OAuth/device endpoints and identifiers for an
// upstream provider aligned with CPA (CLIProxyAPI) reference implementation.
type ProviderSpec struct {
	// Provider key (matches Store keys and Kind flows).
	Provider string
	// Label shown in CLI/TUI prompts.
	Label string
	// SupportsDeviceFlow enables the Device Authorization Grant (RFC 8628)
	// path in addition to browser OAuth.
	SupportsDeviceFlow bool
	// DefaultCallbackPort is the loopback port the local callback server
	// binds during the browser OAuth flow.
	DefaultCallbackPort int
	// CallbackPath is the endpoint path on the local callback server.
	CallbackPath string

	// Browser OAuth (Authorization Code + PKCE) endpoints.
	AuthEndpoint  string
	TokenEndpoint string
	// ClientID used for the public (PKCE) client.
	ClientID string
	// ClientSecret for providers that require a confidential client secret (e.g. Google Antigravity).
	ClientSecret string
	// Scope is the space-separated scope string requested at authorization.
	Scope string

	// Device Flow endpoints.
	DeviceAuthorizationEndpoint string
	DeviceTokenEndpoint         string
	DeviceVerificationEndpoint  string
}

// EffectiveCallbackPath returns the path the callback server listens on (defaults to /callback).
func (s *ProviderSpec) EffectiveCallbackPath() string {
	if strings.TrimSpace(s.CallbackPath) != "" {
		if !strings.HasPrefix(s.CallbackPath, "/") {
			return "/" + s.CallbackPath
		}
		return s.CallbackPath
	}
	return "/callback"
}

// BuildAuthURL constructs the authorization URL for this provider.
func (s *ProviderSpec) BuildAuthURL(redirectURI, state, codeChallenge string) string {
	if s.Provider == ProviderCommandCode {
		return fmt.Sprintf("%s?callback=%s&state=%s",
			s.AuthEndpoint,
			url.QueryEscape(redirectURI),
			url.QueryEscape(state),
		)
	}

	clientID := s.ClientID
	if clientID == "" {
		clientID = s.Provider
	}

	params := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
	}
	if state == "" {
		state = GenerateRandomState()
	}
	if state != "" {
		params.Set("state", state)
	}
	if codeChallenge == "" && (s.Provider == ProviderCodex || s.Provider == ProviderClaude || s.Provider == ProviderGrok) {
		codeChallenge = PKCEChallenge(GenerateCodeVerifier())
	}
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}
	if s.Scope != "" {
		params.Set("scope", s.Scope)
	}
	if s.Provider == ProviderCodex {
		params.Set("prompt", "login")
		params.Set("id_token_add_organizations", "true")
		params.Set("codex_cli_simplified_flow", "true")
	}
	return fmt.Sprintf("%s?%s", s.AuthEndpoint, params.Encode())
}

// Specs lists the provider specs supported by the auth subsystem aligned with CLIProxyAPI.
var Specs = []ProviderSpec{
	{
		Provider:                  ProviderClaude,
		Label:                     "Claude Code",
		SupportsDeviceFlow:        true,
		DefaultCallbackPort:       54545,
		CallbackPath:              "/callback",
		AuthEndpoint:              "https://claude.ai/oauth/authorize",
		TokenEndpoint:             "https://platform.claude.com/v1/oauth/token",
		ClientID:                  "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		DeviceAuthorizationEndpoint: "https://claude.ai/oauth/device/code",
		DeviceTokenEndpoint:       "https://platform.claude.com/v1/oauth/token",
		DeviceVerificationEndpoint:  "https://claude.ai/oauth/device",
		Scope:                     "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload",
	},
	{
		Provider:                    ProviderCodex,
		Label:                       "Codex",
		SupportsDeviceFlow:          true,
		DefaultCallbackPort:         1455,
		CallbackPath:                "/auth/callback",
		AuthEndpoint:                "https://auth.openai.com/oauth/authorize",
		TokenEndpoint:               "https://auth.openai.com/oauth/token",
		ClientID:                    "app_EMoamEEZ73f0CkXaXp7hrann",
		DeviceAuthorizationEndpoint: "https://auth.openai.com/api/accounts/deviceauth/usercode",
		DeviceTokenEndpoint:         "https://auth.openai.com/api/accounts/deviceauth/token",
		DeviceVerificationEndpoint:  "https://auth.openai.com/codex/device",
		Scope:                       "openid email profile offline_access",
	},
	{
		Provider:                  ProviderGemini,
		Label:                     "Gemini / Google AI",
		SupportsDeviceFlow:        true,
		DefaultCallbackPort:       51121,
		CallbackPath:              "/oauth-callback",
		AuthEndpoint:              "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint:             "https://oauth2.googleapis.com/token",
		ClientID:                  os.Getenv("GEMINI_CLIENT_ID"),
		ClientSecret:              os.Getenv("GEMINI_CLIENT_SECRET"),
		DeviceAuthorizationEndpoint: "https://oauth2.googleapis.com/device/code",
		DeviceTokenEndpoint:       "https://oauth2.googleapis.com/token",
		DeviceVerificationEndpoint:  "https://www.google.com/device",
		Scope:                     "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs",
	},
	{
		Provider:                    ProviderGrok,
		Label:                       "Grok / xAI",
		SupportsDeviceFlow:          true,
		DefaultCallbackPort:         54545,
		CallbackPath:                "/callback",
		AuthEndpoint:                "https://auth.x.ai/oauth2/authorize",
		TokenEndpoint:               "https://auth.x.ai/oauth2/token",
		ClientID:                    "b1a00492-073a-47ea-816f-4c329264a828",
		DeviceAuthorizationEndpoint: "https://auth.x.ai/oauth2/device/code",
		DeviceTokenEndpoint:         "https://auth.x.ai/oauth2/token",
		DeviceVerificationEndpoint:  "https://accounts.x.ai/oauth2/device",
		Scope:                       "openid profile email offline_access grok-cli:access api:access",
	},
	{
		Provider:                    ProviderKimi,
		Label:                       "Kimi (Moonshot AI)",
		SupportsDeviceFlow:          true,
		ClientID:                    "17e5f671-d194-4dfb-9706-5516cb48c098",
		DeviceAuthorizationEndpoint: "https://auth.kimi.com/api/oauth/device_authorization",
		DeviceTokenEndpoint:         "https://auth.kimi.com/api/oauth/token",
	},
	{
		Provider:            ProviderCommandCode,
		Label:               "Command Code",
		SupportsDeviceFlow:  false,
		DefaultCallbackPort: 5959,
		CallbackPath:        "/callback",
		AuthEndpoint:        "https://commandcode.ai/studio/auth/cli",
	},
}

// SpecFor returns the ProviderSpec for the given provider key.
func SpecFor(provider string) (ProviderSpec, error) {
	norm := strings.ToLower(strings.TrimSpace(provider))
	switch norm {
	case "anthropic":
		norm = ProviderClaude
	case "openai":
		norm = ProviderCodex
	case "google", "antigravity":
		norm = ProviderGemini
	case "xai":
		norm = ProviderGrok
	case "commandcode", "command-code", "command_code", "cmd", "cmdc", "commandcode-proxy":
		norm = ProviderCommandCode
	}

	clean := strings.ReplaceAll(strings.ReplaceAll(norm, "-", ""), "_", "")
	for _, s := range Specs {
		specClean := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(s.Provider), "-", ""), "_", "")
		if specClean == clean || strings.ToLower(s.Provider) == norm {
			if s.Provider == ProviderGemini {
				if id := os.Getenv("GEMINI_CLIENT_ID"); id != "" {
					s.ClientID = id
				}
				if secret := os.Getenv("GEMINI_CLIENT_SECRET"); secret != "" {
					s.ClientSecret = secret
				}
			}
			return s, nil
		}
	}
	return ProviderSpec{}, fmt.Errorf("auth: unknown provider %q (known: %s)", provider, knownProviders())
}

func knownProviders() string {
	out := ""
	for i, s := range Specs {
		if i > 0 {
			out += ", "
		}
		out += s.Provider
	}
	return out
}

// AuthCatalog defines the default protocol, endpoints, and suggested models
// for an auth provider, decoupling auth capabilities from profile configuration.
type AuthCatalog struct {
	Provider        string   `json:"provider"`
	DefaultProtocol string   `json:"default_protocol"`
	DefaultEndpoint string   `json:"default_endpoint"`
	DefaultModel    string   `json:"default_model"`
	SuggestedModels []string `json:"suggested_models"`
	Description     string   `json:"description,omitempty"`
}

// Catalogs maps providers to their recommended defaults and suggested models.
var Catalogs = map[string]AuthCatalog{
	ProviderClaude: {
		Provider:        ProviderClaude,
		DefaultProtocol: "anthropic_messages",
		DefaultEndpoint: "https://api.anthropic.com",
		DefaultModel:    "claude-3-7-sonnet-20250219",
		SuggestedModels: []string{
			"claude-3-7-sonnet-20250219",
			"claude-3-5-sonnet-20241022",
			"claude-3-5-haiku-20241022",
		},
		Description: "Anthropic Claude via OAuth session",
	},
	ProviderCodex: {
		Provider:        ProviderCodex,
		DefaultProtocol: "openai_responses",
		DefaultEndpoint: "https://chatgpt.com/backend-api/codex",
		DefaultModel:    "gpt-4o",
		SuggestedModels: []string{
			"gpt-4o",
			"gpt-4o-mini",
			"o3-mini",
			"o1",
		},
		Description: "OpenAI Codex via OAuth session",
	},
	ProviderGemini: {
		Provider:        ProviderGemini,
		DefaultProtocol: "gemini_generate_content",
		DefaultEndpoint: "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel:    "gemini-2.5-pro",
		SuggestedModels: []string{
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gemini-2.0-flash",
		},
		Description: "Google Gemini AI via OAuth session",
	},
	ProviderGrok: {
		Provider:        ProviderGrok,
		DefaultProtocol: "openai_chat_completions",
		DefaultEndpoint: "https://cli-chat-proxy.grok.com/v1",
		DefaultModel:    "grok-2-latest",
		SuggestedModels: []string{
			"grok-2-latest",
			"grok-2-vision-latest",
			"grok-beta",
		},
		Description: "xAI Grok via OAuth session",
	},
	ProviderKimi: {
		Provider:        ProviderKimi,
		DefaultProtocol: "openai_chat_completions",
		DefaultEndpoint: "https://api.moonshot.cn/v1",
		DefaultModel:    "moonshot-v1-auto",
		SuggestedModels: []string{
			"moonshot-v1-auto",
			"moonshot-v1-8k",
			"moonshot-v1-32k",
			"moonshot-v1-128k",
		},
		Description: "Moonshot Kimi via Device OAuth",
	},
	ProviderCommandCode: {
		Provider:        ProviderCommandCode,
		DefaultProtocol: "openai_chat_completions",
		DefaultEndpoint: "https://api.commandcode.ai/provider/v1",
		DefaultModel:    "claude-3-7-sonnet",
		SuggestedModels: []string{
			"claude-3-7-sonnet",
			"claude-3-5-sonnet",
			"gpt-4o",
			"gemini-2.5-pro",
		},
		Description: "Command Code AI Gateway",
	},
}

// CatalogFor returns the AuthCatalog for a provider, or false if not registered.
func CatalogFor(provider string) (AuthCatalog, bool) {
	spec, err := SpecFor(provider)
	if err == nil {
		if cat, ok := Catalogs[spec.Provider]; ok {
			return cat, true
		}
	}
	cat, ok := Catalogs[strings.ToLower(strings.TrimSpace(provider))]
	return cat, ok
}
