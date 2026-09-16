package claude

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestForwardStream_ThinkingAndTextAndToolCalls_GoldenSequence(t *testing.T) {
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

	ForwardStream(rec, streamChan, "mimo-v2.5-pro", func(u *schemas.BifrostLLMUsage, m string) {
		capturedUsage = u
		capturedModel = m
	})

	got := strings.TrimSpace(rec.Body.String())
	wantBytes, err := os.ReadFile("testdata/messages_stream_golden.txt")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}
	want := strings.TrimSpace(string(wantBytes))

	if diff := compareGoldenText(got, want); diff != "" {
		t.Fatalf("stream golden mismatch:\n%s", diff)
	}

	if capturedUsage == nil || capturedUsage.PromptTokens != 11 || capturedUsage.CompletionTokens != 3 {
		t.Fatalf("capturedUsage mismatch: %+v", capturedUsage)
	}
	if capturedModel != "mimo-v2.5-pro" {
		t.Fatalf("capturedModel = %s, want mimo-v2.5-pro", capturedModel)
	}
}

func TestForwardStream_BifrostError(t *testing.T) {
	errType := "invalid_request_error"
	streamChan := make(chan *schemas.BifrostStreamChunk, 2)
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostError: &schemas.BifrostError{
			Type: &errType,
			Error: &schemas.ErrorField{
				Message: "rate limit exceeded",
			},
		},
	}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, "claude-3-5-sonnet", nil)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected error event in body, got: %s", body)
	}
	if !strings.Contains(body, "rate limit exceeded") {
		t.Fatalf("expected rate limit message in body, got: %s", body)
	}
}

func TestForwardStream_MaxTokensStopReason(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 2)
	text := "partial response"
	finishReason := "length"

	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "msg_1",
			Model: "claude-3-5-sonnet",
			Choices: []schemas.BifrostResponseChoice{
				{
					FinishReason: &finishReason,
					ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
						Delta: &schemas.ChatStreamResponseChoiceDelta{
							Content: &text,
						},
					},
				},
			},
		},
	}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, "claude-3-5-sonnet", nil)

	body := rec.Body.String()
	if !strings.Contains(body, `"stop_reason":"max_tokens"`) {
		t.Fatalf("expected max_tokens stop reason, got: %s", body)
	}
}

func TestForwardStream_EmptyStream(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk)
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, "claude-3-5-sonnet", nil)

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("expected start and stop events in empty stream, got: %s", body)
	}
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
		if strings.HasPrefix(line, "data: ") {
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
