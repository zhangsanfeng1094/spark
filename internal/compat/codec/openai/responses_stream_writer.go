package openai

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

type ResponsesStreamResult struct {
	ChunkCount       int
	ExtractedTextLen int
	ChunkSamples     []string
	SawDone          bool
	SawContentDelta  bool
	ReasoningLen     int
	FirstValidChunk  string
	LastValidChunk   string
	Usage            map[string]any
	ResponseID       string
	Model            string
	ScanErr          error
	HandledError     bool
}

type streamToolCallState struct {
	OutputIndex int
	ItemID      string
	CallID      string
	Name        string
	Arguments   strings.Builder
}

type responsesStreamState struct {
	writer     io.Writer
	flush      func()
	fullText   strings.Builder
	reasoning  strings.Builder
	model      string
	responseID string

	messageItemID   string
	reasoningItemID string
	messageStarted  bool
	reasoningStarted bool
	messageIndex    int
	reasoningIndex  int
	nextOutputIndex int

	toolStates map[int]*streamToolCallState
	toolOrder  []int
	lastUsage  map[string]any
}

func WriteResponsesStream(writer io.Writer, reader io.Reader, flush func()) ResponsesStreamResult {
	state := newResponsesStreamState(writer, flush)
	result := ResponsesStreamResult{
		ResponseID: state.responseID,
		Model:      state.model,
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	rawJSONLines := make([]string, 0, 1)
	writeStreamStart(state)

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
			rawJSONLines = append(rawJSONLines, line)
		} else {
			continue
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			result.SawDone = true
			break
		}
		if result.FirstValidChunk == "" {
			result.FirstValidChunk = truncateForStreamResult(data, 512)
		}
		result.LastValidChunk = truncateForStreamResult(data, 512)
		result.ChunkCount++
		if len(result.ChunkSamples) < 12 {
			result.ChunkSamples = append(result.ChunkSamples, truncateForStreamResult(data, 512))
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if len(result.ChunkSamples) < 12 {
				result.ChunkSamples = append(result.ChunkSamples, "unmarshal_error:"+truncateForStreamResult(err.Error(), 200))
			}
			continue
		}
		state.observeChunk(chunk)
		hadTextDelta := false
		for _, event := range openaitarget.ChatStreamEvents(chunk) {
			switch event.Type {
			case compatir.StreamEventUsage:
				if event.Usage != nil {
					if usage, ok := ResponsesUsage(*event.Usage); ok {
						state.lastUsage = MergeResponsesUsage(state.lastUsage, usage)
					}
				}
			case compatir.StreamEventContentDelta:
				switch event.Delta.Type {
				case compatir.BlockReasoning:
					if event.Delta.Reasoning == nil {
						continue
					}
					state.writeReasoningDelta(event.Delta.Reasoning.Text)
				case compatir.BlockToolCall:
					if event.Delta.ToolCall == nil {
						continue
					}
					state.writeToolCallDelta(event.Delta)
				case compatir.BlockText:
					if event.Delta.Text == "" {
						continue
					}
					hadTextDelta = true
					result.SawContentDelta = true
					state.writeTextDelta(event.Delta.Text)
				}
			}
		}
		if !hadTextDelta {
			fallback := openaitarget.ChatResponse(chunk)
			if text := responseOutputText(fallback.Output); text != "" {
				result.SawContentDelta = true
				state.writeTextDelta(text)
			}
		}
	}

	if state.fullText.Len() == 0 && len(rawJSONLines) == 1 {
		var full map[string]any
		if err := json.Unmarshal([]byte(rawJSONLines[0]), &full); err == nil {
			state.observeChunk(full)
			fullResp := openaitarget.ChatResponse(full)
			if state.fullText.Len() == 0 {
				if text := responseOutputText(fullResp.Output); text != "" {
					result.SawContentDelta = true
					state.writeTextDelta(text)
				}
			}
			if len(state.toolOrder) == 0 {
				state.addFullResponseToolCalls(fullResp.Output)
			}
		}
	}

	result.ScanErr = scanner.Err()
	if result.ScanErr != nil && result.ChunkCount == 0 {
		writeSSE(state.writer, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "upstream_stream_error",
				"message": "upstream stream parse failed before first chunk: " + result.ScanErr.Error(),
			},
		})
		_, _ = io.WriteString(state.writer, "data: [DONE]\n\n")
		state.flushNow()
		result.HandledError = true
		return result
	}

	state.writeDoneEvents()
	result.ExtractedTextLen = state.fullText.Len()
	result.ReasoningLen = state.reasoning.Len()
	result.Usage = state.lastUsage
	result.ResponseID = state.responseID
	result.Model = state.model
	return result
}

