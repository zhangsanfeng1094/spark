package policy

import (
	"testing"

	"spark/internal/compatir"
)

func TestReasoningPolicyGenericOpenAIStripsReasoningContent(t *testing.T) {
	p := OpenAIChatReasoningPolicy("https://api.openai.com/v1", "gpt-4.1")
	msg := compatir.Message{
		Role: compatir.RoleAssistant,
		Content: []compatir.ContentBlock{
			compatir.Reasoning("think first"),
			{
				Type: compatir.BlockToolCall,
				ToolCall: &compatir.ToolCall{
					ID:   "call_1",
					Type: compatir.ToolTypeFunction,
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
	msg := compatir.Message{
		Role: compatir.RoleAssistant,
		Content: []compatir.ContentBlock{
			{
				Type: compatir.BlockToolCall,
				ToolCall: &compatir.ToolCall{
					ID:   "call_1",
					Type: compatir.ToolTypeFunction,
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

func TestToolPolicyRejectsResultBeforeToolCall(t *testing.T) {
	req := compatir.Request{
		Messages: []compatir.Message{
			{
				Role: compatir.RoleTool,
				Content: []compatir.ContentBlock{
					{
						Type: compatir.BlockToolResult,
						ToolResult: &compatir.ToolResult{
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
	req := compatir.Request{
		Messages: []compatir.Message{
			{
				Role: compatir.RoleAssistant,
				Content: []compatir.ContentBlock{
					{
						Type: compatir.BlockToolCall,
						ToolCall: &compatir.ToolCall{
							ID:   "call_1",
							Type: compatir.ToolTypeFunction,
							Name: "sum",
						},
					},
				},
			},
			{
				Role: compatir.RoleTool,
				Content: []compatir.ContentBlock{
					{
						Type: compatir.BlockToolResult,
						ToolResult: &compatir.ToolResult{
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
