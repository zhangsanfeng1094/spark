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
