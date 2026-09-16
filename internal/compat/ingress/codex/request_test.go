package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/providers/gemini"
	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestToBifrostRequest_ParallelToolCallsGrouping(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://api.openai.com/v1",
		APIKey:        "sk-test",
		DefaultModel:  "gpt-4o",
	}

	rawReq := map[string]any{
		"model":        "gpt-4o",
		"instructions": "You are a helpful coding assistant.",
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Calculate 1+1 and 2+2",
			},
			map[string]any{
				"type": "reasoning",
				"summary": []any{
					map[string]any{"text": "Planning to call add twice"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "add",
				"arguments": `{"a":1,"b":1}`,
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_2",
				"name":      "add",
				"arguments": `{"a":2,"b":2}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  `{"result":2}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_2",
				"output":  `{"result":4}`,
			},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"name": "add",
				"description": "Add two numbers",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"a": map[string]any{"type": "number"},
						"b": map[string]any{"type": "number"},
					},
					"required": []any{"a", "b"},
				},
				"strict": true,
			},
		},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"stop":                []any{"<|im_end|>", "<|stop|>"},
		"prompt_cache_key":    "cache_key_xyz",
		"max_output_tokens":   4096,
		"temperature":         0.2,
		"top_p":               0.95,
	}

	bReq := ToBifrostRequest(rawReq, profile)

	// Check messages structure
	// Expected:
	// 0: system (from instructions)
	// 1: user ("Calculate 1+1 and 2+2")
	// 2: assistant (reasoning + BOTH call_1 and call_2 in ToolCalls)
	// 3: tool (call_1 result)
	// 4: tool (call_2 result)
	if len(bReq.Input) != 5 {
		t.Fatalf("Expected 5 messages, got %d", len(bReq.Input))
	}

	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Errorf("Message[0] role = %v, want system", bReq.Input[0].Role)
	}
	if bReq.Input[1].Role != schemas.ChatMessageRoleUser {
		t.Errorf("Message[1] role = %v, want user", bReq.Input[1].Role)
	}

	// Message 2 MUST be assistant with both tool calls
	asst := bReq.Input[2]
	if asst.Role != schemas.ChatMessageRoleAssistant {
		t.Fatalf("Message[2] role = %v, want assistant", asst.Role)
	}
	if asst.ChatAssistantMessage == nil {
		t.Fatalf("Message[2] ChatAssistantMessage is nil")
	}
	if asst.ChatAssistantMessage.Reasoning == nil || *asst.ChatAssistantMessage.Reasoning != "Planning to call add twice" {
		t.Errorf("Reasoning = %v, want 'Planning to call add twice'", asst.ChatAssistantMessage.Reasoning)
	}
	if len(asst.ChatAssistantMessage.ToolCalls) != 2 {
		t.Fatalf("Message[2] ToolCalls length = %d, want 2", len(asst.ChatAssistantMessage.ToolCalls))
	}
	if *asst.ChatAssistantMessage.ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCalls[0].ID = %v, want call_1", *asst.ChatAssistantMessage.ToolCalls[0].ID)
	}
	if *asst.ChatAssistantMessage.ToolCalls[1].ID != "call_2" {
		t.Errorf("ToolCalls[1].ID = %v, want call_2", *asst.ChatAssistantMessage.ToolCalls[1].ID)
	}

	// Message 3 & 4 MUST be tool messages
	tool1 := bReq.Input[3]
	if tool1.Role != schemas.ChatMessageRoleTool || tool1.ChatToolMessage == nil || *tool1.ChatToolMessage.ToolCallID != "call_1" {
		t.Errorf("Message[3] tool call mismatch: %+v", tool1)
	}
	tool2 := bReq.Input[4]
	if tool2.Role != schemas.ChatMessageRoleTool || tool2.ChatToolMessage == nil || *tool2.ChatToolMessage.ToolCallID != "call_2" {
		t.Errorf("Message[4] tool call mismatch: %+v", tool2)
	}

	// Check params
	if bReq.Params == nil {
		t.Fatalf("Params is nil")
	}
	if bReq.Params.ParallelToolCalls == nil || !*bReq.Params.ParallelToolCalls {
		t.Errorf("ParallelToolCalls mismatch: %+v", bReq.Params.ParallelToolCalls)
	}
	if bReq.Params.ToolChoice == nil || bReq.Params.ToolChoice.ChatToolChoiceStr == nil || *bReq.Params.ToolChoice.ChatToolChoiceStr != "auto" {
		t.Errorf("ToolChoice mismatch: %+v", bReq.Params.ToolChoice)
	}
	if len(bReq.Params.Stop) != 2 || bReq.Params.Stop[0] != "<|im_end|>" || bReq.Params.Stop[1] != "<|stop|>" {
		t.Errorf("Stop mismatch: %+v", bReq.Params.Stop)
	}
	if bReq.Params.PromptCacheKey == nil || *bReq.Params.PromptCacheKey != "cache_key_xyz" {
		t.Errorf("PromptCacheKey mismatch: %+v", bReq.Params.PromptCacheKey)
	}
	if bReq.Params.MaxCompletionTokens == nil || *bReq.Params.MaxCompletionTokens != 4096 {
		t.Errorf("MaxCompletionTokens = %v, want 4096", bReq.Params.MaxCompletionTokens)
	}

	// Check tool strict
	if len(bReq.Params.Tools) != 1 {
		t.Fatalf("Tools length = %d, want 1", len(bReq.Params.Tools))
	}
	tool := bReq.Params.Tools[0]
	if tool.Function == nil || tool.Function.Strict == nil || !*tool.Function.Strict {
		t.Errorf("Tool function strict mismatch: %+v", tool.Function)
	}
}

