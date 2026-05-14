package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	openaitarget "spark/internal/compat/target/openai"
	"spark/internal/compatir"
)

type MessagesStreamResult struct {
	ChunkCount      int
	SawDone         bool
	MessageStarted  bool
	EmptyStream     bool
	FirstValidChunk string
	LastValidChunk  string
	ResponseID      string
	Model           string
	ReasoningText   string
	ToolCallIDs     []string
	Usage           map[string]any
}

type messageToolState struct {
	blockIndex int
	id         string
	name       string
	args       strings.Builder
	closed     bool
}

type messagesStreamState struct {
	writer io.Writer
	flush  func()

	responseID string
	model      string

	promptTokens        int
	completionTokens    int
	messageStarted      bool
	reasoningBlockIndex int
	reasoningClosed     bool
	textBlockIndex      int
	textClosed          bool
	nextBlockIndex      int
	reasoning           strings.Builder
	toolStates          map[int]*messageToolState
	toolOrder           []int
	lastUsage           map[string]any
	stopReason          string
}

func WriteMessagesStream(writer io.Writer, reader io.Reader, requestedModel string, flush func()) MessagesStreamResult {
	state := newMessagesStreamState(writer, requestedModel, flush)
	result := MessagesStreamResult{
		ResponseID: state.responseID,
		Model:      state.model,
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var finalChunk map[string]any
	rawJSONLines := make([]string, 0, 8)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		data := ""
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if strings.HasPrefix(line, "{") {
			data = line
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			result.SawDone = true
			break
		}

		result.ChunkCount++
		rawJSONLines = append(rawJSONLines, data)
		if result.FirstValidChunk == "" {
			result.FirstValidChunk = truncateForStream(data, 512)
		}
		result.LastValidChunk = truncateForStream(data, 512)

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		finalChunk = chunk
		state.observeChunk(chunk)

		hadContentDelta := false
		for _, event := range openaitarget.ChatStreamEvents(chunk) {
			switch event.Type {
			case compatir.StreamEventUsage:
				if event.Usage != nil {
					state.observeUsage(*event.Usage)
				}
			case compatir.StreamEventContentDelta:
				switch event.Delta.Type {
				case compatir.BlockReasoning:
					if event.Delta.Reasoning == nil || event.Delta.Reasoning.Text == "" {
						continue
					}
					hadContentDelta = true
					state.writeReasoningDelta(event.Delta.Reasoning.Text)
				case compatir.BlockToolCall:
					if event.Delta.ToolCall == nil {
						continue
					}
					hadContentDelta = true
					state.writeToolCallDelta(event.Delta)
				case compatir.BlockText:
					if event.Delta.Text == "" {
						continue
					}
					hadContentDelta = true
					state.writeTextDelta(event.Delta.Text)
				}
			case compatir.StreamEventResponseDone:
				state.stopReason = anthropicStreamStopReason(event.StopReason)
			}
		}
		if hadContentDelta {
			continue
		}

		fallback := openaitarget.ChatResponse(chunk)
		if reasoning := responseReasoningText(fallback.Output); reasoning != "" {
			state.writeReasoningDelta(reasoning)
		}
		if text := responseText(fallback.Output); text != "" {
			state.writeTextDelta(text)
		}
		if len(state.toolOrder) == 0 {
			state.addFullResponseToolCalls(fallback.Output)
		}
	}

	if err := scanner.Err(); err != nil && result.ChunkCount == 0 {
		result.EmptyStream = true
		return result
	}

	if !state.messageStarted {
		if finalChunk == nil && len(rawJSONLines) == 1 {
			var full map[string]any
			if err := json.Unmarshal([]byte(rawJSONLines[0]), &full); err == nil {
				finalChunk = full
			}
		}
		if finalChunk == nil {
			result.EmptyStream = true
			return result
		}
		resp := openaitarget.ChatResponse(finalChunk)
		msg := MessagesClientResponse(resp, requestedModel)
		writeAnthropicStreamFromMessage(writer, msg, state.flush)
		result.MessageStarted = true
		result.ResponseID = stringValue(msg["id"])
		result.Model = stringValue(msg["model"])
		result.ReasoningText = responseReasoningText(resp.Output)
		result.ToolCallIDs = responseToolCallIDs(resp.Output)
		result.Usage = mapValue(msg["usage"])
		return result
	}

	state.closeReasoningBlock()
	state.closeTextBlock()
	state.closeToolBlocks()
	if state.stopReason == "" && len(state.toolOrder) > 0 {
		state.stopReason = "tool_use"
	}
	if state.stopReason == "" {
		state.stopReason = "end_turn"
	}
	writeAnthropicSSE(writer, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   state.stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  state.promptTokens,
			"output_tokens": state.completionTokens,
		},
	})
	writeAnthropicSSE(writer, "message_stop", map[string]any{
		"type": "message_stop",
	})
	state.flushNow()

	result.MessageStarted = true
	result.ResponseID = state.responseID
	result.Model = state.model
	result.ReasoningText = state.reasoning.String()
	result.ToolCallIDs = state.toolCallIDs()
	result.Usage = state.lastUsage
	return result
}

