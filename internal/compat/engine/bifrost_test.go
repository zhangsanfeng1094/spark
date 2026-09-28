package engine

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestEngineInit(t *testing.T) {
	ctx := context.Background()
	eng, err := New(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to initialize engine: %v", err)
	}
	if eng.Client() == nil {
		t.Fatal("Engine client is nil")
	}
	if eng.Account() == nil {
		t.Fatal("Engine account is nil")
	}

	providers, err := eng.Account().GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders error: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("Expected configured providers, got empty slice")
	}
}

func TestMapProfileToProvider(t *testing.T) {
	tests := []struct {
		name         string
		profile      *config.Profile
		wantProvider schemas.ModelProvider
		wantBase     string
		wantKey      string
	}{
		{
			name: "OpenAI standard",
			profile: &config.Profile{
				OpenAIBaseURL: "https://api.openai.com/v1",
				APIKey:        "sk-test",
			},
			wantProvider: schemas.OpenAI,
			wantBase:     "https://api.openai.com",
			wantKey:      "sk-test",
		},
		{
			name: "DeepSeek API",
			profile: &config.Profile{
				OpenAIBaseURL: "https://api.deepseek.com/v1",
				APIKey:        "sk-deepseek",
			},
			wantProvider: schemas.DeepSeek,
			wantBase:     "https://api.deepseek.com",
			wantKey:      "sk-deepseek",
		},
		{
			name: "Anthropic Messages",
			profile: &config.Profile{
				AnthropicBaseURL: "https://api.anthropic.com/v1",
				OpenAIAPIType:    config.OpenAIAPITypeAnthropicMessages,
				APIKey:           "sk-ant",
			},
			wantProvider: schemas.Anthropic,
			wantBase:     "https://api.anthropic.com",
			wantKey:      "sk-ant",
		},
		{
			name: "Ollama local",
			profile: &config.Profile{
				OpenAIBaseURL: "http://localhost:11434/v1",
			},
			wantProvider: schemas.Ollama,
			wantBase:     "http://localhost:11434",
			wantKey:      "",
		},
		{
			name: "Command Code legacy localhost:3050 default",
			profile: &config.Profile{
				OpenAIBaseURL: "http://localhost:3050/v1",
				AuthProvider:  "commandcode",
				DefaultModel:  "claude-3-7-sonnet",
			},
			wantProvider: schemas.Anthropic,
			wantBase:     "https://api.commandcode.ai/provider",
			wantKey:      "",
		},
		{
			name: "Command Code gpt model",
			profile: &config.Profile{
				OpenAIBaseURL: "https://api.commandcode.ai/provider/v1",
				AuthProvider:  "commandcode",
				DefaultModel:  "gpt-4o",
			},
			wantProvider: schemas.OpenAI,
			wantBase:     "https://api.commandcode.ai/provider",
			wantKey:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, base, key := MapProfileToProvider(tt.profile)
			if provider != tt.wantProvider {
				t.Errorf("Provider = %v, want %v", provider, tt.wantProvider)
			}
			if base != tt.wantBase {
				t.Errorf("BaseURL = %v, want %v", base, tt.wantBase)
			}
			if key != tt.wantKey {
				t.Errorf("Key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestResolveProfileModel(t *testing.T) {
	t.Parallel()
	profile := &config.Profile{
		DefaultModel: "gemini-3.7-flash-high",
		Models: []string{
			"gemini-3.7-flash-high",
			"gemini-3.1-flash-lite",
			"gemini-3.1-pro-low",
			"gpt-5.6-sol",
		},
	}
	tests := []struct {
		name      string
		requested string
		profile   *config.Profile
		preferred string
		want      string
	}{
		{name: "exact alias", requested: "gemini-3.7-flash-high", profile: profile, want: "gemini-3.7-flash-high"},
		{name: "agy strips thinking suffix", requested: "gemini-3.7-flash", profile: profile, want: "gemini-3.7-flash-high"},
		{name: "preview maps to listed lite", requested: "gemini-3.1-flash-lite-preview", profile: profile, want: "gemini-3.1-flash-lite"},
		{name: "preferred wins same family", requested: "gemini-3.7-flash", profile: profile, preferred: "gemini-3.7-flash-high", want: "gemini-3.7-flash-high"},
		{name: "unrelated model kept", requested: "gpt-4o", profile: profile, want: "gpt-4o"},
		{name: "empty uses default", requested: "", profile: profile, want: "gemini-3.7-flash-high"},
		{name: "models prefix stripped", requested: "models/gemini-3.7-flash", profile: profile, want: "gemini-3.7-flash-high"},
		{name: "nil profile keeps requested", requested: "gemini-3.7-flash", want: "gemini-3.7-flash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveProfileModel(tt.requested, tt.profile, tt.preferred)
			if got != tt.want {
				t.Fatalf("ResolveProfileModel(%q) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY", "all_proxy", "ALL_PROXY"} {
		t.Setenv(key, "")
	}
}

func TestSparkAccountEnvProxyConfig(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantNil  bool
		wantType schemas.ProxyType
		wantURL  string
	}{
		{
			name:    "no proxy env vars",
			wantNil: true,
		},
		{
			name:     "HTTPS_PROXY enables EnvProxy",
			env:      map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7890"},
			wantType: schemas.EnvProxy,
		},
		{
			name:     "HTTP_PROXY enables EnvProxy",
			env:      map[string]string{"HTTP_PROXY": "http://127.0.0.1:7890"},
			wantType: schemas.EnvProxy,
		},
		{
			name:     "http_proxy enables EnvProxy",
			env:      map[string]string{"http_proxy": "http://127.0.0.1:7890"},
			wantType: schemas.EnvProxy,
		},
		{
			name:     "ALL_PROXY socks5 maps to Socks5Proxy",
			env:      map[string]string{"ALL_PROXY": "socks5://127.0.0.1:1080"},
			wantType: schemas.Socks5Proxy,
			wantURL:  "socks5://127.0.0.1:1080",
		},
		{
			name:     "all_proxy http maps to HTTPProxy",
			env:      map[string]string{"all_proxy": "http://127.0.0.1:7890"},
			wantType: schemas.HTTPProxy,
			wantURL:  "http://127.0.0.1:7890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearProxyEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			acct := NewSparkAccount()
			cfg, err := acct.GetConfigForProvider(schemas.OpenAI)
			if err != nil {
				t.Fatalf("GetConfigForProvider(OpenAI): %v", err)
			}
			fallbackCfg, err := acct.GetConfigForProvider(schemas.ModelProvider("custom-provider"))
			if err != nil {
				t.Fatalf("GetConfigForProvider(custom-provider): %v", err)
			}

			for label, gotCfg := range map[string]*schemas.ProviderConfig{
				"configured": cfg,
				"fallback":   fallbackCfg,
			} {
				if tt.wantNil {
					if gotCfg.ProxyConfig != nil {
						t.Fatalf("%s: expected nil ProxyConfig, got %+v", label, gotCfg.ProxyConfig)
					}
					continue
				}
				if gotCfg.ProxyConfig == nil {
					t.Fatalf("%s: expected non-nil ProxyConfig", label)
				}
				if gotCfg.ProxyConfig.Type != tt.wantType {
					t.Fatalf("%s: ProxyConfig.Type = %q, want %q", label, gotCfg.ProxyConfig.Type, tt.wantType)
				}
				gotURL := ""
				if gotCfg.ProxyConfig.URL != nil {
					gotURL = gotCfg.ProxyConfig.URL.GetValue()
				}
				if gotURL != tt.wantURL {
					t.Fatalf("%s: ProxyConfig.URL = %q, want %q", label, gotURL, tt.wantURL)
				}
			}
		})
	}
}
