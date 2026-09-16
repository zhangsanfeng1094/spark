package claude

import (
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
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
						},
					},
					map[string]any{
						"type": "document",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "application/pdf",
							"data":       "JVBERi0xLjQKJeLjz9MKMSAwIG9iago=",
						},
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
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
		t.Errorf("Block 1 type = %v, want image_url", imgBlock.Type)
	}
	if imgBlock.ImageURLStruct == nil || imgBlock.ImageURLStruct.URL == "" {
		t.Fatalf("Block 1 ImageURLStruct is empty")
	}
	wantPrefix := "data:image/png;base64,iVBOR"
	if len(imgBlock.ImageURLStruct.URL) < len(wantPrefix) || imgBlock.ImageURLStruct.URL[:len(wantPrefix)] != wantPrefix {
		t.Errorf("Image URL = %s, want prefix %s", imgBlock.ImageURLStruct.URL, wantPrefix)
	}

	// 3. Document block
	docBlock := userMsg.Content.ContentBlocks[2]
	if docBlock.Type != schemas.ChatContentBlockTypeFile {
		t.Errorf("Block 2 type = %v, want file", docBlock.Type)
	}
	if docBlock.File == nil || docBlock.File.FileData == nil {
		t.Fatalf("Block 2 FileData is nil")
	}
	if *docBlock.File.FileType != "application/pdf" {
		t.Errorf("Block 2 FileType = %s, want application/pdf", *docBlock.File.FileType)
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
				"role": "user",
				"content": "Please inspect image.png",
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use",
						"id":   "toolu_read_1",
						"name": "read_image_file",
						"input": map[string]any{
							"path": "image.png",
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_read_1",
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "Loaded image successfully (100x100)",
							},
							map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": "image/png",
									"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
								},
							},
						},
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if len(bReq.Input) != 3 {
		t.Fatalf("Input count = %d, want 3 (user, assistant, tool with blocks)", len(bReq.Input))
	}

	// Message 0: initial user message
	if bReq.Input[0].Role != schemas.ChatMessageRoleUser {
		t.Errorf("msg[0] role = %v, want user", bReq.Input[0].Role)
	}

	// Message 1: assistant tool call
	if bReq.Input[1].Role != schemas.ChatMessageRoleAssistant {
		t.Errorf("msg[1] role = %v, want assistant", bReq.Input[1].Role)
	}

	// Message 2: tool result message with multimodal content blocks directly attached
	toolMsg := bReq.Input[2]
	if toolMsg.Role != schemas.ChatMessageRoleTool {
		t.Errorf("msg[2] role = %v, want tool", toolMsg.Role)
	}
	if toolMsg.ChatToolMessage == nil || toolMsg.ChatToolMessage.ToolCallID == nil || *toolMsg.ChatToolMessage.ToolCallID != "toolu_read_1" {
		t.Errorf("msg[2] tool_call_id mismatch: %+v", toolMsg.ChatToolMessage)
	}
	if toolMsg.Name == nil || *toolMsg.Name != "read_image_file" {
		t.Errorf("msg[2] name = %v, want read_image_file", toolMsg.Name)
	}
	if toolMsg.Content == nil || len(toolMsg.Content.ContentBlocks) != 2 {
		t.Fatalf("msg[2] content blocks count = %d, want 2 (text + image)", len(toolMsg.Content.ContentBlocks))
	}
	if *toolMsg.Content.ContentBlocks[0].Text != "Loaded image successfully (100x100)" {
		t.Errorf("msg[2] block 0 text mismatch: %s", *toolMsg.Content.ContentBlocks[0].Text)
	}
	if toolMsg.Content.ContentBlocks[1].Type != schemas.ChatContentBlockTypeImage {
		t.Errorf("msg[2] block 1 type mismatch: %v", toolMsg.Content.ContentBlocks[1].Type)
	}

	// Verify downstream Gemini conversion in Bifrost core generates the vision user turn
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

