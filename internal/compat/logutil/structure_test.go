package logutil

import (
	"strings"
	"testing"

	"spark/internal/compat/ir"
)

func TestStructureJSONForLogRedactsConversationContent(t *testing.T) {
	got := StructureJSONForLog(map[string]any{
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
		"metadata": map[string]any{
			"turn_id": "turn_1",
			"trace":   "keep exact metadata",
		},
		"stream_options":      map[string]any{"include_usage": true},
		"parallel_tool_calls": true,
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
		`"turn_id":"turn_1"`,
		`"trace":"keep exact metadata"`,
		`"include_usage":true`,
		`"parallel_tool_calls":true`,
		`"<text len=24>"`,
		`"<json len=12>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log structure missing %s in %s", want, got)
		}
	}
}

func TestStructureJSONForLogPreservesNonTextFields(t *testing.T) {
	got := StructureJSONForLog(map[string]any{
		"model":             "deepseek-v4-flash",
		"stream":            true,
		"temperature":       0.2,
		"max_tokens":        123,
		"reasoning_effort":  "high",
		"service_tier":      "auto",
		"tool_choice":       "auto",
		"stream_options":    map[string]any{"include_usage": true},
		"response_format":   map[string]any{"type": "json_object"},
		"reasoning":         "private chain",
		"output_text":       "private output",
		"private_text_note": "this is not a protocol text field",
	})

	for _, want := range []string{
		`"model":"deepseek-v4-flash"`,
		`"stream":true`,
		`"temperature":0.2`,
		`"max_tokens":123`,
		`"reasoning_effort":"high"`,
		`"service_tier":"auto"`,
		`"tool_choice":"auto"`,
		`"include_usage":true`,
		`"type":"json_object"`,
		`"private_text_note":"this is not a protocol text field"`,
		`"<reasoning len=13>"`,
		`"<output len=14>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log structure missing %s in %s", want, got)
		}
	}
	for _, leaked := range []string{"private chain", "private output"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("log structure leaked %q in %s", leaked, got)
		}
	}
}

func TestStructureJSONForLogRedactsIRStructsAndPreservesUsage(t *testing.T) {
	got := StructureJSONForLog(ir.Response{
		ID:    "chatcmpl_1",
		Model: "mimo-v2.5-pro",
		Output: []ir.ContentBlock{
			ir.Reasoning("private reasoning"),
			ir.Text("private answer"),
			{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        "call_1",
					Type:      ir.ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"secret":1}`,
				},
			},
		},
		Usage: ir.Usage{
			InputTokens:          10,
			OutputTokens:         5,
			TotalTokens:          15,
			CacheReadInputTokens: 4,
			Raw: map[string]any{
				"prompt_tokens": float64(10),
				"prompt_tokens_details": map[string]any{
					"cached_tokens": float64(4),
				},
			},
		},
	})

	for _, leaked := range []string{"private reasoning", "private answer", `{"secret":1}`} {
		if strings.Contains(got, leaked) {
			t.Fatalf("log structure leaked %q in %s", leaked, got)
		}
	}
	for _, want := range []string{
		`"ID":"chatcmpl_1"`,
		`"Model":"mimo-v2.5-pro"`,
		`"ID":"call_1"`,
		`"Name":"sum"`,
		`"InputTokens":10`,
		`"OutputTokens":5`,
		`"TotalTokens":15`,
		`"CacheReadInputTokens":4`,
		`"cached_tokens":4`,
		`"<text len=17>"`,
		`"<text len=14>"`,
		`"<json len=12>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log structure missing %s in %s", want, got)
		}
	}
}

func TestStructureJSONForLogRedactsToolDescriptionsAndToolInputs(t *testing.T) {
	got := StructureJSONForLog(map[string]any{
		"system": "private system prompt",
		"tools": []any{
			map[string]any{
				"name":        "exec_command",
				"description": "very long private tool description",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cmd": map[string]any{
							"type":        "string",
							"description": "private command description",
						},
					},
				},
			},
		},
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_1",
						"name":  "exec_command",
						"input": map[string]any{"cmd": "private command"},
					},
				},
			},
		},
	})

	for _, leaked := range []string{
		"private system prompt",
		"very long private tool description",
		"private command description",
		"private command",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("log structure leaked %q in %s", leaked, got)
		}
	}
	for _, want := range []string{
		`"name":"exec_command"`,
		`"type":"object"`,
		`"cmd"`,
		`"<description len=34>"`,
		`"<description len=27>"`,
		`"<input items=1>"`,
		`"<text len=21>"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log structure missing %s in %s", want, got)
		}
	}
}
