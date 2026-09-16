package openai

import (
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/providers/gemini"
	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestToBifrostRequest_DirectMultimodalUserContent(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
		APIKey:        "gemini-key",
		DefaultModel:  "gemini-2.5-flash",
	}

	rawReq := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "What is in this screenshot?",
					},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
						},
					},
					map[string]any{
						"type":      "input_file",
						"file_data": "JVBERi0xLjQKJeLjz9MKMSAwIG9iago=",
						"mime_type": "application/pdf",
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, nil, profile)
	if bReq.Provider != schemas.Gemini {
		t.Fatalf("Provider = %v, want gemini", bReq.Provider)
	}
	if len(bReq.Input) != 1 {
		t.Fatalf("Input count = %d, want 1", len(bReq.Input))
	}

	userMsg := bReq.Input[0]
	if userMsg.Role != schemas.ChatMessageRoleUser {
		t.Fatalf("Role = %v, want user", userMsg.Role)
	}
	if userMsg.Content == nil || len(userMsg.Content.ContentBlocks) != 3 {
		t.Fatalf("ContentBlocks count = %d, want 3", len(userMsg.Content.ContentBlocks))
	}

	// 1. Text block
	if userMsg.Content.ContentBlocks[0].Type != schemas.ChatContentBlockTypeText {
		t.Errorf("Block 0 type = %v, want text", userMsg.Content.ContentBlocks[0].Type)
	}
	if *userMsg.Content.ContentBlocks[0].Text != "What is in this screenshot?" {
		t.Errorf("Block 0 text mismatch: %s", *userMsg.Content.ContentBlocks[0].Text)
	}

	// 2. Image block
	imgBlock := userMsg.Content.ContentBlocks[1]
	if imgBlock.Type != schemas.ChatContentBlockTypeImage {
		t.Errorf("Block 1 type = %v, want image", imgBlock.Type)
	}
	if imgBlock.ImageURLStruct == nil || !strings.HasPrefix(imgBlock.ImageURLStruct.URL, "data:image/png;base64,") {
		t.Errorf("Block 1 ImageURL mismatch: %+v", imgBlock.ImageURLStruct)
	}

	// 3. Document block (PDF)
	docBlock := userMsg.Content.ContentBlocks[2]
	if docBlock.Type != schemas.ChatContentBlockTypeFile {
		t.Errorf("Block 2 type = %v, want file", docBlock.Type)
	}
	if docBlock.File == nil || docBlock.File.FileData == nil || *docBlock.File.FileType != "application/pdf" {
		t.Errorf("Block 2 file mismatch: %+v", docBlock.File)
	}
}

