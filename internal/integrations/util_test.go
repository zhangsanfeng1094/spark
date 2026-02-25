package integrations

import (
	"testing"

	"spark/internal/config"
)

func TestMergeEnv_OverrideExistingKey(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-old",
		"ANTHROPIC_AUTH_TOKEN=old",
	}
	override := []string{
		"ANTHROPIC_API_KEY=",
		"ANTHROPIC_AUTH_TOKEN=ollama",
	}
	out := mergeEnv(base, override)
	got := map[string]string{}
	for _, kv := range out {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if got["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("expected empty ANTHROPIC_API_KEY, got %q", got["ANTHROPIC_API_KEY"])
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "ollama" {
		t.Fatalf("expected ANTHROPIC_AUTH_TOKEN=ollama, got %q", got["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestProfileOpenAIAPIType(t *testing.T) {
	tests := []struct {
		name string
		in   *config.Profile
		want string
	}{
		{name: "nil profile defaults to auto", in: nil, want: config.OpenAIAPITypeAuto},
		{name: "empty defaults to auto", in: &config.Profile{}, want: config.OpenAIAPITypeAuto},
		{name: "responses stays responses", in: &config.Profile{OpenAIAPIType: "responses"}, want: config.OpenAIAPITypeResponses},
		{name: "auto stays auto", in: &config.Profile{OpenAIAPIType: "auto"}, want: config.OpenAIAPITypeAuto},
		{name: "response alias maps responses", in: &config.Profile{OpenAIAPIType: "response"}, want: config.OpenAIAPITypeResponses},
		{name: "multiple canonicalized", in: &config.Profile{OpenAIAPIType: "chat_completions,responses"}, want: "responses,chat_completions"},
		{name: "unknown falls back to auto", in: &config.Profile{OpenAIAPIType: "foo"}, want: config.OpenAIAPITypeAuto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileOpenAIAPIType(tt.in); got != tt.want {
				t.Fatalf("profileOpenAIAPIType(%+v)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}