func newResponsesStreamState(writer io.Writer, flush func()) *responsesStreamState {
	return &responsesStreamState{
		writer:          writer,
		flush:           flush,
		model:           "unknown",
		responseID:      fmt.Sprintf("resp_%d", time.Now().UnixNano()),
		messageItemID:   fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		reasoningItemID: fmt.Sprintf("rs_%d", time.Now().UnixNano()),
		messageIndex:    -1,
		reasoningIndex:  -1,
		toolStates:      map[int]*streamToolCallState{},
		toolOrder:       make([]int, 0, 2),
	}
}

func writeStreamStart(state *responsesStreamState) {
	writeSSE(state.writer, map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     state.responseID,
			"object": "response",
			"status": "in_progress",
			"model":  state.model,
			"output": []any{},
		},
	})
	writeSSE(state.writer, map[string]any{
		"type": "response.in_progress",
		"response": map[string]any{
			"id":     state.responseID,
			"object": "response",
			"status": "in_progress",
			"model":  state.model,
			"output": []any{},
		},
	})
	state.flushNow()
}

func (s *responsesStreamState) observeChunk(chunk map[string]any) {
	if model := stringValue(chunk["model"]); model != "" {
		s.model = model
	}
	if id := stringValue(chunk["id"]); id != "" {
		s.responseID = id
	}
}

func (s *responsesStreamState) startMessage() {
	if s.messageStarted {
		return
	}
	s.messageStarted = true
	s.messageIndex = s.nextOutputIndex
	s.nextOutputIndex++
	writeSSE(s.writer, map[string]any{
		"type":         "response.output_item.added",
		"output_index": s.messageIndex,
		"item": map[string]any{
			"id":      s.messageItemID,
			"type":    "message",
			"status":  "in_progress",
			"role":    "assistant",
			"content": []any{},
		},
	})
	writeSSE(s.writer, map[string]any{
		"type":          "response.content_part.added",
		"item_id":       s.messageItemID,
		"output_index":  s.messageIndex,
		"content_index": 0,
		"part": map[string]any{
			"type": "output_text",
			"text": "",
		},
	})
}

func (s *responsesStreamState) startReasoning() {
	if s.reasoningStarted {
		return
	}
	s.reasoningStarted = true
	s.reasoningIndex = s.nextOutputIndex
	s.nextOutputIndex++
	writeSSE(s.writer, map[string]any{
		"type":         "response.output_item.added",
		"output_index": s.reasoningIndex,
		"item": map[string]any{
			"id":      s.reasoningItemID,
			"type":    "reasoning",
			"summary": []any{},
		},
	})
}

func (s *responsesStreamState) writeReasoningDelta(delta string) {
	s.startReasoning()
	s.reasoning.WriteString(delta)
	writeSSE(s.writer, map[string]any{
		"type":          "response.reasoning_summary_text.delta",
		"item_id":       s.reasoningItemID,
		"output_index":  s.reasoningIndex,
		"summary_index": 0,
		"delta":         delta,
	})
	s.flushNow()
}

func (s *responsesStreamState) writeTextDelta(delta string) {
	s.startMessage()
	s.fullText.WriteString(delta)
	writeSSE(s.writer, map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       s.messageItemID,
		"delta":         delta,
		"output_index":  s.messageIndex,
		"content_index": 0,
		"logprobs":      []any{},
	})
	s.flushNow()
}

func (s *responsesStreamState) writeToolCallDelta(block compatir.ContentBlock) {
	call := block.ToolCall
	idx := streamToolIndex(block.Raw, len(s.toolOrder))
	st, ok := s.toolStates[idx]
	if !ok {
		st = &streamToolCallState{
			OutputIndex: s.nextOutputIndex,
			ItemID:      fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), idx),
		}
		s.nextOutputIndex++
		s.toolStates[idx] = st
		s.toolOrder = append(s.toolOrder, idx)
	}
	if call.ID != "" {
		st.CallID = call.ID
	}
	if st.CallID == "" {
		st.CallID = st.ItemID
	}
	if call.Name != "" {
		st.Name = call.Name
	}
	startArgs := st.Arguments.Len() == 0
	if call.Arguments != "" {
		st.Arguments.WriteString(call.Arguments)
	}
	if startArgs || call.Name != "" || call.ID != "" {
		writeSSE(s.writer, map[string]any{
			"type":         "response.output_item.added",
			"output_index": st.OutputIndex,
			"item": map[string]any{
				"id":        st.ItemID,
				"type":      "function_call",
				"call_id":   st.CallID,
				"name":      st.Name,
				"arguments": st.Arguments.String(),
				"status":    "in_progress",
			},
		})
	}
	if call.Arguments != "" {
		writeSSE(s.writer, map[string]any{
			"type":         "response.function_call_arguments.delta",
			"item_id":      st.ItemID,
			"output_index": st.OutputIndex,
			"delta":        call.Arguments,
		})
	}
	s.flushNow()
}