func TestToBifrostRequest_DeveloperRoleAndToolRole(t *testing.T) {
	profile := &config.Profile{DefaultModel: "gpt-4o"}

	callID := "call_prev"
	rawReq := map[string]any{
		"input": []any{
			map[string]any{
				"role":    "developer",
				"content": "Developer instructions here",
			},
			map[string]any{
				"role":    "user",
				"content": "Hello",
			},
			map[string]any{
				"role":    "assistant",
				"content": "Calling tool now",
				"tool_calls": []any{
					map[string]any{
						"id":   callID,
						"type": "function",
						"function": map[string]any{
							"name":      "my_tool",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      "Tool output data",
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)

	if len(bReq.Input) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(bReq.Input))
	}

	// Developer role should map to System
	if bReq.Input[0].Role != schemas.ChatMessageRoleSystem {
		t.Errorf("Message[0] role = %v, want system", bReq.Input[0].Role)
	}
	if bReq.Input[0].Content == nil || *bReq.Input[0].Content.ContentStr != "Developer instructions here" {
		t.Errorf("Message[0] content mismatch: %+v", bReq.Input[0].Content)
	}

	// User role
	if bReq.Input[1].Role != schemas.ChatMessageRoleUser {
		t.Errorf("Message[1] role = %v, want user", bReq.Input[1].Role)
	}

	// Assistant role with tool call
	if bReq.Input[2].Role != schemas.ChatMessageRoleAssistant {
		t.Errorf("Message[2] role = %v, want assistant", bReq.Input[2].Role)
	}

	// Tool role with tool_call_id
	if bReq.Input[3].Role != schemas.ChatMessageRoleTool {
		t.Errorf("Message[3] role = %v, want tool", bReq.Input[3].Role)
	}
	if bReq.Input[3].ChatToolMessage == nil || *bReq.Input[3].ChatToolMessage.ToolCallID != callID {
		t.Errorf("Message[3] ToolCallID mismatch: %+v", bReq.Input[3].ChatToolMessage)
	}
}

func TestToBifrostRequest_TrailingFunctionCalls(t *testing.T) {
	profile := &config.Profile{DefaultModel: "gpt-4o"}

	rawReq := map[string]any{
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Run bash command",
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_bash_1",
				"name":      "bash",
				"arguments": `{"cmd":"ls -la"}`,
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)

	// Trailing function_call must be flushed into an assistant message at the end
	if len(bReq.Input) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(bReq.Input))
	}
	if bReq.Input[1].Role != schemas.ChatMessageRoleAssistant {
		t.Fatalf("Message[1] role = %v, want assistant", bReq.Input[1].Role)
	}
	asst := bReq.Input[1].ChatAssistantMessage
	if asst == nil || len(asst.ToolCalls) != 1 {
		t.Fatalf("Message[1] tool calls mismatch: %+v", asst)
	}
	if *asst.ToolCalls[0].ID != "call_bash_1" {
		t.Errorf("Tool call ID = %v, want call_bash_1", *asst.ToolCalls[0].ID)
	}
}

func TestToBifrostRequest_TextFormatJSONSchema(t *testing.T) {
	profile := &config.Profile{DefaultModel: "gpt-4o"}

	rawReq := map[string]any{
		"input": "Produce JSON output",
		"text": map[string]any{
			"format": map[string]any{
				"type": "json_schema",
				"name": "result_schema",
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"answer": map[string]any{"type": "string"},
					},
					"required": []any{"answer"},
				},
				"strict": true,
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)

	if bReq.Params == nil || bReq.Params.ResponseFormat == nil {
		t.Fatalf("ResponseFormat is nil")
	}
	rfBytes, err := json.Marshal(bReq.Params.ResponseFormat)
	if err != nil {
		t.Fatalf("Failed to marshal ResponseFormat: %v", err)
	}
	var rfMap map[string]any
	if err := json.Unmarshal(rfBytes, &rfMap); err != nil {
		t.Fatalf("Failed to unmarshal ResponseFormat: %v", err)
	}
	if rfMap["type"] != "json_schema" {
		t.Errorf("ResponseFormat type = %v, want json_schema", rfMap["type"])
	}
}

func TestToBifrostRequest_ReasoningVendorAliases(t *testing.T) {
	profile := &config.Profile{DefaultModel: "gpt-4o"}

	cases := []struct {
		name     string
		field    string
		expected string
	}{
		{"reasoning_content", "reasoning_content", "Reasoning via reasoning_content"},
		{"reasoning", "reasoning", "Reasoning via reasoning"},
		{"thought", "thought", "Reasoning via thought"},
		{"thoughts", "thoughts", "Reasoning via thoughts"},
		{"think", "think", "Reasoning via think"},
		{"thinking", "thinking", "Reasoning via thinking"},
		{"reasoning_text", "reasoning_text", "Reasoning via reasoning_text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawReq := map[string]any{
				"input": []any{
					map[string]any{
						"role":    "user",
						"content": "Hello",
					},
					map[string]any{
						"role":    "assistant",
						tc.field:  tc.expected,
						"content": "Final answer",
					},
				},
			}

			bReq := ToBifrostRequest(rawReq, profile)
			if len(bReq.Input) != 2 {
				t.Fatalf("Expected 2 messages, got %d", len(bReq.Input))
			}
			asst := bReq.Input[1]
			if asst.ChatAssistantMessage == nil || asst.ChatAssistantMessage.Reasoning == nil {
				t.Fatalf("Expected assistant message with reasoning for %s", tc.name)
			}
			if *asst.ChatAssistantMessage.Reasoning != tc.expected {
				t.Errorf("Expected reasoning %q, got %q", tc.expected, *asst.ChatAssistantMessage.Reasoning)
			}
		})
	}

	// Test summary array format
	t.Run("summary_array", func(t *testing.T) {
		rawReq := map[string]any{
			"input": []any{
				map[string]any{
					"role":    "user",
					"content": "Hello",
				},
				map[string]any{
					"role": "assistant",
					"summary": []any{
						map[string]any{"text": "Part 1 "},
						map[string]any{"text": "Part 2"},
					},
					"content": "Final answer",
				},
			},
		}
		bReq := ToBifrostRequest(rawReq, profile)
		if len(bReq.Input) != 2 {
			t.Fatalf("Expected 2 messages, got %d", len(bReq.Input))
		}
		asst := bReq.Input[1]
		if asst.ChatAssistantMessage == nil || asst.ChatAssistantMessage.Reasoning == nil {
			t.Fatalf("Expected assistant message with reasoning")
		}
		if *asst.ChatAssistantMessage.Reasoning != "Part 1 Part 2" {
			t.Errorf("Expected reasoning %q, got %q", "Part 1 Part 2", *asst.ChatAssistantMessage.Reasoning)
		}
	})

	// Test content blocks format
	t.Run("content_blocks", func(t *testing.T) {
		rawReq := map[string]any{
			"input": []any{
				map[string]any{
					"role":    "user",
					"content": "Hello",
				},
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "reasoning", "text": "Thought process"},
						map[string]any{"type": "text", "text": "Answer"},
					},
				},
			},
		}
		bReq := ToBifrostRequest(rawReq, profile)
		if len(bReq.Input) != 2 {
			t.Fatalf("Expected 2 messages, got %d", len(bReq.Input))
		}
		asst := bReq.Input[1]
		if asst.ChatAssistantMessage == nil || asst.ChatAssistantMessage.Reasoning == nil {
			t.Fatalf("Expected assistant message with reasoning")
		}
		if *asst.ChatAssistantMessage.Reasoning != "Thought process" {
			t.Errorf("Expected reasoning %q, got %q", "Thought process", *asst.ChatAssistantMessage.Reasoning)
		}
	})
}

