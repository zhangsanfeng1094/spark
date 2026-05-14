package gemini

import (
	"testing"

	"spark/internal/compatir"
)

func TestGenerateContentClientResponseMapsTextToolCallsAndUsage(t *testing.T) {
	out := GenerateContentClientResponse(compatir.Response{
		ID:    "resp_1",
		Model: "gemini-2.5-flash",
		Output: []compatir.ContentBlock{
			compatir.Text("hello"),
			{
				Type: compatir.BlockToolCall,
				ToolCall: &compatir.ToolCall{
					ID:        "call_1",
					Type:      compatir.ToolTypeFunction,
					Name:      "sum",
					Arguments: `{"a":1}`,
				},
			},
		},
		StopReason: compatir.StopReasonToolUse,
		Usage: compatir.Usage{
			InputTokens:  10,
			OutputTokens: 4,
			TotalTokens:  14,
		},
	})

	candidates := out["candidates"].([]map[string]any)
	if len(candidates) != 1 {
		t.Fatalf("candidates mismatch: %#v", out)
	}
	if candidates[0]["finishReason"] != "STOP" {
		t.Fatalf("finish reason mismatch: %#v", candidates[0])
	}
	content := candidates[0]["content"].(map[string]any)
	if content["role"] != "model" {
		t.Fatalf("content role mismatch: %#v", content)
	}
	parts := content["parts"].([]map[string]any)
	if len(parts) != 2 || parts[0]["text"] != "hello" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
	call := parts[1]["functionCall"].(map[string]any)
	if call["id"] != "call_1" || call["name"] != "sum" {
		t.Fatalf("function call mismatch: %#v", call)
	}
	args := call["args"].(map[string]any)
	if args["a"] != float64(1) {
		t.Fatalf("function args mismatch: %#v", args)
	}
	usage := out["usageMetadata"].(map[string]any)
	if usage["promptTokenCount"] != 10 || usage["candidatesTokenCount"] != 4 || usage["totalTokenCount"] != 14 {
		t.Fatalf("usage mismatch: %#v", usage)
	}
}
