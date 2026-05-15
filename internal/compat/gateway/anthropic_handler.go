package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	anthropicadapter "spark/internal/compat/client/anthropic_messages"
	"spark/internal/compat/ir"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

type AnthropicMessagesHandler struct {
	PreferredModel        string
	Logf                  func(format string, args ...any)
	PostChatCompletions   func(ctx context.Context, chatReq map[string]any) (*http.Response, error)
	ApplyReasoningContent func(chatReq map[string]any)
	RememberReasoning     func(ids []string, reasoning string)
}

func (h AnthropicMessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAnthropicError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, rawBody, err := DecodeJSONRequest(r)
	if err != nil {
		callLogf(h.Logf, "decode request failed: %v raw=%s", err, rawBody)
		WriteAnthropicError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	callLogf(h.Logf, "incoming request=%s", mustJSONForLog(req))

	reqTranslator := AnthropicMessagesTranslator{}
	chatReq, err := reqTranslator.ToChat(req)
	if err != nil {
		callLogf(h.Logf, "request translate failed: %v", err)
		WriteAnthropicError(w, http.StatusBadRequest, "invalid request")
		return
	}
	stream := boolValue(req["stream"])
	chatReq["stream"] = stream
	if h.PreferredModel != "" {
		incomingModel := stringValue(chatReq["model"])
		if incomingModel != h.PreferredModel {
			callLogf(h.Logf, "override chat model incoming=%q preferred=%q", incomingModel, h.PreferredModel)
		}
		chatReq["model"] = h.PreferredModel
	}
	if h.ApplyReasoningContent != nil {
		h.ApplyReasoningContent(chatReq)
	}
	callLogf(h.Logf, "mapped chat request=%s", mustJSONForLog(chatReq))

	if h.PostChatCompletions == nil {
		WriteAnthropicError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	resp, err := h.PostChatCompletions(r.Context(), chatReq)
	if err != nil {
		callLogf(h.Logf, "upstream request failed: %v", err)
		WriteAnthropicError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		callLogf(h.Logf, "upstream status=%d body=%s", resp.StatusCode, truncateForLog(string(data), 16*1024))
		WriteAnthropicError(w, resp.StatusCode, string(bytes.TrimSpace(data)))
		return
	}
	requestedModel := stringValue(chatReq["model"])
	if stream {
		ForwardAnthropicMessagesStream(w, resp.Body, requestedModel, h.RememberReasoning, h.Logf)
		return
	}
	ForwardAnthropicMessagesNonStream(w, resp, requestedModel, h.RememberReasoning, h.Logf)
}

func ForwardAnthropicMessagesStream(
	w http.ResponseWriter,
	upBody io.Reader,
	requestedModel string,
	remember func(ids []string, reasoning string),
	logf func(format string, args ...any),
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteAnthropicError(w, http.StatusInternalServerError, "stream not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	result := anthropicadapter.WriteMessagesStream(w, upBody, requestedModel, flusher.Flush)
	callLogf(logf, "stream parse flags chunks=%d saw_done=%t message_started=%t reasoning_len=%d tool_call_ids=%d first_chunk=%q last_chunk=%q",
		result.ChunkCount, result.SawDone, result.MessageStarted, len(result.ReasoningText), len(result.ToolCallIDs), result.FirstValidChunk, result.LastValidChunk)
	if result.EmptyStream {
		WriteAnthropicError(w, http.StatusBadGateway, "empty upstream stream")
		return
	}
	if result.ReasoningText != "" && len(result.ToolCallIDs) > 0 && remember != nil {
		remember(result.ToolCallIDs, result.ReasoningText)
	}
}

func ForwardAnthropicMessagesNonStream(
	w http.ResponseWriter,
	upResp *http.Response,
	requestedModel string,
	remember func(ids []string, reasoning string),
	logf func(format string, args ...any),
) {
	data, err := io.ReadAll(upResp.Body)
	if err != nil {
		WriteAnthropicError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	var chatResp map[string]any
	if err := json.Unmarshal(data, &chatResp); err != nil {
		callLogf(logf, "upstream invalid json=%s", truncateForLog(string(data), 16*1024))
		WriteAnthropicError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	callLogf(logf, "upstream response=%s", mustJSONForLog(chatResp))
	irResp := openai_chat_target.ChatResponse(chatResp)
	if remember != nil {
		remember(toolCallIDsFromBlocks(irResp.Output), reasoningTextFromBlocks(irResp.Output))
	}
	msg := anthropicadapter.MessagesClientResponse(irResp, requestedModel)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

func WriteAnthropicError(w http.ResponseWriter, status int, msg string) {
	if msg == "" {
		msg = http.StatusText(status)
	}
	body := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": msg,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func toolCallIDsFromBlocks(blocks []ir.ContentBlock) []string {
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil {
			continue
		}
		if block.ToolCall.ID != "" {
			ids = append(ids, block.ToolCall.ID)
		}
	}
	return ids
}

func reasoningTextFromBlocks(blocks []ir.ContentBlock) string {
	for _, block := range blocks {
		if block.Type == ir.BlockReasoning && block.Reasoning != nil {
			return block.Reasoning.Text
		}
	}
	return ""
}