func newMessagesStreamState(writer io.Writer, requestedModel string, flush func()) *messagesStreamState {
	model := requestedModel
	if model == "" {
		model = "unknown"
	}
	return &messagesStreamState{
		writer:              writer,
		flush:               flush,
		responseID:          fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		model:               model,
		reasoningBlockIndex: -1,
		textBlockIndex:      -1,
		toolStates:          map[int]*messageToolState{},
		toolOrder:           make([]int, 0, 2),
	}
}

func (s *messagesStreamState) flushNow() {
	if s.flush != nil {
		s.flush()
	}
}

func (s *messagesStreamState) observeChunk(chunk map[string]any) {
	if id := stringValue(chunk["id"]); id != "" {
		s.responseID = id
	}
	if model := stringValue(chunk["model"]); model != "" {
		s.model = model
	}
}

func (s *messagesStreamState) observeUsage(usage compatir.Usage) {
	if usage.InputTokens > 0 {
		s.promptTokens = usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		s.completionTokens = usage.OutputTokens
	}
	s.lastUsage = map[string]any{
		"input_tokens":  s.promptTokens,
		"output_tokens": s.completionTokens,
	}
}

func (s *messagesStreamState) startMessage() {
	if s.messageStarted {
		return
	}
	s.messageStarted = true
	writeAnthropicSSE(s.writer, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      s.responseID,
			"type":    "message",
			"role":    "assistant",
			"model":   s.model,
			"content": []any{},
			"usage": map[string]any{
				"input_tokens":  s.promptTokens,
				"output_tokens": 0,
			},
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) startReasoningBlock() {
	if s.reasoningBlockIndex >= 0 {
		return
	}
	s.startMessage()
	s.reasoningBlockIndex = s.nextBlockIndex
	s.nextBlockIndex++
	writeAnthropicSSE(s.writer, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": s.reasoningBlockIndex,
		"content_block": map[string]any{
			"type":      "thinking",
			"thinking":  "",
			"signature": "",
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) writeReasoningDelta(delta string) {
	if delta == "" {
		return
	}
	s.startReasoningBlock()
	s.reasoning.WriteString(delta)
	writeAnthropicSSE(s.writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": s.reasoningBlockIndex,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": delta,
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) closeReasoningBlock() {
	if s.reasoningBlockIndex < 0 || s.reasoningClosed {
		return
	}
	s.reasoningClosed = true
	writeAnthropicSSE(s.writer, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.reasoningBlockIndex,
	})
	s.flushNow()
}

func (s *messagesStreamState) startTextBlock() {
	if s.textBlockIndex >= 0 {
		return
	}
	s.closeReasoningBlock()
	s.startMessage()
	s.textBlockIndex = s.nextBlockIndex
	s.nextBlockIndex++
	writeAnthropicSSE(s.writer, "content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": s.textBlockIndex,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) writeTextDelta(delta string) {
	if delta == "" {
		return
	}
	s.startTextBlock()
	writeAnthropicSSE(s.writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": s.textBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": delta,
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) closeTextBlock() {
	if s.textBlockIndex < 0 || s.textClosed {
		return
	}
	s.textClosed = true
	writeAnthropicSSE(s.writer, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.textBlockIndex,
	})
	s.flushNow()
}

func (s *messagesStreamState) writeToolCallDelta(block compatir.ContentBlock) {
	call := block.ToolCall
	if call == nil {
		return
	}
	idx := streamToolIndex(block.Raw, len(s.toolOrder))
	st, ok := s.toolStates[idx]
	if !ok {
		s.closeReasoningBlock()
		s.startMessage()
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), idx)
		}
		name := call.Name
		if name == "" {
			name = "unknown_tool"
		}
		st = &messageToolState{
			blockIndex: s.nextBlockIndex,
			id:         id,
			name:       name,
		}
		s.nextBlockIndex++
		s.toolStates[idx] = st
		s.toolOrder = append(s.toolOrder, idx)
		writeAnthropicSSE(s.writer, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": st.blockIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    st.id,
				"name":  st.name,
				"input": map[string]any{},
			},
		})
		s.flushNow()
	}
	if call.Arguments == "" {
		return
	}
	st.args.WriteString(call.Arguments)
	writeAnthropicSSE(s.writer, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": st.blockIndex,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": call.Arguments,
		},
	})
	s.flushNow()
}

