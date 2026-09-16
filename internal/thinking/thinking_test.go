package thinking

import (
	"testing"

	"spark/internal/config"
)

func ptr(v int) *int { return &v }

func TestParseShorthandAndModelSuffix(t *testing.T) {
	for _, tt := range []struct {
		in, model string
		budget    int
		effort    string
	}{
		{"high", "", 0, "high"}, {"8k", "", 8192, ""}, {"8192", "", 8192, ""},
	} {
		got, ok := ParseShorthand(tt.in)
		if !ok {
			t.Fatalf("ParseShorthand(%q) not recognized", tt.in)
		}
		if got.Effort != tt.effort {
			t.Fatalf("effort = %q, want %q", got.Effort, tt.effort)
		}
		if tt.budget > 0 && (got.BudgetTokens == nil || *got.BudgetTokens != tt.budget) {
			t.Fatalf("budget = %v, want %d", got.BudgetTokens, tt.budget)
		}
	}
	model, policy := SplitModelSuffix("qwen3:8b")
	if model != "qwen3:8b" || policy != nil {
		t.Fatalf("ordinary model suffix changed: %q %#v", model, policy)
	}
	model, policy = SplitModelSuffix("claude-opus:high")
	if model != "claude-opus" || policy == nil || policy.Effort != "high" {
		t.Fatalf("suffix = %q %#v", model, policy)
	}
}

func TestResolveProfileOffWins(t *testing.T) {
	got := Resolve(&config.ThinkingConfig{Mode: ModeOff}, &config.ThinkingConfig{Mode: ModeForce, Effort: "high"})
	if got == nil || got.Mode != ModeOff {
		t.Fatalf("profile off must win: %#v", got)
	}
}

func TestApplyAnthropicBudget(t *testing.T) {
	raw := map[string]any{"max_tokens": 1000, "temperature": .7, "top_k": 2}
	Apply(raw, config.ProtocolAnthropic, &config.ThinkingConfig{Mode: ModeForce, BudgetTokens: ptr(2048)})
	thinking := raw["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 2048 {
		t.Fatalf("thinking = %#v", thinking)
	}
	if raw["max_tokens"] != 2049 {
		t.Fatalf("max_tokens = %#v", raw["max_tokens"])
	}
	if _, ok := raw["temperature"]; ok {
		t.Fatal("temperature must be removed")
	}
	if _, ok := raw["top_k"]; ok {
		t.Fatal("top_k must be removed")
	}
}

func TestApplyWireShapes(t *testing.T) {
	tests := []struct {
		name     string
		protocol config.APIProtocol
		raw      map[string]any
		policy   *config.ThinkingConfig
		check    func(*testing.T, map[string]any)
	}{
		{"responses", config.ProtocolOpenAIResponses, map[string]any{}, &config.ThinkingConfig{Mode: ModeForce, Effort: "high"}, func(t *testing.T, raw map[string]any) {
			if raw["reasoning"].(map[string]any)["effort"] != "high" {
				t.Fatal(raw)
			}
		}},
		{"chat off", config.ProtocolOpenAIChat, map[string]any{"reasoning_effort": "high"}, &config.ThinkingConfig{Mode: ModeOff}, func(t *testing.T, raw map[string]any) {
			if _, ok := raw["reasoning_effort"]; ok {
				t.Fatal(raw)
			}
		}},
		{"gemini", config.ProtocolGemini, map[string]any{"generationConfig": map[string]any{"thinkingLevel": "low"}}, &config.ThinkingConfig{Mode: ModeForce, Effort: "high"}, func(t *testing.T, raw map[string]any) {
			if raw["generationConfig"].(map[string]any)["thinkingLevel"] != "high" {
				t.Fatal(raw)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { Apply(tt.raw, tt.protocol, tt.policy); tt.check(t, tt.raw) })
	}
}

func TestAutoDoesNotReplaceClientChoice(t *testing.T) {
	raw := map[string]any{"reasoning_effort": "low"}
	Apply(raw, config.ProtocolOpenAIChat, &config.ThinkingConfig{Mode: ModeAuto, Effort: "high"})
	if raw["reasoning_effort"] != "low" {
		t.Fatal(raw)
	}
}