func (s *responsesStreamState) addFullResponseToolCalls(blocks []compatir.ContentBlock) {
	for _, block := range blocks {
		if block.Type != compatir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		idx := len(s.toolOrder)
		call := block.ToolCall
		itemID := call.ID
		if itemID == "" {
			itemID = fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), idx)
		}
		st := &streamToolCallState{
			OutputIndex: s.nextOutputIndex,
			ItemID:      itemID,
			CallID:      itemID,
			Name:        call.Name,
		}
		s.nextOutputIndex++
		st.Arguments.WriteString(call.Arguments)
		s.toolStates[idx] = st
		s.toolOrder = append(s.toolOrder, idx)
	}
}

func (s *responsesStreamState) writeDoneEvents() {
	text := s.fullText.String()
	if s.reasoningStarted {
		reasoningText := s.reasoning.String()
		writeSSE(s.writer, map[string]any{
			"type":          "response.reasoning_summary_text.done",
			"item_id":       s.reasoningItemID,
			"output_index":  s.reasoningIndex,
			"summary_index": 0,
			"text":          reasoningText,
		})
		writeSSE(s.writer, map[string]any{
			"type":         "response.output_item.done",
			"output_index": s.reasoningIndex,
			"item": map[string]any{
				"id":      s.reasoningItemID,
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": reasoningText}},
			},
		})
	}
	for _, idx := range s.toolOrder {
		st := s.toolStates[idx]
		if st == nil {
			continue
		}
		if st.CallID == "" {
			st.CallID = st.ItemID
		}
		args := st.Arguments.String()
		if args == "" {
			args = "{}"
		}
		writeSSE(s.writer, map[string]any{
			"type":         "response.function_call_arguments.done",
			"item_id":      st.ItemID,
			"output_index": st.OutputIndex,
			"arguments":    args,
		})
		writeSSE(s.writer, map[string]any{
			"type":         "response.output_item.done",
			"output_index": st.OutputIndex,
			"item": map[string]any{
				"id":        st.ItemID,
				"type":      "function_call",
				"call_id":   st.CallID,
				"name":      st.Name,
				"arguments": args,
				"status":    "completed",
			},
		})
	}
	if s.messageStarted {
		writeSSE(s.writer, map[string]any{
			"type":          "response.output_text.done",
			"item_id":       s.messageItemID,
			"text":          text,
			"output_index":  s.messageIndex,
			"content_index": 0,
			"logprobs":      []any{},
		})
		writeSSE(s.writer, map[string]any{
			"type":          "response.content_part.done",
			"item_id":       s.messageItemID,
			"output_index":  s.messageIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": text,
			},
		})
		writeSSE(s.writer, map[string]any{
			"type":         "response.output_item.done",
			"output_index": s.messageIndex,
			"item": map[string]any{
				"id":     s.messageItemID,
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []map[string]any{
					{
						"type": "output_text",
						"text": text,
					},
				},
			},
		})
	}
	writeSSE(s.writer, map[string]any{
		"type": "response.completed",
		"response": s.completedResponse(),
	})
	_, _ = io.WriteString(s.writer, "data: [DONE]\n\n")
	s.flushNow()
}

func (s *responsesStreamState) completedResponse() map[string]any {
	text := s.fullText.String()
	outputItems := make([]map[string]any, 0, 2+len(s.toolOrder))
	if s.reasoningStarted {
		outputItems = append(outputItems, map[string]any{
			"id":      s.reasoningItemID,
			"type":    "reasoning",
			"summary": []map[string]any{{"type": "summary_text", "text": s.reasoning.String()}},
		})
	}
	for _, idx := range s.toolOrder {
		st := s.toolStates[idx]
		if st == nil {
			continue
		}
		callID := st.CallID
		if callID == "" {
			callID = st.ItemID
		}
		args := st.Arguments.String()
		if args == "" {
			args = "{}"
		}
		outputItems = append(outputItems, map[string]any{
			"id":        st.ItemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      st.Name,
			"arguments": args,
			"status":    "completed",
		})
	}
	if s.messageStarted {
		outputItems = append(outputItems, map[string]any{
			"id":     s.messageItemID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": text,
				},
			},
		})
	}
	resp := map[string]any{
		"id":          s.responseID,
		"object":      "response",
		"status":      "completed",
		"model":       s.model,
		"output_text": text,
		"output":      outputItems,
	}
	if len(s.lastUsage) > 0 {
		resp["usage"] = s.lastUsage
	}
	return resp
}

func (s *responsesStreamState) flushNow() {
	if s.flush != nil {
		s.flush()
	}
}

func streamToolIndex(raw map[string]any, fallback int) int {
	if idx, ok := intValue(raw["index"]); ok {
		return idx
	}
	return fallback
}

func writeSSE(w io.Writer, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}

func truncateForStreamResult(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
