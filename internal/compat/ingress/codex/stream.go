package codex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

// ForwardStream processes stream chunks from Bifrost and writes SSE events to the Codex client.
func ForwardStream(w http.ResponseWriter, streamChan chan *schemas.BifrostStreamChunk, onUsage func(usage *schemas.BifrostLLMUsage, model string)) {
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

	state := schemas.AcquireChatToResponsesStreamState()
	defer schemas.ReleaseChatToResponsesStreamState(state)

	var lastUsage *schemas.BifrostLLMUsage
	var lastModel string
	var pendingCompleted *schemas.BifrostResponsesStreamResponse

	for chunk := range streamChan {
		if chunk == nil {
			continue
		}

		if chunk.BifrostError != nil {
			msg := chunk.BifrostError.GetErrorString()
			var code any
			if chunk.BifrostError.Error != nil {
				code = chunk.BifrostError.Error.Code
			}
			errEvent := map[string]any{
				"type": "error",
				"error": map[string]any{
					"message": msg,
					"code":    code,
				},
			}
			writeSSE(w, flusher, errEvent)
			break
		}

		if chunk.BifrostResponsesStreamResponse != nil {
			ensureResponsesEventStatus(chunk.BifrostResponsesStreamResponse)
			writeSSE(w, flusher, chunk.BifrostResponsesStreamResponse)
			if chunk.BifrostResponsesStreamResponse.Response != nil && chunk.BifrostResponsesStreamResponse.Response.Usage != nil {
				respUsage := chunk.BifrostResponsesStreamResponse.Response.Usage
				lastUsage = &schemas.BifrostLLMUsage{
					PromptTokens:     respUsage.InputTokens,
					CompletionTokens: respUsage.OutputTokens,
					TotalTokens:      respUsage.TotalTokens,
				}
			}
			continue
		}

		if chunk.BifrostChatResponse != nil {
			if chunk.BifrostChatResponse.Model != "" {
				lastModel = chunk.BifrostChatResponse.Model
			}
			if chunk.BifrostChatResponse.Usage != nil {
				lastUsage = chunk.BifrostChatResponse.Usage
			}

			events := chunk.BifrostChatResponse.ToBifrostResponsesStreamResponse(state)
			for _, ev := range events {
				if ev == nil {
					continue
				}
				if ev.Type == schemas.ResponsesStreamResponseTypeCompleted {
					pendingCompleted = ev
					continue
				}
				ensureResponsesEventStatus(ev)
				writeSSE(w, flusher, ev)
			}
		}
	}

	if pendingCompleted != nil {
		if lastUsage != nil && pendingCompleted.Response != nil {
			if pendingCompleted.Response.Usage == nil || pendingCompleted.Response.Usage.TotalTokens == 0 {
				pendingCompleted.Response.Usage = lastUsage.ToResponsesResponseUsage()
			}
		}
		ensureResponsesEventStatus(pendingCompleted)
		writeSSE(w, flusher, pendingCompleted)
	} else if state.HasEmittedCreated && !state.HasEmittedCompleted {
		finalEvents := state.FinalizeStream(lastUsage)
		for _, ev := range finalEvents {
			if ev != nil {
				ensureResponsesEventStatus(ev)
				writeSSE(w, flusher, ev)
			}
		}
	}

	// Terminate stream
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	if onUsage != nil && lastUsage != nil {
		onUsage(lastUsage, lastModel)
	}
}

// ensureResponsesEventStatus makes all response and item payloads consumable by
// strict OpenAI Responses clients. Some providers (and older Bifrost conversion
// paths) omit status on lifecycle or output-item events; Grok Build rejects the
// complete stream at the first omission.
func ensureResponsesEventStatus(event *schemas.BifrostResponsesStreamResponse) {
	if event == nil {
		return
	}

	responseStatus := responsesEventStatus(event.Type)
	if event.Response != nil {
		if event.Response.Status == nil {
			event.Response.Status = &responseStatus
		}
		if event.Response.Object == "" {
			event.Response.Object = "response"
		}
		if event.Response.Output == nil {
			event.Response.Output = []schemas.ResponsesMessage{}
		}
		ensureResponsesUsageDetails(event.Response.Usage)
		outputStatus := *event.Response.Status
		for i := range event.Response.Output {
			ensureResponsesMessageDefaults(&event.Response.Output[i], outputStatus)
		}
	}

	if event.Item != nil {
		itemStatus := responseStatus
		if event.Type == schemas.ResponsesStreamResponseTypeOutputItemDone {
			itemStatus = "completed"
		}
		ensureResponsesMessageDefaults(event.Item, itemStatus)
	}
	if responsesEventNeedsSummaryIndex(event.Type) && event.SummaryIndex == nil {
		summaryIndex := 0
		event.SummaryIndex = &summaryIndex
	}
}

func responsesEventNeedsSummaryIndex(eventType schemas.ResponsesStreamResponseType) bool {
	switch eventType {
	case schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded,
		schemas.ResponsesStreamResponseTypeReasoningSummaryPartDone,
		schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
		schemas.ResponsesStreamResponseTypeReasoningSummaryTextDone:
		return true
	default:
		return false
	}
}

func ensureResponsesMessageDefaults(item *schemas.ResponsesMessage, status string) {
	if item == nil {
		return
	}
	if item.Status == nil {
		item.Status = &status
	}
	if item.Type == nil || *item.Type != schemas.ResponsesMessageTypeReasoning {
		return
	}
	if item.ResponsesReasoning == nil {
		item.ResponsesReasoning = &schemas.ResponsesReasoning{}
	}
	if item.ResponsesReasoning.Summary == nil {
		item.ResponsesReasoning.Summary = []schemas.ResponsesReasoningSummary{}
	}
	if len(item.ResponsesReasoning.Summary) == 0 && item.Content != nil && len(item.Content.ContentBlocks) > 0 {
		var sb strings.Builder
		for _, b := range item.Content.ContentBlocks {
			if b.Type == schemas.ResponsesOutputMessageContentTypeReasoning && b.Text != nil {
				sb.WriteString(*b.Text)
			}
		}
		if sb.Len() > 0 && item.Status != nil && *item.Status == "completed" {
			item.ResponsesReasoning.Summary = []schemas.ResponsesReasoningSummary{
				{
					Type: schemas.ResponsesReasoningContentBlockTypeSummaryText,
					Text: sb.String(),
				},
			}
		}
	}
}

func ensureResponsesUsageDetails(usage *schemas.ResponsesResponseUsage) {
	if usage == nil {
		return
	}
	if usage.InputTokensDetails == nil {
		usage.InputTokensDetails = &schemas.ResponsesResponseInputTokens{}
	}
	if usage.OutputTokensDetails == nil {
		usage.OutputTokensDetails = &schemas.ResponsesResponseOutputTokens{}
	}
}

func responsesEventStatus(eventType schemas.ResponsesStreamResponseType) string {
	status := "in_progress"
	switch eventType {
	case schemas.ResponsesStreamResponseTypeCompleted:
		status = "completed"
	case schemas.ResponsesStreamResponseTypeIncomplete:
		status = "incomplete"
	case schemas.ResponsesStreamResponseTypeFailed:
		status = "failed"
	}
	return status
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
