package integrations

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnthropicToChatCompletions_BasicMapping(t *testing.T) {
	req := map[string]any{
		"model":      "gpt-4.1",
		"max_tokens": float64(256),
		"system":     "be concise",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"name": "sum",
				"input_schema": map[string]any{
					"type": "object",
				},
			},
		},
		"tool_choice": map[string]any{
			"type": "tool",
			"name": "sum",
		},
	}
	out := anthropicToChatCompletions(req)
	if out["model"] != "gpt-4.1" {
		t.Fatalf("model mismatch: %v", out["model"])
	}
	if out["max_tokens"] != 256 {
		t.Fatalf("max_tokens mismatch: %v", out["max_tokens"])
	}
	msgs, ok := out["messages"].([]map[string]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages mismatch: %#v", out["messages"])
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "be concise" {
		t.Fatalf("system message mismatch: %#v", msgs[0])
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "hello" {
		t.Fatalf("user message mismatch: %#v", msgs[1])
	}
	tools, ok := out["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools mismatch: %#v", out["tools"])
	}
	tc, ok := out["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice mismatch: %#v", out["tool_choice"])
	}
	fn, ok := tc["function"].(map[string]any)
	if !ok || fn["name"] != "sum" {
		t.Fatalf("tool_choice function mismatch: %#v", tc)
	}
}

func TestChatToAnthropicMessage_WithToolCalls(t *testing.T) {
	chatResp := map[string]any{
		"id":    "chatcmpl_1",
		"model": "gpt-4.1",
		"choices": []any{
			map[string]any{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"content": "calling tool",
					"tool_calls": []any{
						map[string]any{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "sum",
								"arguments": `{"a":1,"b":2}`,
							},
						},
					},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     float64(12),
			"completion_tokens": float64(6),
		},
	}
	msg := chatToAnthropicMessage(chatResp, "")
	if msg["type"] != "message" || msg["role"] != "assistant" {
		t.Fatalf("message shape mismatch: %#v", msg)
	}
	if msg["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason mismatch: %#v", msg["stop_reason"])
	}
	content, ok := msg["content"].([]map[string]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content mismatch: %#v", msg["content"])
	}
	if content[0]["type"] != "text" || content[0]["text"] != "calling tool" {
		t.Fatalf("text block mismatch: %#v", content[0])
	}
	if content[1]["type"] != "tool_use" || content[1]["name"] != "sum" {
		t.Fatalf("tool_use block mismatch: %#v", content[1])
	}
}

func TestForwardAnthropicStream_RealTimeTextDelta(t *testing.T) {
	p := &anthropicCompatProxy{}
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-4.1","choices":[{"delta":{"content":"Hel"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","model":"gpt-4.1","choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	rec := &flushResponseRecorder{responseRecorder: responseRecorder{header: make(http.Header)}}
	p.forwardAnthropicStream(rec, strings.NewReader(upstream), "gpt-4.1")
	out := rec.body.String()
	if !strings.Contains(out, "event: message_start") {
		t.Fatalf("missing message_start event: %q", out)
	}
	if !strings.Contains(out, `"type":"content_block_delta"`) || !strings.Contains(out, `"type":"text_delta"`) {
		t.Fatalf("missing text delta event: %q", out)
	}
	if !strings.Contains(out, `"text":"Hel"`) || !strings.Contains(out, `"text":"lo"`) {
		t.Fatalf("missing streamed chunks: %q", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Fatalf("missing message_stop event: %q", out)
	}
}
