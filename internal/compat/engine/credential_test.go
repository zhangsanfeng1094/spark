package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"spark/internal/auth"
	"spark/internal/config"
)

func TestResolveRequestCredential(t *testing.T) {
	tempDir := t.TempDir()
	store := auth.NewStore(filepath.Join(tempDir, "auth"))

	// 1. Profile with explicit API key takes precedence if no auth provider is set
	p1 := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-explicit-key",
	}
	cred1 := ResolveRequestCredentialWithStore(context.Background(), p1, nil, store)
	if cred1.Value != "sk-explicit-key" || cred1.Source != "profile.api_key" {
		t.Fatalf("unexpected cred1: %+v", cred1)
	}

	// 2. Profile with explicit AuthProvider uses stored auth
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Kind:        auth.KindOAuth,
		AccessToken: "claude-token-123",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	p2 := &config.Profile{
		AnthropicBaseURL: "https://api.anthropic.com",
		APIKey:           "byok-ignored",
		AuthProvider:     auth.ProviderClaude,
	}
	cred2 := ResolveRequestCredentialWithStore(context.Background(), p2, nil, store)
	if cred2.Value != "claude-token-123" || cred2.Source != "oauth.claude" {
		t.Fatalf("unexpected cred2: %+v", cred2)
	}

	// 3. Profile without API key auto-resolves to provider OAuth if available
	p3 := &config.Profile{
		AnthropicBaseURL: "https://api.anthropic.com",
		OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
	}
	cred3 := ResolveRequestCredentialWithStore(context.Background(), p3, nil, store)
	if cred3.Value != "claude-token-123" || cred3.Source != "oauth.claude" {
		t.Fatalf("unexpected cred3: %+v", cred3)
	}

	// 4. Empty profile with no stored auth returns empty cred
	p4 := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
	}
	cred4 := ResolveRequestCredentialWithStore(context.Background(), p4, nil, store)
	if cred4.Value != "" {
		t.Fatalf("expected empty cred4, got %+v", cred4)
	}

	// 5. Command Code: explicit AuthProvider
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderCommandCode,
		Kind:        auth.KindOAuth,
		AccessToken: "user_cc_auth_token_999",
	})
	p5 := &config.Profile{
		OpenAIBaseURL: "https://api.commandcode.ai/alpha",
		AuthProvider: "commandcode",
	}
	cred5 := ResolveRequestCredentialWithStore(context.Background(), p5, nil, store)
	if cred5.Value != "user_cc_auth_token_999" || cred5.Source != "oauth.commandcode" {
		t.Fatalf("unexpected cred5: %+v", cred5)
	}

	// 6. Command Code: auto-fallback by base URL (e.g. commandcode-proxy or commandcode.ai)
	p6 := &config.Profile{
		OpenAIBaseURL: "http://127.0.0.1:3050/v1", // commandcode-proxy endpoint
	}
	cred6 := ResolveRequestCredentialWithStore(context.Background(), p6, nil, store)
	if cred6.Value != "user_cc_auth_token_999" || cred6.Source != "oauth.commandcode" {
		t.Fatalf("unexpected cred6: %+v", cred6)
	}

	// 7. Multi-account AuthRef in Auth mode
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderClaude,
		Account:     "work@corp.com",
		AccessToken: "claude-work-token-xyz",
	})
	p7 := &config.Profile{
		Credential: config.CredentialConfig{
			Mode:    config.CredentialModeAuth,
			AuthRef: "claude:work@corp.com",
			APIKey:  "ignored-api-key",
		},
	}
	cred7 := ResolveRequestCredentialWithStore(context.Background(), p7, nil, store)
	if cred7.Value != "claude-work-token-xyz" || cred7.Source != "oauth.claude:work@corp.com" {
		t.Fatalf("unexpected cred7: %+v", cred7)
	}

	// 8. Mode APIKey ignores any authRef or store
	p8 := &config.Profile{
		Credential: config.CredentialConfig{
			Mode:    config.CredentialModeAPIKey,
			AuthRef: "claude:work@corp.com",
			APIKey:  "my-explicit-key",
		},
	}
	cred8 := ResolveRequestCredentialWithStore(context.Background(), p8, nil, store)
	if cred8.Value != "my-explicit-key" || cred8.Source != "profile.api_key" {
		t.Fatalf("unexpected cred8: %+v", cred8)
	}

	// 9. Mode APIKey with empty API key returns empty even if auth exists
	p9 := &config.Profile{
		Credential: config.CredentialConfig{
			Mode:    config.CredentialModeAPIKey,
			AuthRef: "claude:work@corp.com",
		},
	}
	cred9 := ResolveRequestCredentialWithStore(context.Background(), p9, nil, store)
	if cred9.Value != "" {
		t.Fatalf("expected empty cred9 for empty api_key mode, got %+v", cred9)
	}

	// 10. Codex OAuth: resolves Chatgpt-Account-Id, Originator, and User-Agent headers
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderCodex,
		Kind:        auth.KindOAuth,
		AccessToken: "codex-oauth-token",
		AccountID:   "org-chatgpt-12345",
	})
	p10 := &config.Profile{
		AuthProvider: auth.ProviderCodex,
	}
	cred10 := ResolveRequestCredentialWithStore(context.Background(), p10, nil, store)
	if cred10.Value != "codex-oauth-token" || cred10.Source != "oauth.codex" {
		t.Fatalf("unexpected cred10: %+v", cred10)
	}
	if cred10.ExtraHeaders["Chatgpt-Account-Id"] != "org-chatgpt-12345" {
		t.Fatalf("expected Chatgpt-Account-Id header, got %+v", cred10.ExtraHeaders)
	}
	if cred10.ExtraHeaders["Originator"] != "codex-tui" {
		t.Fatalf("expected Originator header, got %+v", cred10.ExtraHeaders)
	}

	// 11. Grok OAuth: resolves xAI CLI proxy headers
	_ = store.Save(&auth.Auth{
		Provider:    auth.ProviderGrok,
		Kind:        auth.KindDevice,
		AccessToken: "xai-oauth-token",
	})
	p11 := &config.Profile{
		AuthProvider: auth.ProviderGrok,
	}
	cred11 := ResolveRequestCredentialWithStore(context.Background(), p11, nil, store)
	if cred11.Value != "xai-oauth-token" || cred11.Source != "oauth.grok" {
		t.Fatalf("unexpected cred11: %+v", cred11)
	}
	if cred11.ExtraHeaders["X-XAI-Token-Auth"] != "xai-grok-cli" || cred11.ExtraHeaders["x-grok-client-identifier"] != "grok-shell" {
		t.Fatalf("expected Grok CLI proxy headers, got %+v", cred11.ExtraHeaders)
	}
}
