package gemini

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

type toolCallAccum struct {
	id      string
	name    string
	args    strings.Builder
	emitted bool
}

// ForwardStream writes Gemini generateContent SSE chunks from a Bifrost chat stream.
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

	model := requestedModel
	responseID := ""
	var lastUsage *schemas.BifrostLLMUsage
	finishReason := ""
	tools := map[int]*toolCallAccum{}

	writeParts := func(parts []map[string]any, reason string, usage *schemas.BifrostLLMUsage) {
		if len(parts) == 0 && reason == "" && usage == nil {
			return
		}
		candidate := map[string]any{"index": 0}
		if len(parts) > 0 {
			candidate["content"] = map[string]any{
				"role":  "model",
				"parts": parts,
			}
		}
		if reason != "" {
			candidate["finishReason"] = reason
		}
		event := map[string]any{
			"candidates": []map[string]any{candidate},
		}
		if responseID != "" {
			event["responseId"] = responseID
		}
		if model != "" {
			event["modelVersion"] = model
		}
		if meta := geminiUsageMetadata(usage); meta != nil {
			event["usageMetadata"] = meta
		}
		writeGeminiSSE(w, flusher, event)
	}

	flushTools := func(force bool) {
		parts := make([]map[string]any, 0, len(tools))
		for _, state := range tools {
			if state == nil || state.emitted || state.name == "" {
				continue
			}
			raw := strings.TrimSpace(state.args.String())
			var parsed any
			if raw == "" {
				if !force {
					continue
				}
				parsed = map[string]any{}
			} else if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				if !force {
					continue
				}
				parsed = objectFromJSONString(raw)
			}
			state.emitted = true
			call := map[string]any{"name": state.name, "args": parsed}
			if state.id != "" {
				call["id"] = state.id
			}
			parts = append(parts, map[string]any{"functionCall": call})
		}
		if len(parts) > 0 {
			writeParts(parts, "", nil)
		}
	}

	for chunk := range streamChan {
		if chunk == nil {
			continue
		}
		if chunk.BifrostError != nil {
			writeGeminiSSE(w, flusher, geminiErrorBody(chunk.BifrostError))
			return
		}
		if chunk.BifrostChatResponse == nil {
			continue
		}
		cr := chunk.BifrostChatResponse
		if cr.Model != "" {
			model = cr.Model
		}
		if cr.ID != "" {
			responseID = cr.ID
		}
		if cr.Usage != nil {
			lastUsage = cr.Usage
		}
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

		parts := make([]map[string]any, 0, 2)
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			parts = append(parts, map[string]any{"text": *delta.Reasoning, "thought": true})
		}
		if delta.Content != nil && *delta.Content != "" {
			parts = append(parts, map[string]any{"text": *delta.Content})
		}
		if len(parts) > 0 {
			writeParts(parts, "", nil)
		}

		for _, tc := range delta.ToolCalls {
			state, exists := tools[int(tc.Index)]
			if !exists {
				state = &toolCallAccum{}
				tools[int(tc.Index)] = state
			}
			if tc.ID != nil && *tc.ID != "" {
				state.id = *tc.ID
			}
			if tc.Function.Name != nil && *tc.Function.Name != "" {
				state.name = *tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				state.args.WriteString(tc.Function.Arguments)
			}
		}
		flushTools(false)
	}

	flushTools(true)
	writeParts(nil, geminiFinishReason(finishReason), lastUsage)
	if onUsage != nil && lastUsage != nil {
		onUsage(lastUsage, model)
	}
}

func writeGeminiSSE(w http.ResponseWriter, flusher http.Flusher, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// writeGeminiStreamFromNonStream emits a complete generateContent payload as
// Gemini SSE so clients that always stream still work when the upstream chat
// completions stream is unavailable.
func takeFirstStreamChunk(ch <-chan *schemas.BifrostStreamChunk) (*schemas.BifrostStreamChunk, bool) {
	for chunk := range ch {
		if chunk != nil {
			return chunk, true
		}
	}
	return nil, false
}

func prependStreamChunk(first *schemas.BifrostStreamChunk, rest <-chan *schemas.BifrostStreamChunk) chan *schemas.BifrostStreamChunk {
	out := make(chan *schemas.BifrostStreamChunk, 8)
	go func() {
		defer close(out)
		if first != nil {
			out <- first
		}
		for chunk := range rest {
			out <- chunk
		}
	}()
	return out
}

func drainStream(ch <-chan *schemas.BifrostStreamChunk) {
	if ch == nil {
		return
	}
	go func() {
		for range ch {
		}
	}()
}

func writeGeminiStreamFromNonStream(w http.ResponseWriter, resp *schemas.BifrostChatResponse) {
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
	writeGeminiSSE(w, flusher, geminiNonStreamResponse(resp))
}
