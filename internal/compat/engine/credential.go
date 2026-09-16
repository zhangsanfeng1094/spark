package engine

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/auth"
	"spark/internal/auth/oauth"
	"spark/internal/config"
)

// Credential holds the resolved credential value and metadata for an upstream request.
type Credential struct {
	Scheme       string // "bearer", "x-api-key", "x-goog-api-key"
	Value        string
	Source       string // e.g. "oauth.claude", "profile.api_key"
	ExtraHeaders map[string]string
}

// ResolveRequestCredential determines the upstream authentication credential for a profile.
// It checks explicit profile keys, configured AuthProvider, or stored OAuth/Device credentials.
func ResolveRequestCredential(ctx context.Context, profile *config.Profile, r *http.Request) Credential {
	store, _ := auth.DefaultStore()
	return ResolveRequestCredentialWithStore(ctx, profile, r, store)
}

// ResolveRequestCredentialWithStore determines credentials using a specific auth.Store.
func ResolveRequestCredentialWithStore(ctx context.Context, profile *config.Profile, r *http.Request, store *auth.Store) Credential {
	if profile == nil {
		return Credential{}
	}

	mode := profile.EffectiveCredentialMode()

	switch mode {
	case config.CredentialModeAuth:
		authRef := profile.EffectiveAuthRef()
		if authRef == "" && store != nil {
			lowerBase := strings.ToLower(profile.EffectiveEndpoint() + " " + profile.DefaultModel)
			if strings.Contains(lowerBase, "commandcode") || strings.Contains(lowerBase, "command-code") || strings.Contains(lowerBase, ":3050") {
				authRef = auth.ProviderCommandCode
			} else {
				provider, _, _ := MapProfileToProvider(profile)
				authRef = mapBifrostProviderToAuth(provider)
			}
		}
		if authRef != "" && store != nil {
			if cred, ok := lookupStoredAuth(ctx, store, authRef); ok {
				return cred
			}
		}
		return Credential{}

	case config.CredentialModeAPIKey:
		if key := profile.EffectiveAPIKey(); key != "" {
			return Credential{
				Scheme: defaultSchemeForProfile(profile),
				Value:  key,
				Source: "profile.api_key",
			}
		}
		return Credential{}

	case config.CredentialModeAuto:
		fallthrough
	default:
		// 1. Explicit AuthRef configured on profile
		if authRef := profile.EffectiveAuthRef(); authRef != "" && store != nil {
			if cred, ok := lookupStoredAuth(ctx, store, authRef); ok {
				return cred
			}
		}

		// 2. Explicit API key (BYOK)
		if key := profile.EffectiveAPIKey(); key != "" {
			return Credential{
				Scheme: defaultSchemeForProfile(profile),
				Value:  key,
				Source: "profile.api_key",
			}
		}

		// 3. Auto-fallback: check if the profile's target provider has a stored OAuth session
		if store != nil {
			lowerBase := strings.ToLower(profile.EffectiveEndpoint() + " " + profile.DefaultModel)
			if strings.Contains(lowerBase, "commandcode") || strings.Contains(lowerBase, "command-code") || strings.Contains(lowerBase, ":3050") {
				if cred, ok := lookupStoredAuth(ctx, store, auth.ProviderCommandCode); ok {
					return cred
				}
			}

			provider, _, _ := MapProfileToProvider(profile)
			authProv := mapBifrostProviderToAuth(provider)
			if authProv != "" {
				if cred, ok := lookupStoredAuth(ctx, store, authProv); ok {
					return cred
				}
			}
		}

		return Credential{}
	}
}

func lookupStoredAuth(ctx context.Context, store *auth.Store, authRef string) (Credential, bool) {
	if store == nil || strings.TrimSpace(authRef) == "" {
		return Credential{}, false
	}
	provider, account := auth.ParseAuthRef(authRef)
	spec, err := auth.SpecFor(provider)
	sourceName := "oauth." + provider
	if account != "" && account != "default" {
		sourceName = "oauth." + provider + ":" + account
	}

	if err != nil {
		rec, err := store.GetByRef(authRef)
		if err == nil && rec != nil && rec.AccessToken != "" && !rec.Expired(0) {
			return buildCredentialFromAuth(rec, sourceName), true
		}
		return Credential{}, false
	}

	// For Command Code, token is a user API key (user_...) managed without OAuth refresh
	if spec.Provider == auth.ProviderCommandCode {
		rec, err := store.GetByRef(authRef)
		if err == nil && rec != nil && rec.AccessToken != "" && !rec.Expired(0) {
			return buildCredentialFromAuth(rec, sourceName), true
		}
		return Credential{}, false
	}

	// EnsureFresh automatically refreshes if near expiry and refreshToken exists (for default account)
	if account == "" || account == "default" {
		rec, err := oauth.EnsureFresh(ctx, store, spec, 5*time.Minute)
		if err == nil && rec != nil && rec.AccessToken != "" {
			return buildCredentialFromAuth(rec, sourceName), true
		}
	}

	// Check stored record directly
	rec, err := store.GetByRef(authRef)
	if err == nil && rec != nil && rec.AccessToken != "" && !rec.Expired(0) {
		return buildCredentialFromAuth(rec, sourceName), true
	}

	return Credential{}, false
}

func buildCredentialFromAuth(rec *auth.Auth, sourceName string) Credential {
	if rec == nil {
		return Credential{}
	}
	cred := Credential{
		Scheme:       "bearer",
		Value:        rec.AccessToken,
		Source:       sourceName,
		ExtraHeaders: make(map[string]string),
	}
	if rec.Provider == auth.ProviderCodex {
		accID := rec.AccountID
		if accID == "" && rec.Metadata != nil {
			accID = rec.Metadata["chatgpt_account_id"]
		}
		if accID != "" {
			cred.ExtraHeaders["Chatgpt-Account-Id"] = accID
		}
		cred.ExtraHeaders["Originator"] = "codex-tui"
		cred.ExtraHeaders["User-Agent"] = "codex_cli_rs/0.114.0 (Mac OS 14.2.0; x86_64) vscode/1.111.0"
	}
	if rec.Provider == auth.ProviderGrok {
		cred.ExtraHeaders["X-XAI-Token-Auth"] = "xai-grok-cli"
		cred.ExtraHeaders["x-grok-client-version"] = "0.2.120"
		cred.ExtraHeaders["User-Agent"] = "xai-grok-workspace/0.2.120"
		cred.ExtraHeaders["x-grok-client-identifier"] = "grok-shell"
		cred.ExtraHeaders["x-authenticateresponse"] = "authenticate-response"
	}
	return cred
}

func mapBifrostProviderToAuth(p schemas.ModelProvider) string {
	switch p {
	case schemas.Anthropic:
		return auth.ProviderClaude
	case schemas.OpenAI:
		return auth.ProviderCodex
	case schemas.Gemini:
		return auth.ProviderGemini
	case schemas.XAI:
		return auth.ProviderGrok
	default:
		return ""
	}
}

func defaultSchemeForProfile(profile *config.Profile) string {
	if profile == nil {
		return "bearer"
	}
	proto := profile.EffectiveProtocol()
	if proto == config.ProtocolAnthropic || config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		return "x-api-key"
	}
	if proto == config.ProtocolGemini || config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeGeminiGenerateContent) {
		return "x-goog-api-key"
	}
	return "bearer"
}
