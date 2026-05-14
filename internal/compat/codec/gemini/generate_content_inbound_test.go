package gemini

import (
	"testing"

	"spark/internal/compatir"
)

func TestGenerateContentInboundMapsContentsAndConfig(t *testing.T) {
	req := GenerateContentInbound(map[string]any{
		"model": "gemini-2.5-flash",
		"systemInstruction": map[string]any{
			"parts": []any{
				map[string]any{"text": "be concise"},
			},
		},
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "hello"},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"text": "hi"},
					map[string]any{
						"functionCall": map[string]any{
							"id":   "call_1",
							"name": "sum",
							"args": map[string]any{"a": float64(1)},
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionResponse": map[string]any{
							"id":       "call_1",
							"name":     "sum",
							"response": map[string]any{"result": float64(1)},
						},
					},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": float64(64),
			"temperature":     float64(0.2),
			"topP":            float64(0.9),
			"stopSequences":   []any{"stop"},
		},
	})

	if req.Source != compatir.ProtocolGeminiGenerateContent || req.Model != "gemini-2.5-flash" {
		t.Fatalf("request basics mismatch: %#v", req)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	if req.Messages[0].Role != compatir.RoleSystem || req.Messages[0].Text() != "be concise" {
		t.Fatalf("system message mismatch: %#v", req.Messages[0])
	}
	if req.Messages[2].Role != compatir.RoleAssistant || req.Messages[2].Text() != "hi" {
		t.Fatalf("assistant message mismatch: %#v", req.Messages[2])
	}
	if ids := req.Messages[2].ToolCallIDs(); len(ids) != 1 || ids[0] != "call_1" {
		t.Fatalf("tool call ids mismatch: %#v", ids)
	}
	if req.Messages[3].Role != compatir.RoleTool || len(req.Messages[3].Content) != 1 {
		t.Fatalf("tool result message mismatch: %#v", req.Messages[3])
	}
	if req.Messages[3].Content[0].ToolResult.ToolCallID != "call_1" {
		t.Fatalf("tool result id mismatch: %#v", req.Messages[3].Content[0])
	}
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 64 {
		t.Fatalf("max tokens mismatch: %#v", req.Generation.MaxTokens)
	}
	if req.Generation.Temperature == nil || *req.Generation.Temperature != 0.2 {
		t.Fatalf("temperature mismatch: %#v", req.Generation.Temperature)
	}
	if req.Generation.TopP == nil || *req.Generation.TopP != 0.9 {
		t.Fatalf("topP mismatch: %#v", req.Generation.TopP)
	}
	if len(req.Generation.Stop) != 1 || req.Generation.Stop[0] != "stop" {
		t.Fatalf("stop mismatch: %#v", req.Generation.Stop)
	}
}

func TestGenerateContentInboundMapsToolsAndToolChoice(t *testing.T) {
	req := GenerateContentInbound(map[string]any{
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "sum",
						"description": "add numbers",
						"parameters":  map[string]any{"type": "object"},
					},
				},
			},
		},
		"toolConfig": map[string]any{
			"functionCallingConfig": map[string]any{
				"mode":                 "ANY",
				"allowedFunctionNames": []any{"sum"},
			},
		},
	})

	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "sum" {
		t.Fatalf("tools mismatch: %#v", req.Tools)
	}
	if req.ToolChoice.Mode != compatir.ToolChoiceFunction || req.ToolChoice.Name != "sum" {
		t.Fatalf("tool choice mismatch: %#v", req.ToolChoice)
	}
}

func TestGenerateContentInboundMapsMultimodalAndThoughtParts(t *testing.T) {
	req := GenerateContentInbound(map[string]any{
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"inlineData": map[string]any{
							"mimeType": "image/png",
							"data":     "aW1n",
						},
					},
					map[string]any{
						"fileData": map[string]any{
							"mimeType": "text/plain",
							"fileUri":  "gs://bucket/doc.txt",
						},
					},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"text":             "thinking",
						"thought":          true,
						"thoughtSignature": "sig_1",
					},
				},
			},
		},
	})

	if len(req.Messages) != 2 {
		t.Fatalf("messages mismatch: %#v", req.Messages)
	}
	blocks := req.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Image == nil || blocks[0].Image.MimeType != "image/png" {
		t.Fatalf("image block mismatch: %#v", blocks)
	}
	if blocks[1].Document == nil || blocks[1].Document.MimeType != "text/plain" || blocks[1].Document.Name != "gs://bucket/doc.txt" {
		t.Fatalf("document block mismatch: %#v", blocks[1])
	}
	reasoning := req.Messages[1].Content[0].Reasoning
	if reasoning == nil || reasoning.Text != "thinking" || reasoning.Signature != "sig_1" {
		t.Fatalf("reasoning block mismatch: %#v", req.Messages[1].Content)
	}
}
