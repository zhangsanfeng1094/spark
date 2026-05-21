package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"spark/internal/compat/ir"
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
	Added       bool
}

type responsesStreamState struct {
	writer     io.Writer
	flush      func()
	fullText   strings.Builder
	reasoning  strings.Builder
	model      string
	responseID string

	messageItemID    string
	reasoningItemID  string
	messageStarted   bool
	reasoningStarted bool
	messageIndex     int
	reasoningIndex   int
	nextOutputIndex  int

	toolStates map[int]*streamToolCallState
	toolOrder  []int
	lastUsage  map[string]any
}

type ResponsesStreamWriter struct {
	state  *responsesStreamState
	result ResponsesStreamResult
}

func NewResponsesStreamWriter(writer io.Writer, flush func()) *ResponsesStreamWriter {
	state := newResponsesStreamState(writer, flush)
	result := ResponsesStreamResult{
		ResponseID: state.responseID,
		Model:      state.model,
	}
	writeStreamStart(state)
	return &ResponsesStreamWriter{state: state, result: result}
}

func (w *ResponsesStreamWriter) ObserveUpstreamChunk(raw map[string]any, sample string) {
	if w == nil {
		return
	}
	if w.result.FirstValidChunk == "" {
		w.result.FirstValidChunk = truncateForStreamResult(sample, 512)
	}
	w.result.LastValidChunk = truncateForStreamResult(sample, 512)
	w.result.ChunkCount++
	if len(w.result.ChunkSamples) < 12 {
		w.result.ChunkSamples = append(w.result.ChunkSamples, truncateForStreamResult(sample, 512))
	}
	w.state.observeChunk(raw)
}

func (w *ResponsesStreamWriter) WriteEvent(event ir.StreamEvent) bool {
	if w == nil {
		return false
	}
	switch event.Type {
	case ir.StreamEventUsage:
		if event.Usage != nil {
			if usage, ok := ResponsesUsage(*event.Usage); ok {
				w.state.lastUsage = MergeResponsesUsage(w.state.lastUsage, usage)
			}
		}
	case ir.StreamEventContentDelta:
		switch event.Delta.Type {
		case ir.BlockReasoning:
			if event.Delta.Reasoning == nil {
				return false
			}
			w.state.writeReasoningDelta(event.Delta.Reasoning.Text)
		case ir.BlockToolCall:
			if event.Delta.ToolCall == nil {
				return false
			}
			w.state.writeToolCallDelta(event.Delta)
		case ir.BlockText:
			if event.Delta.Text == "" {
				return false
			}
			w.result.SawContentDelta = true
			w.state.writeTextDelta(event.Delta.Text)
			return true
		}
	}
	return false
}

func (w *ResponsesStreamWriter) WriteResponseFallback(resp ir.Response) {
	if w == nil {
		return
	}
	if w.state.fullText.Len() == 0 {
		if text := responseOutputText(resp.Output); text != "" {
			w.result.SawContentDelta = true
			w.state.writeTextDelta(text)
		}
	}
	if len(w.state.toolOrder) == 0 {
		w.state.addFullResponseToolCalls(resp.Output)
	}
}

func (w *ResponsesStreamWriter) MarkDone() {
	if w != nil {
		w.result.SawDone = true
	}
}

func (w *ResponsesStreamWriter) WriteScanError(err error) ResponsesStreamResult {
	if w == nil {
		return ResponsesStreamResult{ScanErr: err}
	}
	w.result.ScanErr = err
	if err != nil && w.result.ChunkCount == 0 {
		writeSSE(w.state.writer, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "upstream_stream_error",
				"message": "upstream stream parse failed before first chunk: " + err.Error(),
			},
		})
		_, _ = io.WriteString(w.state.writer, "data: [DONE]\n\n")
		w.state.flushNow()
		w.result.HandledError = true
	}
	return w.result
}

func (w *ResponsesStreamWriter) Finish() ResponsesStreamResult {
	if w == nil {
		return ResponsesStreamResult{}
	}
	w.state.writeDoneEvents()
	w.result.ExtractedTextLen = w.state.fullText.Len()
	w.result.ReasoningLen = w.state.reasoning.Len()
	w.result.Usage = w.state.lastUsage
	w.result.ResponseID = w.state.responseID
	w.result.Model = w.state.model
	return w.result
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

func (s *responsesStreamState) writeToolCallDelta(block ir.ContentBlock) {
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
	if call.Arguments != "" {
		st.Arguments.WriteString(call.Arguments)
	}
	if !st.Added {
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
		st.Added = true
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

func (s *responsesStreamState) addFullResponseToolCalls(blocks []ir.ContentBlock) {
	for _, block := range blocks {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
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
		"type":     "response.completed",
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
