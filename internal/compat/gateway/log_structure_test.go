package gateway

import (
	"strings"
	"testing"
)

func TestStructureJSONForLogRedactsConversationContent(t *testing.T) {
	got := structureJSONForLog(map[string]any{
		"model": "mimo-v2.5-pro",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "please keep this private",
			},
			map[string]any{
				"role":              "assistant",
				"content":           "private answer",
				"reasoning_content": "private reasoning",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "sum",
							"arguments": `{"secret":1}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      `{"result":"private tool output"}`,
			},
		},
		"output_text": "private final output",
	})

	for _, secret := range []string{
		"please keep this private",
		"private answer",
		"private reasoning",
		`{"secret":1}`,
		"private tool output",
		"private final output",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("log structure leaked %q in %s", secret, got)
		}
	}
	for _, want := range []string{
		`"model":"mimo-v2.5-pro"`,
		`"role":"user"`,
		`"role":"assistant"`,
		`"role":"tool"`,
		`"name":"sum"`,
		`"id":"call_1"`,
		`"tool_call_id":"call_1"`,
		`"<text len=24>"`,
		`"<json len=12>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log structure missing %s in %s", want, got)
		}
	}
}
