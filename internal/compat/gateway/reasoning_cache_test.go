package gateway

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