func TestToBifrostRequest_ToolResultImagesSynthesis(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
		APIKey:        "gemini-key",
		DefaultModel:  "gemini-2.5-flash",
	}

	rawReq := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Please view image.png",
			},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_view_img_1",
						"type": "function",
						"function": map[string]any{
							"name":      "view_image",
							"arguments": `{"path":"image.png"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_view_img_1",
				"content": map[string]any{
					"status": "success",
					"path":   "image.png",
					"image":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, nil, profile)
	// Expected messages:
	// 0: user ("Please view image.png")
	// 1: assistant (function_call view_image)
	// 2: tool (tool_result with clean content blocks carrying image block)
	if len(bReq.Input) != 3 {
		t.Fatalf("Input count = %d, want 3 (user, assistant, tool with blocks)", len(bReq.Input))
	}

	// Message 2: tool message
	toolMsg := bReq.Input[2]
	if toolMsg.Role != schemas.ChatMessageRoleTool {
		t.Errorf("msg[2] role = %v, want tool", toolMsg.Role)
	}
	if toolMsg.Name == nil || *toolMsg.Name != "view_image" {
		t.Errorf("msg[2] name = %v, want view_image", toolMsg.Name)
	}
	if toolMsg.Content == nil || len(toolMsg.Content.ContentBlocks) != 2 {
		t.Fatalf("msg[2] ContentBlocks length = %d, want 2", len(toolMsg.Content.ContentBlocks))
	}
	if toolMsg.Content.ContentBlocks[0].Type != schemas.ChatContentBlockTypeText {
		t.Errorf("msg[2] block 0 type = %v, want text", toolMsg.Content.ContentBlocks[0].Type)
	}
	if strings.Contains(*toolMsg.Content.ContentBlocks[0].Text, "iVBORw0KGgo") {
		t.Errorf("msg[2] tool output text should not contain raw base64: %s", *toolMsg.Content.ContentBlocks[0].Text)
	}
	if toolMsg.Content.ContentBlocks[1].Type != schemas.ChatContentBlockTypeImage {
		t.Errorf("msg[2] block 1 type = %v, want image", toolMsg.Content.ContentBlocks[1].Type)
	}

	// Verify downstream Gemini conversion generates vision user turn in Bifrost
	geminiReq, bErr := gemini.ToGeminiChatCompletionRequest(nil, bReq)
	if bErr != nil {
		t.Fatalf("ToGeminiChatCompletionRequest failed: %v", bErr)
	}
	if len(geminiReq.Contents) != 4 {
		t.Fatalf("Gemini contents count = %d, want 4", len(geminiReq.Contents))
	}
	if geminiReq.Contents[3].Role != "user" || len(geminiReq.Contents[3].Parts) != 2 {
		t.Fatalf("Gemini synthesized user turn mismatch: %+v", geminiReq.Contents[3])
	}
	if geminiReq.Contents[3].Parts[1].InlineData == nil {
		t.Errorf("Gemini media part should be InlineData")
	}
}

func TestToBifrostRequest_ParallelToolResultsWithImages(t *testing.T) {
	profile := &config.Profile{DefaultModel: "gpt-4o"}

	rawReq := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Check chart and query db",
			},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "view_image",
							"arguments": `{"path":"chart.png"}`,
						},
					},
					map[string]any{
						"id":   "call_2",
						"type": "function",
						"function": map[string]any{
							"name":      "query_db",
							"arguments": `{"sql":"SELECT 1"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_2",
				"content":      `{"rows":1}`,
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, nil, profile)
	// Expected:
	// 0: User
	// 1: Assistant with both call_1 and call_2
	// 2: Tool call_1 (multimodal block)
	// 3: Tool call_2 ({"rows":1})
	if len(bReq.Input) != 4 {
		t.Fatalf("Input count = %d, want 4", len(bReq.Input))
	}

	// Tool 1 and Tool 2 must be contiguous
	if bReq.Input[2].Role != schemas.ChatMessageRoleTool || *bReq.Input[2].ChatToolMessage.ToolCallID != "call_1" {
		t.Errorf("Input[2] should be tool call_1, got %+v", bReq.Input[2])
	}
	if bReq.Input[3].Role != schemas.ChatMessageRoleTool || *bReq.Input[3].ChatToolMessage.ToolCallID != "call_2" {
		t.Errorf("Input[3] should be tool call_2, got %+v", bReq.Input[3])
	}
}

func TestToBifrostRequest_GeminiProviderEndToEnd(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
		APIKey:        "gemini-key",
		DefaultModel:  "gemini-2.5-flash",
	}

	rawReq := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Analyze image",
			},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_read_img",
						"type": "function",
						"function": map[string]any{
							"name":      "read_image",
							"arguments": `{"file":"test.png"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_read_img",
				"content": map[string]any{
					"status": "ok",
					"image":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, nil, profile)
	geminiReq, bErr := gemini.ToGeminiChatCompletionRequest(nil, bReq)
	if bErr != nil {
		t.Fatalf("ToGeminiChatCompletionRequest failed: %v", bErr)
	}

	if len(geminiReq.Contents) < 4 {
		t.Fatalf("Gemini contents length = %d, want at least 4", len(geminiReq.Contents))
	}

	// Verify tool response content: Role user, Part with FunctionResponse (no image, clean JSON output)
	toolRespContent := geminiReq.Contents[2]
	if toolRespContent.Role != "user" {
		t.Errorf("tool response content role = %s, want user", toolRespContent.Role)
	}
	if len(toolRespContent.Parts) == 0 || toolRespContent.Parts[0].FunctionResponse == nil {
		t.Fatalf("tool response content part[0] is not a FunctionResponse: %+v", toolRespContent.Parts)
	}
	if toolRespContent.Parts[0].FunctionResponse.Name != "read_image" {
		t.Errorf("FunctionResponse name = %s, want read_image", toolRespContent.Parts[0].FunctionResponse.Name)
	}

	// Verify synthesized user content: Role user, Part 0 Text ("...attached"), Part 1 InlineData with PNG Blob
	synthContent := geminiReq.Contents[3]
	if synthContent.Role != "user" {
		t.Errorf("synthesized content role = %s, want user", synthContent.Role)
	}
	if len(synthContent.Parts) != 2 {
		t.Fatalf("synthesized content parts length = %d, want 2", len(synthContent.Parts))
	}
	if synthContent.Parts[0].Text == "" {
		t.Errorf("synthesized content part 0 should have text label")
	}
	if synthContent.Parts[1].InlineData == nil {
		t.Fatalf("synthesized content part 1 should be InlineData (Blob), got nil")
	}
	if synthContent.Parts[1].InlineData.MIMEType != "image/png" {
		t.Errorf("InlineData MIMEType = %s, want image/png", synthContent.Parts[1].InlineData.MIMEType)
	}
	if synthContent.Parts[1].InlineData.Data == "" {
		t.Errorf("InlineData Data is empty")
	}
}
