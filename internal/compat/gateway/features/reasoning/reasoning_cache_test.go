package reasoning

import "testing"

func TestChatReasoningAdapterAppliesStatelessTargetRules(t *testing.T) {
	adapter := ChatReasoningAdapter{UpstreamBase: "https://gateway.example/v1"}
	chatReq := map[string]any{
		"model": "mimo-v2.5-pro",
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	got, ok := msgs[0]["reasoning_content"]
	if !ok || got != "" {
		t.Fatalf("expected empty reasoning_content for stateless MiMo target, got %#v", msgs[0])
	}
}

func TestChatReasoningAdapterStripsReasoningForGenericGateway(t *testing.T) {
	adapter := ChatReasoningAdapter{UpstreamBase: "https://api.openai.com/v1"}
	chatReq := map[string]any{
		"model": "gpt-4.1",
		"messages": []map[string]any{
			{
				"role":              "assistant",
				"content":           "",
				"reasoning_content": "think first",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content for generic gateway, got %#v", msgs[0])
	}
}

func TestChatReasoningAdapterRemembersReasoningWithStore(t *testing.T) {
	var cache ReasoningCache
	adapter := ChatReasoningAdapter{
		UpstreamBase: "https://api.openai.com/v1",
		Cache:        &cache,
	}
	adapter.RememberForToolCallIDs([]string{"call_1"}, "think first")
	chatReq := map[string]any{
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	if msgs[0]["reasoning_content"] != "think first" {
		t.Fatalf("expected cached reasoning_content, got %#v", msgs[0])
	}
}

func TestChatReasoningAdapterWritesCopilotReasoningTextField(t *testing.T) {
	var cache ReasoningCache
	adapter := ChatReasoningAdapter{
		UpstreamBase: "https://api.githubcopilot.com",
		Cache:        &cache,
	}
	adapter.RememberForToolCallIDs([]string{"call_1"}, "think first")
	chatReq := map[string]any{
		"model": "gpt-5",
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	if got := msgs[0]["reasoning_text"]; got != "think first" {
		t.Fatalf("expected reasoning_text=%q for copilot, got %#v", "think first", msgs[0])
	}
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content key for copilot, got %#v", msgs[0])
	}
}

func TestChatReasoningAdapterWritesQwenThoughtField(t *testing.T) {
	var cache ReasoningCache
	adapter := ChatReasoningAdapter{
		UpstreamBase: "https://dashscope.aliyuncs.com/v1",
		Cache:        &cache,
	}
	adapter.RememberForToolCallIDs([]string{"call_1"}, "think first")
	chatReq := map[string]any{
		"model": "qwen3-235b",
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	if got := msgs[0]["thought"]; got != "think first" {
		t.Fatalf("expected thought=%q for qwen, got %#v", "think first", msgs[0])
	}
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content key for qwen, got %#v", msgs[0])
	}
}

func TestChatReasoningAdapterNoEmptyPaddingForNonEchoUpstream(t *testing.T) {
	// Stateless adapter (no Cache) on a non-echo upstream: a tool-call-bearing
	// assistant message with no cache hit must NOT get an empty reasoning
	// field padded in. Locks the decision that only echo upstreams
	// (deepseek/mimo/xiaomimimo) force empty reasoning_content.
	adapter := ChatReasoningAdapter{UpstreamBase: "https://api.githubcopilot.com"}
	chatReq := map[string]any{
		"model": "gpt-5",
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"a":1}`,
						},
					},
				},
			},
		},
	}

	adapter.ApplyToChatRequest(chatReq)

	msgs := chatReq["messages"].([]map[string]any)
	if _, ok := msgs[0]["reasoning_text"]; ok {
		t.Fatalf("did not expect empty reasoning_text padding for non-echo upstream, got %#v", msgs[0])
	}
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("did not expect empty reasoning_content padding for non-echo upstream, got %#v", msgs[0])
	}
	if _, ok := msgs[0]["thought"]; ok {
		t.Fatalf("did not expect empty thought padding for non-echo upstream, got %#v", msgs[0])
	}
}
