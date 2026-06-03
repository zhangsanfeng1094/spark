package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"spark/internal/compat/gateway/bridge"
	"spark/internal/compat/gateway/core"
	"spark/internal/compat/httpjson"
)

func ForwardCodexNonStream(w http.ResponseWriter, upResp *http.Response, warnf func(string), logf func(string, ...any)) {
	selection, _ := bridge.SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	ForwardCodexNonStreamWithWriter(w, upResp, selection.NonStream, warnf, logf)
}

func ForwardCodexNonStreamWithWriter(w http.ResponseWriter, upResp *http.Response, writer bridge.NonStreamWriter, warnf func(string), logf func(string, ...any)) {
	if upResp.StatusCode >= 400 {
		callWarnf(warnf, fmt.Sprintf("forward non-stream upstream status %d", upResp.StatusCode))
		httpjson.WriteUpstreamError(w, upResp)
		return
	}
	if writer == nil {
		selection, _ := bridge.SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
		writer = selection.NonStream
	}
	rawBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		callWarnf(warnf, "failed to read upstream non-stream body")
		httpjson.WriteError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	var chatResp map[string]any
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&chatResp); err != nil {
		callLogf(logf, "upstream non-stream invalid json bytes=%d", len(rawBody))
		callWarnf(warnf, "invalid upstream non-stream JSON")
		httpjson.WriteError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	callLogf(logf, "upstream non-stream response structure=%s", structureJSONForLog(chatResp))

	out := writer(chatResp)
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
	selection, _ := bridge.SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
	ForwardCodexStreamWithWriter(w, upResp, selection.Stream, warnf, logf)
}

func ForwardCodexStreamWithWriter(w http.ResponseWriter, upResp *http.Response, writer bridge.StreamWriter, warnf func(string), logf func(string, ...any)) {
	if upResp.StatusCode >= 400 {
		callWarnf(warnf, fmt.Sprintf("forward stream upstream status %d", upResp.StatusCode))
		httpjson.WriteUpstreamError(w, upResp)
		return
	}
	if writer == nil {
		selection, _ := bridge.SelectRoute(core.Route{Client: core.ClientCodexResponses, Target: core.TargetOpenAIChat})
		writer = selection.Stream
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpjson.WriteError(w, http.StatusInternalServerError, "stream not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	callLogf(logf, "forward stream headers status=%d content_type=%q content_encoding=%q transfer_encoding=%v",
		upResp.StatusCode, upResp.Header.Get("Content-Type"), upResp.Header.Get("Content-Encoding"), upResp.TransferEncoding)

	result := writer(w, upResp.Body, flusher.Flush)
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
