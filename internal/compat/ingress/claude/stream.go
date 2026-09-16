package claude

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

type toolBlockState struct {
	index int
	id    string
	name  string
}

// ForwardStream processes stream chunks from Bifrost and writes Anthropic SSE events.
func ForwardStream(w http.ResponseWriter, streamChan chan *schemas.BifrostStreamChunk, requestedModel string, onUsage func(usage *schemas.BifrostLLMUsage, model string)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	msgID := "msg_spark_" + fmt.Sprintf("%d", time.Now().Unix())
	model := requestedModel
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	messageStarted := false
	var lastUsage *schemas.BifrostLLMUsage

	nextIndex := 0

	thinkingOpen := false
	thinkingIndex := -1

	textOpen := false
	textIndex := -1

	toolStates := map[int]*toolBlockState{}
	sawToolCall := false
	finishReason := ""

	startMessage := func(m string, id string) {
		if messageStarted {
			return
		}
		if id != "" {
			msgID = id
		}
		if m != "" {
			model = m
		}
		writeAnthropicSSE(w, flusher, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            msgID,
				"type":          "message",
				"role":          "assistant",
				"model":         model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		})
		messageStarted = true
	}

	closeThinking := func() {
		if thinkingOpen {
			writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": thinkingIndex,
			})
			thinkingOpen = false
		}
	}

	closeText := func() {
		if textOpen {
			writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": textIndex,
			})
			textOpen = false
		}
	}

	for chunk := range streamChan {
		if chunk == nil {
			continue
		}

		if chunk.BifrostError != nil {
			writeAnthropicSSE(w, flusher, "error", map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    chunk.BifrostError.Type,
					"message": chunk.BifrostError.GetErrorString(),
				},
			})
			break
		}

		if chunk.BifrostChatResponse == nil {
			continue
		}

		cr := chunk.BifrostChatResponse
		if cr.Model != "" {
			model = cr.Model
		}
		if cr.Usage != nil {
			lastUsage = cr.Usage
		}

		startMessage(model, cr.ID)

		if len(cr.Choices) == 0 {
			continue
		}

		choice := cr.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason = *choice.FinishReason
		}

		var delta *schemas.ChatStreamResponseChoiceDelta
		if choice.ChatStreamResponseChoice != nil {
			delta = choice.ChatStreamResponseChoice.Delta
		}
		if delta == nil {
			continue
		}

		// 1. Thinking / Reasoning delta
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			if !thinkingOpen {
				closeText()
				thinkingIndex = nextIndex
				nextIndex++
				writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": thinkingIndex,
					"content_block": map[string]any{
						"type":      "thinking",
						"thinking":  "",
						"signature": "",
					},
				})
				thinkingOpen = true
			}
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": thinkingIndex,
				"delta": map[string]any{
					"type":     "thinking_delta",
					"thinking": *delta.Reasoning,
				},
			})
		}

		// 2. Text Content delta
		if delta.Content != nil && *delta.Content != "" {
			closeThinking()
			if !textOpen {
				textIndex = nextIndex
				nextIndex++
				writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": textIndex,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				})
				textOpen = true
			}
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": textIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": *delta.Content,
				},
			})
		}

		// 3. Tool Calls
		if len(delta.ToolCalls) > 0 {
			closeThinking()
			closeText()
			sawToolCall = true

			for _, tc := range delta.ToolCalls {
				tcIndex := int(tc.Index)
				state, exists := toolStates[tcIndex]
				if !exists {
					callID := ""
					if tc.ID != nil {
						callID = *tc.ID
					}
					if callID == "" {
						callID = fmt.Sprintf("toolu_spark_%d", tcIndex)
					}
					fnName := ""
					if tc.Function.Name != nil {
						fnName = *tc.Function.Name
					}
					bIndex := nextIndex
					nextIndex++
					state = &toolBlockState{
						index: bIndex,
						id:    callID,
						name:  fnName,
					}
					toolStates[tcIndex] = state

					writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": bIndex,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    callID,
							"name":  fnName,
							"input": map[string]any{},
						},
					})
				}

				if tc.Function.Arguments != "" {
					writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": state.index,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": tc.Function.Arguments,
						},
					})
				}
			}
		}
	}

	// Ensure message_start was sent if stream was completely empty
	if !messageStarted {
		startMessage(model, "")
	}

	// Close any open blocks
	closeThinking()
	closeText()
	for _, state := range toolStates {
		writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": state.index,
		})
	}

	// Determine stop_reason
	stopReason := "end_turn"
	if sawToolCall {
		stopReason = "tool_use"
	} else if strings.Contains(strings.ToLower(finishReason), "length") {
		stopReason = "max_tokens"
	}

	outputTokens := 0
	inputTokens := 0
	if lastUsage != nil {
		outputTokens = lastUsage.CompletionTokens
		inputTokens = lastUsage.PromptTokens
	}

	// message_delta
	writeAnthropicSSE(w, flusher, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	})

	// message_stop
	writeAnthropicSSE(w, flusher, "message_stop", map[string]any{
		"type": "message_stop",
	})

	if onUsage != nil && lastUsage != nil {
		onUsage(lastUsage, model)
	}
}

func writeAnthropicSSE(w http.ResponseWriter, flusher http.Flusher, event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}
