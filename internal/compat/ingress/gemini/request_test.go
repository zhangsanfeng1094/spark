package gemini

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestToBifrostRequest_AGYGenerateContent(t *testing.T) {
	t.Parallel()
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-test",
		DefaultModel:  "gpt-4o",
	}
	raw := map[string]any{
		"model": "gemini-3.7-flash",
		"systemInstruction": map[string]any{
			"parts": []any{map[string]any{"text": "You are Antigravity."}},
		},
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "say ok"}},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"text": "thinking", "thought": true},
					map[string]any{"text": "hello"},
					map[string]any{
						"functionCall": map[string]any{
							"id":   "call_1",
							"name": "sum",
							"args": map[string]any{"a": 1},
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionResponse": map[string]any{
							"id":   "call_1",
							"name": "sum",
							"response": map[string]any{
								"result": 1,
							},
						},
					},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "sum",
						"description": "Add numbers",
						"parameters": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"a": map[string]any{"type": "number"},
							},
						},
					},
				},
			},
		},
		"toolConfig": map[string]any{
			"functionCallingConfig": map[string]any{"mode": "AUTO"},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 1024,
			"temperature":     0.4,
			"topP":            1,
			"topK":            50,
			"thinkingConfig": map[string]any{
				"includeThoughts": true,
				"thinkingBudget":  1000,
			},
		},
		"stream": true,
	}

	bReq := ToBifrostRequest(raw, profile)
	if bReq.Model != "gemini-3.7-flash" {
		t.Fatalf("model=%q", bReq.Model)
	}
	if bReq.Provider != schemas.OpenAI {
		t.Fatalf("provider=%v", bReq.Provider)
	}
	if len(bReq.Input) < 4 {
		t.Fatalf("input count=%d want >=4", len(bReq.Input))
	}
	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Fatalf("first role=%v", bReq.Input[0].Role)
	}
	if bReq.Params == nil || bReq.Params.MaxCompletionTokens == nil || *bReq.Params.MaxCompletionTokens != 1024 {
		t.Fatalf("max tokens=%v", bReq.Params)
	}
	if bReq.Params.Reasoning == nil || bReq.Params.Reasoning.MaxTokens == nil || *bReq.Params.Reasoning.MaxTokens != 1000 {
		t.Fatalf("reasoning=%v", bReq.Params.Reasoning)
	}
	if len(bReq.Params.Tools) != 1 || bReq.Params.Tools[0].Function == nil || bReq.Params.Tools[0].Function.Name != "sum" {
		t.Fatalf("tools=%v", bReq.Params.Tools)
	}
	if bReq.Params.StreamOptions == nil || bReq.Params.StreamOptions.IncludeUsage == nil || !*bReq.Params.StreamOptions.IncludeUsage {
		t.Fatalf("stream options=%v", bReq.Params.StreamOptions)
	}
}

func TestToBifrostRequest_StripsModelsPrefix(t *testing.T) {
	t.Parallel()
	bReq := ToBifrostRequest(map[string]any{"model": "models/gemini-pro", "contents": []any{}}, nil)
	if bReq.Model != "gemini-pro" {
		t.Fatalf("model=%q", bReq.Model)
	}
}

func TestToBifrostRequest_RemapsAGYNativeModel(t *testing.T) {
	t.Parallel()
	profile := &config.Profile{
		OpenAIBaseURL: "http://example.invalid/v1",
		APIKey:        "cpa-test",
		DefaultModel:  "gemini-3.7-flash-high",
		Models:        []string{"gemini-3.7-flash-high", "gemini-3.1-flash-lite"},
	}
	bReq := ToBifrostRequest(map[string]any{
		"model":    "gemini-3.7-flash",
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}},
	}, profile)
	if bReq.Model != "gemini-3.7-flash-high" {
		t.Fatalf("model=%q", bReq.Model)
	}
}