func TestToBifrostRequest_ToolResultOnlyImage(t *testing.T) {
	profile := &config.Profile{
		DefaultModel: "claude-3-7-sonnet-20250219",
	}

	rawReq := map[string]any{
		"model": "claude-3-7-sonnet-20250219",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "call_bare_img",
						"name":        "capture_screen",
						"content": []any{
							map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": "image/jpeg",
									"data":       "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP3/2wBDAP3/wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAf/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/8QAFAEBAAAAAAAAAAAAAAAAAAAAAP/EABQRAQAAAAAAAAAAAAAAAAAAAAD/2gAMAwEBAhEDEQB/AKsA/9k=",
								},
							},
						},
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	if len(bReq.Input) != 1 {
		t.Fatalf("Input count = %d, want 1 (pure tool with image block)", len(bReq.Input))
	}

	toolMsg := bReq.Input[0]
	if toolMsg.Role != schemas.ChatMessageRoleTool {
		t.Errorf("toolMsg role = %v, want tool", toolMsg.Role)
	}
	if toolMsg.Content == nil || len(toolMsg.Content.ContentBlocks) == 0 {
		t.Fatalf("toolMsg content blocks is empty")
	}
	hasImg := false
	for _, blk := range toolMsg.Content.ContentBlocks {
		if blk.Type == schemas.ChatContentBlockTypeImage {
			hasImg = true
			break
		}
	}
	if !hasImg {
		t.Errorf("toolMsg does not contain ChatContentBlockTypeImage")
	}
}

func TestToBifrostRequest_ParallelToolResultsWithImages(t *testing.T) {
	profile := &config.Profile{
		DefaultModel: "gpt-4o",
	}

	rawReq := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use",
						"id":   "call_1",
						"name": "read_chart",
					},
					map[string]any{
						"type": "tool_use",
						"id":   "call_2",
						"name": "read_metrics",
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "call_1",
						"content": []any{
							map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": "image/png",
									"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
								},
							},
						},
					},
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "call_2",
						"content":     "CPU: 22%, Mem: 45%",
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	// Assistant (1) + Tool 1 (1) + Tool 2 (1) = 3 (clean standard IR)
	if len(bReq.Input) != 3 {
		t.Fatalf("Input count = %d, want 3", len(bReq.Input))
	}

	// Tool 1 and Tool 2 must be contiguous
	if bReq.Input[1].Role != schemas.ChatMessageRoleTool || *bReq.Input[1].ChatToolMessage.ToolCallID != "call_1" {
		t.Errorf("Input[1] should be tool call_1, got %+v", bReq.Input[1])
	}
	if bReq.Input[2].Role != schemas.ChatMessageRoleTool || *bReq.Input[2].ChatToolMessage.ToolCallID != "call_2" {
		t.Errorf("Input[2] should be tool call_2, got %+v", bReq.Input[2])
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
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Analyze image",
					},
				},
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use",
						"id":   "call_read_img",
						"name": "read_image",
						"input": map[string]any{
							"file": "test.png",
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "call_read_img",
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "Read file successfully",
							},
							map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": "image/png",
									"data":       "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
								},
							},
						},
					},
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	geminiReq, bErr := gemini.ToGeminiChatCompletionRequest(nil, bReq)
	if bErr != nil {
		t.Fatalf("ToGeminiChatCompletionRequest failed: %v", bErr)
	}

	// Verify Gemini contents:
	// 1. Initial user prompt: Role "user", Part with Text
	// 2. Model tool call: Role "model", Part with FunctionCall
	// 3. Tool response: Role "user", Part with FunctionResponse (no image, clean JSON output)
	// 4. Synthesized user message: Role "user", Part 0 Text ("...attached"), Part 1 InlineData with PNG Blob
	if len(geminiReq.Contents) < 4 {
		t.Fatalf("Gemini contents length = %d, want at least 4", len(geminiReq.Contents))
	}

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
