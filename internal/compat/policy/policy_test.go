package policy

import (
	"testing"

	"spark/internal/compat/ir"
)

func TestReasoningPolicyGenericOpenAIStripsReasoningContent(t *testing.T) {
	p := OpenAIChatReasoningPolicy("https://api.openai.com/v1", "gpt-4.1")
	msg := ir.Message{
		Role: ir.RoleAssistant,
		Content: []ir.ContentBlock{
			ir.Reasoning("think first"),
			{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:   "call_1",
					Type: ir.ToolTypeFunction,
					Name: "sum",
				},
			},
		},
	}

	if _, ok := p.ChatReasoningContent(msg); ok {
		t.Fatal("expected generic OpenAI target to strip reasoning_content")
	}
}

func TestReasoningPolicyMimoRequiresEmptyReasoningForToolCalls(t *testing.T) {
	p := OpenAIChatReasoningPolicy("https://gateway.example/v1", "mimo-v2.5-pro")
	msg := ir.Message{
		Role: ir.RoleAssistant,
		Content: []ir.ContentBlock{
			{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:   "call_1",
					Type: ir.ToolTypeFunction,
					Name: "sum",
				},
			},
		},
	}

	got, ok := p.ChatReasoningContent(msg)
	if !ok || got != "" {
		t.Fatalf("expected empty reasoning_content field, got %q ok=%v", got, ok)
	}
}

func TestReasoningPolicyProviderSpecificControls(t *testing.T) {
	enabled := true
	budget := 1024
	reasoning := ir.ReasoningConfig{
		Enabled:         &enabled,
		Effort:          ir.ReasoningEffortHigh,
		BudgetTokens:    &budget,
		IncludeThoughts: &enabled,
	}

	tests := []struct {
		name        string
		policy      ReasoningPolicy
		wantEffort  bool
		wantDropped []string
	}{
		{
			name:        "openai reasoning model allows effort but drops non-chat controls",
			policy:      OpenAIChatReasoningPolicy("https://api.openai.com/v1", "gpt-4.1"),
			wantEffort:  true,
			wantDropped: []string{"thinking.type", "thinking.budget_tokens", "include_thoughts"},
		},
		{
			name:        "mimo echo provider strips unknown top-level controls",
			policy:      OpenAIChatReasoningPolicy("https://gateway.example/v1", "mimo-v2.5-pro"),
			wantDropped: []string{"reasoning_effort", "thinking.type", "thinking.budget_tokens", "include_thoughts"},
		},
		{
			name:        "strict generic provider strips unknown top-level controls",
			policy:      OpenAIChatReasoningPolicy("https://strict.example/v1", "generic-model"),
			wantDropped: []string{"reasoning_effort", "thinking.type", "thinking.budget_tokens", "include_thoughts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controls, dropped := tt.policy.ChatReasoningControls(reasoning)
			if _, ok := controls["reasoning_effort"]; ok != tt.wantEffort {
				t.Fatalf("reasoning_effort presence mismatch controls=%#v dropped=%#v", controls, dropped)
			}
			for _, want := range tt.wantDropped {
				if !containsString(dropped, want) {
					t.Fatalf("missing dropped control %q in %#v", want, dropped)
				}
			}
		})
	}
}

func TestOpenAIChatReasoningPolicyFieldPerUpstream(t *testing.T) {
	tests := []struct {
		name        string
		upstream    string
		model       string
		wantField   string
		wantEcho    bool
	}{
		{
			name:      "deepseek uses reasoning_content and echoes",
			upstream:  "https://api.deepseek.com/v1",
			model:     "deepseek-chat",
			wantField: "reasoning_content",
			wantEcho:  true,
		},
		{
			name:      "mimo uses reasoning_content and echoes",
			upstream:  "https://gateway.example/v1",
			model:     "mimo-v2.5-pro",
			wantField: "reasoning_content",
			wantEcho:  true,
		},
		{
			name:      "copilot uses reasoning_text and does not echo",
			upstream:  "https://api.githubcopilot.com",
			model:     "gpt-5",
			wantField: "reasoning_text",
			wantEcho:  false,
		},
		{
			name:      "qwen uses thought and does not echo",
			upstream:  "https://dashscope.aliyuncs.com/v1",
			model:     "qwen3-235b-a22b",
			wantField: "thought",
			wantEcho:  false,
		},
		{
			name:      "generic openai uses reasoning_content and does not echo",
			upstream:  "https://api.openai.com/v1",
			model:     "gpt-4.1",
			wantField: "reasoning_content",
			wantEcho:  false,
		},
		{
			name:      "strict generic uses reasoning_content and does not echo",
			upstream:  "https://strict.example/v1",
			model:     "generic-model",
			wantField: "reasoning_content",
			wantEcho:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := OpenAIChatReasoningPolicy(tt.upstream, tt.model)
			if p.Field != tt.wantField {
				t.Fatalf("field mismatch: want %q, got %q", tt.wantField, p.Field)
			}
			isEcho := p.Mode == ReasoningRequireForToolCalls
			if isEcho != tt.wantEcho {
				t.Fatalf("echo mismatch: want %v, got %v (mode=%q)",
					tt.wantEcho, isEcho, p.Mode)
			}
			// ChatReasoningField() must agree with Field when Field is set.
			if got := p.ChatReasoningField(); got != tt.wantField {
				t.Fatalf("ChatReasoningField mismatch: want %q, got %q", tt.wantField, got)
			}
		})
	}
}

func TestPreserveReasoningContentFieldIsReasoningContent(t *testing.T) {
	// PreserveReasoningContent is the generic model-routed openai_chat
	// fallback (used when upstreamBase==""), not an anthropic-protocol path,
	// so it writes the DeepSeek-extension "reasoning_content" field.
	p := PreserveReasoningContent()
	if p.Field != "reasoning_content" {
		t.Fatalf("PreserveReasoningContent field: want %q, got %q", "reasoning_content", p.Field)
	}
	if p.Mode != ReasoningPreserve {
		t.Fatalf("PreserveReasoningContent mode: want %q, got %q", ReasoningPreserve, p.Mode)
	}
	if got := p.ChatReasoningField(); got != "reasoning_content" {
		t.Fatalf("PreserveReasoningContent ChatReasoningField: want %q, got %q", "reasoning_content", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestToolPolicyRejectsResultBeforeToolCall(t *testing.T) {
	req := ir.Request{
		Messages: []ir.Message{
			{
				Role: ir.RoleTool,
				Content: []ir.ContentBlock{
					{
						Type: ir.BlockToolResult,
						ToolResult: &ir.ToolResult{
							ToolCallID: "call_missing",
							Output:     "{}",
						},
					},
				},
			},
		},
	}

	if err := (ToolPolicy{}).ValidateRequest(req); err == nil {
		t.Fatal("expected missing tool call validation error")
	}
}

func TestToolPolicyAcceptsKnownToolCallID(t *testing.T) {
	req := ir.Request{
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Content: []ir.ContentBlock{
					{
						Type: ir.BlockToolCall,
						ToolCall: &ir.ToolCall{
							ID:   "call_1",
							Type: ir.ToolTypeFunction,
							Name: "sum",
						},
					},
				},
			},
			{
				Role: ir.RoleTool,
				Content: []ir.ContentBlock{
					{
						Type: ir.BlockToolResult,
						ToolResult: &ir.ToolResult{
							ToolCallID: "call_1",
							Output:     `{"result":3}`,
						},
					},
				},
			},
		},
	}

	if err := (ToolPolicy{}).ValidateRequest(req); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
