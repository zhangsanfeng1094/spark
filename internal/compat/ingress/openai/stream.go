package openai

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/maximhq/bifrost/core/schemas"
)

// ForwardStream processes stream chunks from Bifrost and writes OpenAI SSE events.
func ForwardStream(w http.ResponseWriter, streamChan chan *schemas.BifrostStreamChunk, requestedModel string, onUsage func(usage *schemas.BifrostLLMUsage, model string)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for chunk := range streamChan {
		if chunk == nil {
			continue
		}

		if chunk.BifrostError != nil {
			errType := "api_error"
			if chunk.BifrostError.Type != nil && *chunk.BifrostError.Type != "" {
				errType = *chunk.BifrostError.Type
			}
			errPayload, _ := json.Marshal(map[string]any{
				"error": map[string]any{
					"message": chunk.BifrostError.GetErrorString(),
					"type":    errType,
				},
			})
			fmt.Fprintf(w, "data: %s\n\n", errPayload)
			flusher.Flush()
			break
		}

		if chunk.BifrostChatResponse == nil {
			continue
		}

		cr := chunk.BifrostChatResponse
		if cr.Usage != nil && onUsage != nil {
			model := cr.Model
			if model == "" {
				model = requestedModel
			}
			onUsage(cr.Usage, model)
		}

		data, err := json.Marshal(cr)
		if err == nil {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