func (s *messagesStreamState) addFullResponseToolCalls(blocks []compatir.ContentBlock) {
	for idx, tc := range responseToolCalls(blocks) {
		s.writeToolCallDelta(compatir.ContentBlock{
			Type: compatir.BlockToolCall,
			ToolCall: &compatir.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Type:      tc.Type,
				Arguments: tc.Arguments,
			},
			Raw: map[string]any{"index": idx},
		})
	}
}

func (s *messagesStreamState) closeToolBlocks() {
	for _, idx := range s.toolOrder {
		st := s.toolStates[idx]
		if st == nil || st.closed {
			continue
		}
		st.closed = true
		writeAnthropicSSE(s.writer, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": st.blockIndex,
		})
		s.flushNow()
	}
}

func (s *messagesStreamState) toolCallIDs() []string {
	out := make([]string, 0, len(s.toolOrder))
	for _, idx := range s.toolOrder {
		if st := s.toolStates[idx]; st != nil && st.id != "" {
			out = append(out, st.id)
		}
	}
	return out
}

func anthropicStreamStopReason(reason compatir.StopReason) string {
	switch reason {
	case compatir.StopReasonMaxTokens:
		return "max_tokens"
	case compatir.StopReasonToolUse:
		return "tool_use"
	default:
		return "end_turn"
	}
}

func responseReasoningText(blocks []compatir.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == compatir.BlockReasoning && block.Reasoning != nil && block.Reasoning.Text != "" {
			parts = append(parts, block.Reasoning.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responseToolCallIDs(blocks []compatir.ContentBlock) []string {
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == compatir.BlockToolCall && block.ToolCall != nil && block.ToolCall.ID != "" {
			ids = append(ids, block.ToolCall.ID)
		}
	}
	return ids
}

func writeAnthropicStreamFromMessage(writer io.Writer, msg map[string]any, flush func()) {
	usage := mapValue(msg["usage"])
	startMessage := map[string]any{
		"id":      stringValue(msg["id"]),
		"type":    "message",
		"role":    "assistant",
		"model":   stringValue(msg["model"]),
		"content": []any{},
		"usage": map[string]any{
			"input_tokens":  intFromAny(usage["input_tokens"]),
			"output_tokens": 0,
		},
	}
	writeAnthropicSSE(writer, "message_start", map[string]any{
		"type":    "message_start",
		"message": startMessage,
	})
	blockIndex := 0
	for _, raw := range listValue(msg["content"]) {
		block := mapValue(raw)
		switch stringValue(block["type"]) {
		case "thinking":
			writeAnthropicSSE(writer, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]any{
					"type":      "thinking",
					"thinking":  "",
					"signature": "",
				},
			})
			if thinking := stringValue(block["thinking"]); thinking != "" {
				writeAnthropicSSE(writer, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":     "thinking_delta",
						"thinking": thinking,
					},
				})
			}
			if signature := stringValue(block["signature"]); signature != "" {
				writeAnthropicSSE(writer, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":      "signature_delta",
						"signature": signature,
					},
				})
			}
			writeAnthropicSSE(writer, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": blockIndex,
			})
			blockIndex++
		case "text":
			writeAnthropicSSE(writer, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			if text := stringValue(block["text"]); text != "" {
				writeAnthropicSSE(writer, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": text,
					},
				})
			}
			writeAnthropicSSE(writer, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": blockIndex,
			})
			blockIndex++
		case "tool_use":
			writeAnthropicSSE(writer, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    stringValue(block["id"]),
					"name":  stringValue(block["name"]),
					"input": map[string]any{},
				},
			})
			if input := block["input"]; input != nil {
				data, _ := json.Marshal(input)
				writeAnthropicSSE(writer, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": string(data),
					},
				})
			}
			writeAnthropicSSE(writer, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": blockIndex,
			})
			blockIndex++
		}
	}
	writeAnthropicSSE(writer, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stringValue(msg["stop_reason"]),
			"stop_sequence": msg["stop_sequence"],
		},
		"usage": usage,
	})
	writeAnthropicSSE(writer, "message_stop", map[string]any{
		"type": "message_stop",
	})
	if flush != nil {
		flush()
	}
}

func writeAnthropicSSE(writer io.Writer, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = io.WriteString(writer, "event: "+event+"\n")
	_, _ = io.WriteString(writer, "data: "+string(data)+"\n\n")
}

func streamToolIndex(raw map[string]any, fallback int) int {
	if idx, ok := intValue(raw["index"]); ok && idx >= 0 {
		return idx
	}
	return fallback
}

func truncateForStream(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
