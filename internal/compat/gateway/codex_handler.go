package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"spark/internal/compat/client/codex"
	"spark/internal/compat/policy"
	openai_chat_target "spark/internal/compat/target/openai_chat"
	"spark/internal/usage"
)

const (
	ResponsesModeChatCompletionsOnly = "chat_completions_only"
	ResponsesModePreferResponses     = "prefer_responses"
)

type CodexResponsesHandler struct {
	Mode          string
	Route         Route
	UpstreamBase  string
	Logf          func(format string, args ...any)
	Warnf         func(summary string)
	PostResponses func(ctx context.Context, req map[string]any) (*http.Response, error)
	Executor      ChatExecutor
	PrepareChat   ChatRequestPreparer
}

func (h CodexResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Logf("request method=%s path=%s content_type=%q content_encoding=%q user_agent=%q",
		r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"), r.Header.Get("User-Agent"))

	if r.Method != http.MethodPost {
		WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req, rawBody, err := DecodeJSONRequest(r)
	if err != nil {
		h.Logf("raw incoming body bytes=%d", len(rawBody))
		h.Logf("decode request failed: %v", err)
		h.Warnf("request decode failed")
		WriteJSONError(w, http.StatusBadRequest, "invalid json (adapter request decode failed: "+err.Error()+")")
		return
	}
	h.Logf("raw incoming body bytes=%d", len(rawBody))
	h.Logf("decoded responses request structure=%s", structureJSONForLog(req))

	if h.Mode == ResponsesModePreferResponses && h.PostResponses != nil {
		upResp, err := h.PostResponses(r.Context(), req)
		if err != nil {
			h.Logf("upstream responses request failed: %v", err)
			h.Warnf("upstream request failed")
			WriteJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
			return
		}
		if upResp.StatusCode < 400 {
			h.Logf("route=request->responses_passthrough status=%d", upResp.StatusCode)
			defer upResp.Body.Close()
			ForwardResponsesPassthrough(w, upResp, h.Logf)
			return
		}
		errBody, _ := io.ReadAll(upResp.Body)
		_ = upResp.Body.Close()
		if !ShouldFallbackFromResponses(upResp.StatusCode, errBody) {
			h.Warnf(fmt.Sprintf("forward responses upstream status %d", upResp.StatusCode))
			WriteUpstreamErrorAsJSON(w, &http.Response{
				StatusCode: upResp.StatusCode,
				Body:       io.NopCloser(bytes.NewReader(errBody)),
				Header:     upResp.Header,
			})
			return
		}
		h.Logf("responses passthrough fallback triggered status=%d body_bytes=%d", upResp.StatusCode, len(errBody))
		h.Logf("route=request->chat_fallback reason=responses_not_supported status=%d", upResp.StatusCode)
	}

	selection, err := SelectRoute(h.Route)
	if err != nil {
		h.Logf("route selection failed: %v", err)
		WriteJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	selection.Translator = h.translatorForSelection(selection, req)
	h.logDroppedReasoningControls(req)

	chatReq, upResp, err := ExecuteTranslatedChat(r.Context(), req, selection.Translator, h.Executor, h.PrepareChat)
	if err != nil {
		var perr PipelineError
		if errors.As(err, &perr) && perr.Stage == PipelineStageTranslate {
			h.Logf("request translate failed: %v", perr.Err)
			WriteJSONError(w, http.StatusBadRequest, "invalid request")
			return
		}
		h.Logf("upstream request failed: %v", err)
		h.Warnf("upstream request failed")
		WriteJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	h.Logf("mapped chat request(initial) structure=%s", structureJSONForLog(chatReq))
	h.Logf("route=request->chat_completions status=%d", upResp.StatusCode)
	defer upResp.Body.Close()

	stream, _ := req["stream"].(bool)
	if stream {
		ForwardCodexStream(w, upResp, h.Warnf, h.Logf)
		return
	}
	ForwardCodexNonStream(w, upResp, h.Warnf, h.Logf)
}

func (h CodexResponsesHandler) translatorForSelection(selection RouteSelection, req map[string]any) RequestTranslator {
	if selection.Route.Client == ClientCodexResponses && selection.Route.Target == TargetOpenAIChat {
		model, _ := req["model"].(string)
		return CodexResponsesTranslator{
			Reasoning: policy.OpenAIChatReasoningPolicy(h.UpstreamBase, model),
		}
	}
	return selection.Translator
}

func (h CodexResponsesHandler) logDroppedReasoningControls(req map[string]any) {
	irReq := codex.ResponsesInbound(req)
	reasoning := policy.OpenAIChatReasoningPolicy(h.UpstreamBase, irReq.Model)
	_, dropped := reasoning.ChatReasoningControls(irReq.Generation.Reasoning)
	if len(dropped) == 0 {
		return
	}
	h.Logf("reasoning controls degraded target=%s model=%s dropped=%s", TargetOpenAIChat, irReq.Model, strings.Join(dropped, ","))
}

func DecodeJSONRequest(r *http.Request) (map[string]any, string, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	rawBody := string(data)
	var req map[string]any
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rawBody, err
	}
	if req == nil {
		req = map[string]any{}
	}
	return req, rawBody, nil
}

func ForwardResponsesPassthrough(w http.ResponseWriter, upResp *http.Response, logf func(format string, args ...any)) {
	copyResponseHeaders(w.Header(), upResp.Header)
	w.WriteHeader(upResp.StatusCode)
	flusher, _ := w.(http.Flusher)
	contentType := strings.ToLower(upResp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		reader := bufio.NewReader(upResp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = w.Write(line)
				if flusher != nil {
					flusher.Flush()
				}
			}
			if err != nil {
				if err == io.EOF {
					return
				}
				if logf != nil {
					logf("responses passthrough stream read failed: %v", err)
				}
				return
			}
		}
	}
	if _, err := io.Copy(w, upResp.Body); err != nil && logf != nil {
		logf("responses passthrough copy failed: %v", err)
	}
}

func ForwardCodexNonStream(w http.ResponseWriter, upResp *http.Response, warnf func(string), logf func(string, ...any)) {
	if upResp.StatusCode >= 400 {
		callWarnf(warnf, fmt.Sprintf("forward non-stream upstream status %d", upResp.StatusCode))
		WriteUpstreamErrorAsJSON(w, upResp)
		return
	}
	rawBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		callWarnf(warnf, "failed to read upstream non-stream body")
		WriteJSONError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	var chatResp map[string]any
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&chatResp); err != nil {
		callLogf(logf, "upstream non-stream invalid json bytes=%d", len(rawBody))
		callWarnf(warnf, "invalid upstream non-stream JSON")
		WriteJSONError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	callLogf(logf, "upstream non-stream response structure=%s", structureJSONForLog(chatResp))

	irResp := openai_chat_target.ChatResponse(chatResp)
	usage.RecordIR(irResp.Usage, irResp.Model, false, time.Now().UTC())
	out := codex.ResponsesClientResponse(irResp)
	text := stringValue(out["output_text"])
	callLogf(logf, "non-stream extracted text length=%d", len(text))
	model := stringValue(out["model"])
	if model == "" {
		model = "unknown"
	}
	id := stringValue(out["id"])
	if id == "" {
		id = "unknown"
	}
	if usage := mapValue(out["usage"]); len(usage) > 0 {
		callLogf(logf, "non-stream usage present response_id=%s model=%s %s", id, model, formatUsageForLog(usage))
	} else {
		callLogf(logf, "non-stream usage missing response_id=%s model=%s", id, model)
		callWarnf(warnf, "upstream non-stream response missing token usage")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func ForwardCodexStream(w http.ResponseWriter, upResp *http.Response, warnf func(string), logf func(string, ...any)) {
	if upResp.StatusCode >= 400 {
		callWarnf(warnf, fmt.Sprintf("forward stream upstream status %d", upResp.StatusCode))
		WriteUpstreamErrorAsJSON(w, upResp)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteJSONError(w, http.StatusInternalServerError, "stream not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	callLogf(logf, "forward stream headers status=%d content_type=%q content_encoding=%q transfer_encoding=%v",
		upResp.StatusCode, upResp.Header.Get("Content-Type"), upResp.Header.Get("Content-Encoding"), upResp.TransferEncoding)

	result := WriteCodexResponsesStreamFromOpenAIChat(w, upResp.Body, flusher.Flush)
	if result.ScanErr != nil {
		callLogf(logf, "upstream stream scan error: %v", result.ScanErr)
	}
	callLogf(logf, "stream parse summary chunks=%d extracted_text_len=%d sample_count=%d",
		result.ChunkCount, result.ExtractedTextLen, len(result.ChunkSamples))
	callLogf(logf, "stream parse flags saw_done=%t saw_content_delta=%t reasoning_len=%d first_chunk_bytes=%d last_chunk_bytes=%d",
		result.SawDone, result.SawContentDelta, result.ReasoningLen, len(result.FirstValidChunk), len(result.LastValidChunk))
	if result.HandledError {
		callWarnf(warnf, "upstream stream failed before first chunk")
		return
	}
	if result.ExtractedTextLen == 0 {
		callWarnf(warnf, "stream response extracted empty text")
	}
	if len(result.Usage) > 0 {
		callLogf(logf, "stream usage present response_id=%s model=%s %s", result.ResponseID, result.Model, formatUsageForLog(result.Usage))
	} else {
		callLogf(logf, "stream usage missing response_id=%s model=%s chunks=%d saw_done=%t", result.ResponseID, result.Model, result.ChunkCount, result.SawDone)
		callWarnf(warnf, "upstream stream completed without token usage")
	}
}

func ShouldFallbackFromResponses(status int, data []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType, http.StatusNotImplemented:
		return true
	}
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(data)))
	if msg == "" {
		return false
	}
	unsupportedSignals := []string{
		"unknown parameter",
		"unknown field",
		"unexpected field",
		"unrecognized field",
	}
	hasUnsupportedSignal := false
	for _, s := range unsupportedSignals {
		if strings.Contains(msg, s) {
			hasUnsupportedSignal = true
			break
		}
	}
	if hasUnsupportedSignal {
		if strings.Contains(msg, "input") || strings.Contains(msg, "instructions") || strings.Contains(msg, "max_output_tokens") {
			return true
		}
	}
	if strings.Contains(msg, "responses") && (strings.Contains(msg, "not support") || strings.Contains(msg, "unsupported")) {
		return true
	}
	return false
}

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func formatUsageForLog(usage map[string]any) string {
	input := intFromAny(usage["input_tokens"])
	if input == 0 {
		input = intFromAny(usage["prompt_tokens"])
	}
	output := intFromAny(usage["output_tokens"])
	if output == 0 {
		output = intFromAny(usage["completion_tokens"])
	}
	total := intFromAny(usage["total_tokens"])
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}
	cached := intFromAny(usage["cached_input_tokens"])
	if cached == 0 {
		cached = intFromAny(usage["cache_read_input_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(usage["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["prompt_tokens_details"])["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["input_tokens_details"])["cached_tokens"])
	}
	cacheCreation := intFromAny(usage["cache_creation_input_tokens"])
	reasoning := intFromAny(usage["reasoning_output_tokens"])
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(usage["output_tokens_details"])["reasoning_tokens"])
	}
	return fmt.Sprintf("usage input=%d output=%d total=%d cached=%d cache_creation=%d reasoning=%d", input, output, total, cached, cacheCreation, reasoning)
}

func callLogf(logf func(string, ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}

func callWarnf(warnf func(string), summary string) {
	if warnf != nil {
		warnf(summary)
	}
}
