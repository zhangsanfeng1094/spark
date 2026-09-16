package codex

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestForwardStream_ChatToResponsesGolden(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 10)

	id := "chatcmpl_1"
	model := "mimo-v2.5-pro"
	reasoning := "think "
	text := "Hel"
	toolName := "sum"
	toolID := "call_1"
	toolArgs1 := `{"a":`
	toolArgs2 := `1}`
	finishReason := "tool_calls"

	// Chunk 1: Reasoning
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    id,
			Model: model,
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Reasoning: &reasoning,
						},
					},
				},
			},
		},
	}

	// Chunk 2: Text content
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    id,
			Model: model,
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Content: &text,
						},
					},
				},
			},
		},
	}

	// Chunk 3: Tool call start + partial args
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    id,
			Model: model,
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							ToolCalls: []schemas.ChatAssistantMessageToolCall{
								{
									Index: 0,
									ID:    &toolID,
									Function: schemas.ChatAssistantMessageToolCallFunction{
										Name:      &toolName,
										Arguments: toolArgs1,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Chunk 4: Tool call partial args continuation + finish reason + usage
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    id,
			Model: model,
			Choices: []schemas.BifrostResponseChoice{
				{
					FinishReason: &finishReason,
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							ToolCalls: []schemas.ChatAssistantMessageToolCall{
								{
									Index: 0,
									Function: schemas.ChatAssistantMessageToolCallFunction{
										Arguments: toolArgs2,
									},
								},
							},
						},
					},
				},
			},
			Usage: &schemas.BifrostLLMUsage{
				PromptTokens:     11,
				CompletionTokens: 3,
				TotalTokens:      14,
			},
		},
	}

	close(streamChan)

	rec := httptest.NewRecorder()
	var capturedUsage *schemas.BifrostLLMUsage
	var capturedModel string

	ForwardStream(rec, streamChan, func(u *schemas.BifrostLLMUsage, m string) {
		capturedUsage = u
		capturedModel = m
	})

	got := normalizeResponsesStreamGolden(rec.Body.String())
	wantBytes, err := os.ReadFile("testdata/responses_stream_golden.txt")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}
	want := normalizeResponsesStreamGolden(string(wantBytes))

	if diff := compareGoldenText(got, want); diff != "" {
		t.Fatalf("responses stream golden mismatch:\n%s", diff)
	}

	if capturedUsage == nil || capturedUsage.PromptTokens != 11 || capturedUsage.CompletionTokens != 3 {
		t.Fatalf("capturedUsage mismatch: %+v", capturedUsage)
	}
	if capturedModel != "mimo-v2.5-pro" {
		t.Fatalf("capturedModel = %s, want mimo-v2.5-pro", capturedModel)
	}
}

