package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	anthropicadapter "spark/internal/compat/client/anthropic_messages"
	reasoningfeature "spark/internal/compat/gateway/features/reasoning"
	"spark/internal/compat/httpjson"
	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
	openai_chat_target "spark/internal/compat/target/openai_chat"
	"spark/internal/usage"
)

type anthropicMessagesHandler struct {
	preferredModel      string
	upstreamBase        string
	reasoningCache      *reasoningfeature.ReasoningCache
	logf                func(format string, args ...any)
	postChatCompletions func(ctx context.Context, chatReq map[string]any) (*http.Response, error)
}

type AnthropicMessagesOptions struct {
	PreferredModel      string
	UpstreamBase        string
	ReasoningCache      *reasoningfeature.ReasoningCache
	Logf                func(format string, args ...any)
	PostChatCompletions func(ctx context.Context, chatReq map[string]any) (*http.Response, error)
}

func NewAnthropicMessagesToOpenAIChatHandler(opts AnthropicMessagesOptions) anthropicMessagesHandler {
	return anthropicMessagesHandler{
		preferredModel:      opts.PreferredModel,
		upstreamBase:        opts.UpstreamBase,
		reasoningCache:      opts.ReasoningCache,
		logf:                opts.Logf,
		postChatCompletions: opts.PostChatCompletions,
	}
}

func (h anthropicMessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, rawBody, err := httpjson.DecodeRequest(r)
	if err != nil {
		callLogf(h.logf, "decode request failed: %v raw_bytes=%d", err, len(rawBody))
		writeAnthropicError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	logCompatStage(h.logf, "anthropic.request", req)

	irReq := anthropicadapter.MessagesInbound(req)
	logCompatStage(h.logf, "ir.request", irReq)
	targetModel := irReq.Model
	if h.preferredModel != "" {
		targetModel = h.preferredModel
	}
	chatReq := openai_chat_target.ChatOutbound{
		Reasoning: h.reasoningPolicy(targetModel),
	}.BuildRequest(irReq)
	stream := boolValue(req["stream"])
	chatReq["stream"] = stream
	if stream {
		ensureChatStreamUsageOption(chatReq)
	}
	if h.preferredModel != "" {
		incomingModel := stringValue(chatReq["model"])
		if incomingModel != h.preferredModel {
			callLogf(h.logf, "override chat model incoming=%q preferred=%q", incomingModel, h.preferredModel)
		}
		chatReq["model"] = h.preferredModel
	}
	h.applyReasoningContent(chatReq)
	logCompatStage(h.logf, "openai_chat.request", chatReq)

	if h.postChatCompletions == nil {
		writeAnthropicError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	resp, err := h.postChatCompletions(r.Context(), chatReq)
	if err != nil {
		callLogf(h.logf, "upstream request failed: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		callLogf(h.logf, "upstream status=%d body_bytes=%d", resp.StatusCode, len(data))
		writeAnthropicError(w, resp.StatusCode, string(bytes.TrimSpace(data)))
		return
	}
	requestedModel := stringValue(chatReq["model"])
	if stream {
		ForwardAnthropicMessagesStream(w, resp.Body, requestedModel, h.rememberReasoning, h.logf)
		return
	}
	forwardAnthropicMessagesNonStream(w, resp, requestedModel, h.rememberReasoning, h.logf)
}

func (h anthropicMessagesHandler) reasoningPolicy(model string) policy.ReasoningPolicy {
	if h.upstreamBase != "" {
		return policy.OpenAIChatReasoningPolicy(h.upstreamBase, model)
	}
	return policy.PreserveReasoningContent()
}

func (h anthropicMessagesHandler) applyReasoningContent(chatReq map[string]any) {
	reasoningfeature.ChatReasoningAdapter{
		UpstreamBase: h.upstreamBase,
		Cache:        h.reasoningCache,
	}.ApplyToChatRequest(chatReq)
}

func (h anthropicMessagesHandler) rememberReasoning(ids []string, reasoning string) {
	reasoningfeature.ChatReasoningAdapter{
		Cache: h.reasoningCache,
	}.RememberForToolCallIDs(ids, reasoning)
}

func ensureChatStreamUsageOption(chatReq map[string]any) {
	streamOptions, _ := chatReq["stream_options"].(map[string]any)
	if streamOptions == nil {
		streamOptions = map[string]any{}
		chatReq["stream_options"] = streamOptions
	}
	streamOptions["include_usage"] = true
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
		writeAnthropicError(w, http.StatusInternalServerError, "stream not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	result := anthropicadapter.WriteMessagesStream(w, upBody, requestedModel, flusher.Flush)
	callLogf(logf, "stream parse flags chunks=%d saw_done=%t message_started=%t reasoning_len=%d tool_call_ids=%d first_chunk_bytes=%d last_chunk_bytes=%d",
		result.ChunkCount, result.SawDone, result.MessageStarted, len(result.ReasoningText), len(result.ToolCallIDs), len(result.FirstValidChunk), len(result.LastValidChunk))
	if len(result.Usage) > 0 {
		callLogf(logf, "middleware stage=anthropic.stream %s raw_usage=%s", formatUsageForLog(result.Usage), structureJSONForLog(result.Usage))
	} else {
		callLogf(logf, "middleware stage=anthropic.stream usage missing")
	}
	if result.EmptyStream {
		writeAnthropicError(w, http.StatusBadGateway, "empty upstream stream")
		return
	}
	if result.ReasoningText != "" && len(result.ToolCallIDs) > 0 && remember != nil {
		remember(result.ToolCallIDs, result.ReasoningText)
	}
}

func forwardAnthropicMessagesNonStream(
	w http.ResponseWriter,
	upResp *http.Response,
	requestedModel string,
	remember func(ids []string, reasoning string),
	logf func(format string, args ...any),
) {
	data, err := io.ReadAll(upResp.Body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	var chatResp map[string]any
	if err := json.Unmarshal(data, &chatResp); err != nil {
		callLogf(logf, "upstream invalid json bytes=%d", len(data))
		writeAnthropicError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	logCompatStage(logf, "openai_chat.response", chatResp)
	irResp := openai_chat_target.ChatResponse(chatResp)
	logCompatStage(logf, "ir.response", irResp)
	logCompatUsage(logf, "ir.response", irResp.Usage)
	usage.RecordIR(irResp.Usage, irResp.Model, false, time.Now().UTC())
	if remember != nil {
		remember(toolCallIDsFromBlocks(irResp.Output), reasoningTextFromBlocks(irResp.Output))
	}
	msg := anthropicadapter.MessagesClientResponse(irResp, requestedModel)
	logCompatStage(logf, "anthropic.response", msg)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

func writeAnthropicError(w http.ResponseWriter, status int, msg string) {
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
