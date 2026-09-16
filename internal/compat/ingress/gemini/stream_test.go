package gemini

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestForwardStream_TextThoughtAndToolCall(t *testing.T) {
	streamChan := make(chan *schemas.BifrostStreamChunk, 8)
	reasoning := "think "
	text := "hello"
	toolName := "sum"
	toolID := "call_1"
	toolArgs := `{"a":1}`
	finish := "tool_calls"
	usage := &schemas.BifrostLLMUsage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14}

	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "resp-1",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{{
				ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
					Delta: &schemas.ChatStreamResponseChoiceDelta{Reasoning: &reasoning},
				},
			}},
		},
	}
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "resp-1",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{{
				ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
					Delta: &schemas.ChatStreamResponseChoiceDelta{Content: &text},
				},
			}},
		},
	}
	streamChan <- &schemas.BifrostStreamChunk{
		BifrostChatResponse: &schemas.BifrostChatResponse{
			ID:    "resp-1",
			Model: "gpt-4o",
			Choices: []schemas.BifrostResponseChoice{{
				FinishReason: &finish,
				ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
					Delta: &schemas.ChatStreamResponseChoiceDelta{
						ToolCalls: []schemas.ChatAssistantMessageToolCall{{
							Index: 0,
							ID:    &toolID,
							Function: schemas.ChatAssistantMessageToolCallFunction{
								Name:      &toolName,
								Arguments: toolArgs,
							},
						}},
					},
				},
			}},
			Usage: usage,
		},
	}
	close(streamChan)

	rec := httptest.NewRecorder()
	ForwardStream(rec, streamChan, "gpt-4o", nil)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(body, `"thought":true`) {
		t.Fatalf("missing thought part:\n%s", body)
	}
	if !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("missing text part:\n%s", body)
	}
	if !strings.Contains(body, `"functionCall"`) || !strings.Contains(body, `"name":"sum"`) {
		t.Fatalf("missing function call:\n%s", body)
	}
	if !strings.Contains(body, `"finishReason":"STOP"`) {
		t.Fatalf("missing finish reason:\n%s", body)
	}
	if !strings.Contains(body, `"promptTokenCount":10`) {
		t.Fatalf("missing usage:\n%s", body)
	}

	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if !strings.HasPrefix(block, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(block, "data: ")), &event); err != nil {
			t.Fatalf("invalid sse json %q: %v", block, err)
		}
	}
}

func TestPrependStreamChunk(t *testing.T) {
	rest := make(chan *schemas.BifrostStreamChunk, 1)
	second := &schemas.BifrostStreamChunk{}
	rest <- second
	close(rest)
	first := &schemas.BifrostStreamChunk{}
	out := prependStreamChunk(first, rest)
	got := []*schemas.BifrostStreamChunk{}
	for chunk := range out {
		got = append(got, chunk)
	}
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("chunks=%v", got)
	}
}

func TestWriteGeminiStreamFromNonStream(t *testing.T) {
	text := "你好"
	finish := "stop"
	rec := httptest.NewRecorder()
	writeGeminiStreamFromNonStream(rec, &schemas.BifrostChatResponse{
		ID:    "chatcmpl-1",
		Model: "gemini-3.7-flash-high",
		Choices: []schemas.BifrostResponseChoice{{
			FinishReason: &finish,
			ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
				Message: &schemas.ChatMessage{
					Role:    schemas.ChatMessageRoleAssistant,
					Content: &schemas.ChatMessageContent{ContentStr: &text},
				},
			},
		}},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "data: ") {
		t.Fatalf("expected sse payload, got:\n%s", body)
	}
	if !strings.Contains(body, `"text":"你好"`) || !strings.Contains(body, `"finishReason":"STOP"`) {
		t.Fatalf("missing gemini payload:\n%s", body)
	}
}