func TestForwardStream_BifrostError(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 2)
	code := "rate_limit_exceeded"
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostError: &schemas.BifrostError{
			Error: &schemas.ErrorField{
				Message: "rate limit exceeded",
				Code:    &code,
			},
		},
	}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"error"`) {
		t.Fatalf("expected error type in body, got: %s", body)
	}
	if !strings.Contains(body, "rate limit exceeded") {
		t.Fatalf("expected rate limit message, got: %s", body)
	}
}

func TestForwardStream_EmptyStream(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk)
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected [DONE] in empty stream, got: %s", body)
	}
}

func TestForwardStreamAddsRequiredLifecycleStatus(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 1)
	streamChan <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type:     schemas.ResponsesStreamResponseTypeCreated,
		Response: &schemas.BifrostResponsesResponse{Object: "response"},
	}}
	close(streamChan)
	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)
	if !strings.Contains(rec.Body.String(), `"status":"in_progress"`) {
		t.Fatalf("lifecycle response must include status, got: %s", rec.Body.String())
	}
}

func TestForwardStreamAddsRequiredOutputItemStatus(t *testing.T) {
	messageType := schemas.ResponsesMessageTypeMessage
	streamChan := make(chan *schemas.BifrostStreamChunk, 1)
	streamChan <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeOutputItemAdded,
		Item: &schemas.ResponsesMessage{Type: &messageType},
	}}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)
	if !strings.Contains(rec.Body.String(), `"status":"in_progress"`) {
		t.Fatalf("output item must include status, got: %s", rec.Body.String())
	}
}

func TestForwardStreamAddsEmptySummaryForReasoningItems(t *testing.T) {
	reasoningType := schemas.ResponsesMessageTypeReasoning
	streamChan := make(chan *schemas.BifrostStreamChunk, 1)
	streamChan <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeOutputItemAdded,
		Item: &schemas.ResponsesMessage{Type: &reasoningType},
	}}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)
	if !strings.Contains(rec.Body.String(), `"summary":[]`) {
		t.Fatalf("reasoning item must include an empty summary array, got: %s", rec.Body.String())
	}
}

func TestForwardStreamAddsSummaryIndexForReasoningEvents(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 1)
	streamChan <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
	}}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)
	if !strings.Contains(rec.Body.String(), `"summary_index":0`) {
		t.Fatalf("reasoning summary event must include summary_index, got: %s", rec.Body.String())
	}
}

func TestForwardStreamAddsRequiredUsageDetails(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 1)
	streamChan <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeCompleted,
		Response: &schemas.BifrostResponsesResponse{
			Usage: &schemas.ResponsesResponseUsage{},
		},
	}}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)
	body := rec.Body.String()
	if strings.Contains(body, `"input_tokens_details":null`) || strings.Contains(body, `"output_tokens_details":null`) {
		t.Fatalf("usage token details must be objects, got: %s", body)
	}
}

func TestForwardStream_TrailingUsageChunkWithoutChoices(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 5)
	text := "Hello"
	finishReason := "stop"

	// Chunk 1: content delta
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "chatcmpl_trailing_test",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Content: &text,
						},
					},
				},
			},
		},
	}

	// Chunk 2: finish_reason="stop", but usage is nil
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "chatcmpl_trailing_test",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{
				{
					FinishReason: &finishReason,
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{},
					},
				},
			},
		},
	}

	// Chunk 3: Trailing usage chunk with empty Choices (standard OpenAI stream_options behavior)
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:      "chatcmpl_trailing_test",
			Model:   "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{},
			Usage: &schemas.BifrostLLMUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
	}

	close(streamChan)

	rec := httptest.NewRecorder()
	var capturedUsage *schemas.BifrostLLMUsage
	ForwardStream(rec, streamChan, func(u *schemas.BifrostLLMUsage, m string) {
		capturedUsage = u
	})

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.created"`) {
		t.Errorf("Expected response.created in stream, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"response.completed"`) {
		t.Errorf("Expected response.completed in stream, got:\n%s", body)
	}
	if !strings.Contains(body, `"total_tokens":150`) {
		t.Errorf("Expected response.completed to contain total_tokens: 150, got:\n%s", body)
	}
	if capturedUsage == nil || capturedUsage.TotalTokens != 150 {
		t.Errorf("Captured usage mismatch: %+v", capturedUsage)
	}
}

func TestForwardStream_StreamEndsWithoutFinishReason(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 2)
	text := "Abrupt finish text"

	// Chunk 1: content delta without any finish_reason
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "chatcmpl_abrupt_test",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Content: &text,
						},
					},
				},
			},
		},
	}

	// Stream closes immediately without finish_reason
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, nil)

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.created"`) {
		t.Errorf("Expected response.created, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"response.output_text.done"`) {
		t.Errorf("Expected output_text.done on finalized stream, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"response.output_item.done"`) {
		t.Errorf("Expected output_item.done on finalized stream, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"response.completed"`) {
		t.Errorf("Expected response.completed on finalized stream, got:\n%s", body)
	}
}

func normalizeResponsesStreamGolden(s string) string {
	replacements := []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`"created_at":[0-9]+`), `"created_at":1234567890`},
		{regexp.MustCompile(`"id":"resp_[0-9a-zA-Z_-]+"`), `"id":"resp_STATIC"`},
		{regexp.MustCompile(`"id":"rs_[0-9a-zA-Z_-]+"`), `"id":"rs_STATIC"`},
		{regexp.MustCompile(`"id":"msg_[0-9a-zA-Z_-]+"`), `"id":"msg_STATIC"`},
		{regexp.MustCompile(`"id":"fc_[0-9a-zA-Z_-]+"`), `"id":"fc_STATIC_0"`},
		{regexp.MustCompile(`"response":\{"id":"resp_[0-9a-zA-Z_-]+"`), `"response":{"id":"resp_STATIC"`},
		{regexp.MustCompile(`"item_id":"rs_[0-9a-zA-Z_-]+"`), `"item_id":"rs_STATIC"`},
		{regexp.MustCompile(`"item_id":"msg_[0-9a-zA-Z_-]+"`), `"item_id":"msg_STATIC"`},
		{regexp.MustCompile(`"item_id":"fc_[0-9a-zA-Z_-]+"`), `"item_id":"fc_STATIC_0"`},
	}
	for _, item := range replacements {
		s = item.re.ReplaceAllString(s, item.repl)
	}
	return strings.TrimSpace(s)
}

func compareGoldenText(got, want string) string {
	gotNorm := normalizeGolden(got)
	wantNorm := normalizeGolden(want)
	if gotNorm == wantNorm {
		return ""
	}
	return "got:\n" + gotNorm + "\n\nwant:\n" + wantNorm
}

func normalizeGolden(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var v any
			if err := json.Unmarshal([]byte(jsonData), &v); err == nil {
				if normBytes, err := json.Marshal(v); err == nil {
					out = append(out, "data: "+string(normBytes))
					continue
				}
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