func TestToBifrostRequest_DirectMultimodalUserContent(t *testing.T) {
	profile := &config.Profile{
		OpenAIBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		OpenAIAPIType: config.OpenAIAPITypeGeminiGenerateContent,
		APIKey:        "gemini-key",
		DefaultModel:  "gemini-2.5-flash",
	}

	rawReq := map[string]any{
		"model": "gemini-2.5-flash",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "What is in this screenshot?",
					},
					map[string]any{
						"type": "input_image",
						"image_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
					},
					map[string]any{
						"type":      "input_image",
						"file_data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
						"mime_type": "image/webp",
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
	if userMsg.Content == nil || len(userMsg.Content.ContentBlocks) != 4 {
		t.Fatalf("ContentBlocks count = %d, want 4", len(userMsg.Content.ContentBlocks))
	}

	// 1. Text block
	if userMsg.Content.ContentBlocks[0].Type != schemas.ChatContentBlockTypeText {
		t.Errorf("Block 0 type = %v, want text", userMsg.Content.ContentBlocks[0].Type)
	}
	if *userMsg.Content.ContentBlocks[0].Text != "What is in this screenshot?" {
		t.Errorf("Block 0 text mismatch: %s", *userMsg.Content.ContentBlocks[0].Text)
	}

	// 2. Image block 1 (input_image with image_url)
	imgBlock1 := userMsg.Content.ContentBlocks[1]
	if imgBlock1.Type != schemas.ChatContentBlockTypeImage {
		t.Errorf("Block 1 type = %v, want image", imgBlock1.Type)
	}
	if imgBlock1.ImageURLStruct == nil || !strings.HasPrefix(imgBlock1.ImageURLStruct.URL, "data:image/png;base64,") {
		t.Errorf("Block 1 ImageURL mismatch: %+v", imgBlock1.ImageURLStruct)
	}

	// 3. Image block 2 (Grok input_image with file_data + mime_type)
	imgBlock2 := userMsg.Content.ContentBlocks[2]
	if imgBlock2.Type != schemas.ChatContentBlockTypeImage {
		t.Errorf("Block 2 type = %v, want image", imgBlock2.Type)
	}
	if imgBlock2.ImageURLStruct == nil || !strings.HasPrefix(imgBlock2.ImageURLStruct.URL, "data:image/webp;base64,") {
		t.Errorf("Block 2 ImageURL mismatch: %+v", imgBlock2.ImageURLStruct)
	}

	// 4. Document block (PDF)
	docBlock := userMsg.Content.ContentBlocks[3]
	if docBlock.Type != schemas.ChatContentBlockTypeFile {
		t.Errorf("Block 3 type = %v, want file", docBlock.Type)
	}
	if docBlock.File == nil || docBlock.File.FileData == nil || *docBlock.File.FileType != "application/pdf" {
		t.Errorf("Block 3 file mismatch: %+v", docBlock.File)
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
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Please view image.png",
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_view_img_1",
				"name":      "view_image",
				"arguments": `{"path":"image.png"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_view_img_1",
				"output": map[string]any{
					"status": "success",
					"path":   "image.png",
					"image":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
	// Expected messages:
	// 0: user ("Please view image.png")
	// 1: assistant (function_call view_image)
	// 2: tool (function_call_output with content blocks carrying clean JSON text and image block)
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
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Check chart and query db",
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "view_image",
				"arguments": `{"path":"chart.png"}`,
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_2",
				"name":      "query_db",
				"arguments": `{"sql":"SELECT 1"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_2",
				"output":  `{"rows":1}`,
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
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
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": "Analyze image",
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_read_img",
				"name":      "read_image",
				"arguments": `{"file":"test.png"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read_img",
				"output": map[string]any{
					"status": "ok",
					"image":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
				},
			},
		},
	}

	bReq := ToBifrostRequest(rawReq, profile)
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
