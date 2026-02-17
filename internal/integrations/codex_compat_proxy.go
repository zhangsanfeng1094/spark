package integrations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type responsesCompatProxy struct {
	server       *http.Server
	listener     net.Listener
	baseURL      string
	upstreamBase string
	upstreamKey  string
	client       *http.Client
	quietStderr  bool
	logFile      io.WriteCloser
	logMu        sync.Mutex
	logPath      string
}

func startResponsesCompatProxy(upstreamBase, upstreamKey string, quietStderr bool) (*responsesCompatProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &responsesCompatProxy{
		listener:     ln,
		baseURL:      "http://" + ln.Addr().String() + "/v1",
		upstreamBase: strings.TrimRight(upstreamBase, "/"),
		upstreamKey:  upstreamKey,
		client:       newStreamingHTTPClient(),
		quietStderr:  quietStderr,
	}
	logFile, logPath, err := openCompatLogFile()
	if err != nil {
		return nil, err
	}
	p.logFile = logFile
	p.logPath = logPath
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", p.handleResponses)
	p.server = &http.Server{Handler: mux}

	go func() {
		_ = p.server.Serve(ln)
	}()
	return p, nil
}

func (p *responsesCompatProxy) BaseURL() string {
	return p.baseURL
}

func (p *responsesCompatProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return err
}

func (p *responsesCompatProxy) LogPath() string {
	return p.logPath
}

func (p *responsesCompatProxy) logf(format string, args ...any) {
	line := fmt.Sprintf("[compat] "+format, args...)
	p.logMu.Lock()
	defer p.logMu.Unlock()
	if p.logFile != nil {
		_, _ = fmt.Fprintf(p.logFile, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

func (p *responsesCompatProxy) warnf(summary string) {
	if p.quietStderr {
		return
	}
	fmt.Fprintf(os.Stderr, "[compat] %s (details: %s)\n", summary, p.logPath)
}

func (p *responsesCompatProxy) handleResponses(w http.ResponseWriter, r *http.Request) {
	p.logf("request method=%s path=%s content_type=%q content_encoding=%q user_agent=%q",
		r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"), r.Header.Get("User-Agent"))

	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req, rawBody, err := decodeResponsesRequest(r)
	if err != nil {
		p.logf("raw incoming body=%s", rawBody)
		p.logf("decode request failed: %v", err)
		p.warnf("request decode failed")
		writeJSONError(w, http.StatusBadRequest, "invalid json (adapter request decode failed: "+err.Error()+")")
		return
	}
	p.logf("raw incoming body=%s", rawBody)
	p.logf("decoded responses request=%s", mustJSONForLog(req))

	reqTranslator := newResponsesRequestTranslator()
	executor := newCodexChatExecutor(p)
	chatReq, upResp, err := executeTranslatedChat(r.Context(), req, reqTranslator, executor)
	if err != nil {
		var perr pipelineError
		if errors.As(err, &perr) && perr.stage == pipelineStageTranslate {
			p.logf("request translate failed: %v", perr.err)
			writeJSONError(w, http.StatusBadRequest, "invalid request")
			return
		}
		p.logf("upstream request failed: %v", err)
		p.warnf("upstream request failed")
		writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	p.logf("mapped chat request(initial)=%s", mustJSONForLog(chatReq))
	defer upResp.Body.Close()

	stream, _ := req["stream"].(bool)
	writer := newCodexResponseWriter(p)
	writer.Write(w, upResp, stream)
}

func (p *responsesCompatProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.upstreamBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept-Encoding", "identity")
	if p.upstreamKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+p.upstreamKey)
	}
	p.logf("upstream POST %s payload=%s", p.upstreamBase+"/chat/completions", truncateForLog(string(body), 16*1024))
	return p.client.Do(upReq)
}

func shouldRetryWithMinimalChatReq(status int, data []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(data)))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "invalid json") {
		return true
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return false
	}
	if errObj, ok := decoded["error"].(map[string]any); ok {
		em := strings.ToLower(stringValue(errObj["message"]))
		return strings.Contains(em, "invalid json")
	}
	return false
}

func minimalChatCompletionsRequest(chatReq map[string]any) map[string]any {
	out := map[string]any{
		"model":    chatReq["model"],
		"messages": chatReq["messages"],
		"stream":   chatReq["stream"],
	}
	return out
}

func ultraMinimalChatCompletionsRequest(chatReq map[string]any) map[string]any {
	content := ""
	msgs, _ := chatReq["messages"].([]map[string]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		role := stringValue(msgs[i]["role"])
		if role != "user" && role != "system" {
			continue
		}
		c := normalizeMessageContent(msgs[i]["content"])
		if c != "" {
			content = c
			break
		}
	}
	out := map[string]any{
		"model": chatReq["model"],
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
		"stream": chatReq["stream"],
	}
	return out
}

func (p *responsesCompatProxy) forwardNonStream(w http.ResponseWriter, upResp *http.Response) {
	if upResp.StatusCode >= 400 {
		p.warnf(fmt.Sprintf("forward non-stream upstream status %d", upResp.StatusCode))
		writeUpstreamErrorAsJSON(w, upResp)
		return
	}
	rawBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		p.warnf("failed to read upstream non-stream body")
		writeJSONError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}
	p.logf("upstream non-stream raw body=%s", truncateForLog(string(rawBody), 16*1024))
	var chatResp map[string]any
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&chatResp); err != nil {
		p.warnf("invalid upstream non-stream JSON")
		writeJSONError(w, http.StatusBadGateway, "invalid upstream response")
		return
	}

	text := extractChatText(chatResp)
	p.logf("non-stream extracted text length=%d", len(text))
	model := stringValue(chatResp["model"])
	if model == "" {
		model = "unknown"
	}
	id := stringValue(chatResp["id"])
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}

	outputItems := make([]map[string]any, 0, 2)
	if text != "" {
		outputItems = append(outputItems, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": text,
				},
			},
		})
	}
	for _, tc := range extractChatToolCalls(chatResp) {
		outputItems = append(outputItems, map[string]any{
			"id":        tc.ID,
			"type":      "function_call",
			"call_id":   tc.CallID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
			"status":    "completed",
		})
	}
	out := map[string]any{
		"id":          id,
		"object":      "response",
		"status":      "completed",
		"model":       model,
		"output_text": text,
		"output":      outputItems,
	}
	if usage, ok := chatUsageToResponsesUsage(chatResp); ok {
		out["usage"] = usage
		p.logf("non-stream usage present response_id=%s model=%s %s", id, model, formatUsageForLog(usage))
	} else {
		p.logf("non-stream usage missing response_id=%s model=%s", id, model)
		p.warnf("upstream non-stream response missing token usage")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (p *responsesCompatProxy) forwardStream(w http.ResponseWriter, upResp *http.Response) {
	if upResp.StatusCode >= 400 {
		p.warnf(fmt.Sprintf("forward stream upstream status %d", upResp.StatusCode))
		writeUpstreamErrorAsJSON(w, upResp)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "stream not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	p.logf("forward stream headers status=%d content_type=%q content_encoding=%q transfer_encoding=%v",
		upResp.StatusCode, upResp.Header.Get("Content-Type"), upResp.Header.Get("Content-Encoding"), upResp.TransferEncoding)

	scanner := bufio.NewScanner(upResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var fullText strings.Builder
	var fullReasoning strings.Builder
	model := "unknown"
	respID := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	msgItemID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	reasoningItemID := fmt.Sprintf("rs_%d", time.Now().UnixNano())
	type streamToolCallState struct {
		OutputIndex int
		ItemID      string
		CallID      string
		Name        string
		Arguments   strings.Builder
	}
	toolStates := map[int]*streamToolCallState{}
	toolOrder := make([]int, 0, 2)
	var rawJSONLines []string
	var chunkSamples []string
	chunkCount := 0
	sawDone := false
	firstValidChunk := ""
	lastValidChunk := ""
	messageStarted := false
	reasoningStarted := false
	sawContentDelta := false
	reasoningOutputIndex := -1
	messageOutputIndex := -1
	nextOutputIndex := 0
	lastUsage := map[string]any{}

	startMessage := func() {
		if messageStarted {
			return
		}
		messageStarted = true
		messageOutputIndex = nextOutputIndex
		nextOutputIndex++
		writeSSE(w, map[string]any{
			"type":         "response.output_item.added",
			"output_index": messageOutputIndex,
			"item": map[string]any{
				"id":      msgItemID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []any{},
			},
		})
		writeSSE(w, map[string]any{
			"type":          "response.content_part.added",
			"item_id":       msgItemID,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": "",
			},
		})
	}

	writeSSE(w, map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     respID,
			"object": "response",
			"status": "in_progress",
			"model":  model,
			"output": []any{},
		},
	})
	writeSSE(w, map[string]any{
		"type": "response.in_progress",
		"response": map[string]any{
			"id":     respID,
			"object": "response",
			"status": "in_progress",
			"model":  model,
			"output": []any{},
		},
	})
	flusher.Flush()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		data := ""
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if strings.HasPrefix(line, "{") {
			// Some gateways stream raw NDJSON instead of SSE "data:" lines.
			data = line
			rawJSONLines = append(rawJSONLines, line)
		} else {
			continue
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			sawDone = true
			break
		}
		if firstValidChunk == "" {
			firstValidChunk = truncateForLog(data, 512)
		}
		lastValidChunk = truncateForLog(data, 512)
		chunkCount++
		if len(chunkSamples) < 12 {
			chunkSamples = append(chunkSamples, truncateForLog(data, 512))
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if len(chunkSamples) < 12 {
				chunkSamples = append(chunkSamples, "unmarshal_error:"+truncateForLog(err.Error(), 200))
			}
			continue
		}
		if usage, ok := chatUsageToResponsesUsage(chunk); ok {
			lastUsage = mergeResponsesUsage(lastUsage, usage)
		}
		if m := stringValue(chunk["model"]); m != "" {
			model = m
		}
		if i := stringValue(chunk["id"]); i != "" {
			respID = i
		}
		reasoningDelta := extractChatReasoningDelta(chunk)
		if reasoningDelta != "" {
			if !reasoningStarted {
				reasoningStarted = true
				reasoningOutputIndex = nextOutputIndex
				nextOutputIndex++
				writeSSE(w, map[string]any{
					"type":         "response.output_item.added",
					"output_index": reasoningOutputIndex,
					"item": map[string]any{
						"id":      reasoningItemID,
						"type":    "reasoning",
						"summary": []any{},
					},
				})
			}
			fullReasoning.WriteString(reasoningDelta)
			writeSSE(w, map[string]any{
				"type":          "response.reasoning_summary_text.delta",
				"item_id":       reasoningItemID,
				"output_index":  reasoningOutputIndex,
				"summary_index": 0,
				"delta":         reasoningDelta,
			})
			// Some OpenAI-compatible gateways emit only reasoning deltas without content deltas.
			// Mirror reasoning to output_text when no content delta has been observed.
			if !sawContentDelta {
				startMessage()
				fullText.WriteString(reasoningDelta)
				writeSSE(w, map[string]any{
					"type":          "response.output_text.delta",
					"item_id":       msgItemID,
					"delta":         reasoningDelta,
					"output_index":  messageOutputIndex,
					"content_index": 0,
					"logprobs":      []any{},
				})
			}
			flusher.Flush()
		}
		for _, td := range extractChatToolCallDeltas(chunk) {
			if td.Index < 0 {
				continue
			}
			st, ok := toolStates[td.Index]
			if !ok {
				st = &streamToolCallState{
					OutputIndex: nextOutputIndex,
					ItemID:      fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), td.Index),
				}
				nextOutputIndex++
				toolStates[td.Index] = st
				toolOrder = append(toolOrder, td.Index)
			}
			if td.CallID != "" {
				st.CallID = td.CallID
			}
			if st.CallID == "" {
				st.CallID = st.ItemID
			}
			if td.Name != "" {
				st.Name = td.Name
			}
			startArgs := st.Arguments.Len() == 0
			if td.ArgumentsDelta != "" {
				st.Arguments.WriteString(td.ArgumentsDelta)
			}
			if startArgs || td.Name != "" || td.CallID != "" {
				writeSSE(w, map[string]any{
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
			if td.ArgumentsDelta != "" {
				writeSSE(w, map[string]any{
					"type":         "response.function_call_arguments.delta",
					"item_id":      st.ItemID,
					"output_index": st.OutputIndex,
					"delta":        td.ArgumentsDelta,
				})
			}
			flusher.Flush()
		}
		delta := extractChatDelta(chunk)
		if delta == "" {
			// Some gateways emit full message chunks in stream mode.
			delta = extractChatText(chunk)
		}
		if delta == "" {
			continue
		}
		sawContentDelta = true
		startMessage()
		fullText.WriteString(delta)
		writeSSE(w, map[string]any{
			"type":          "response.output_text.delta",
			"item_id":       msgItemID,
			"delta":         delta,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"logprobs":      []any{},
		})
		flusher.Flush()
	}

	text := fullText.String()
	if text == "" && len(rawJSONLines) == 1 {
		var full map[string]any
		if err := json.Unmarshal([]byte(rawJSONLines[0]), &full); err == nil {
			text = extractChatText(full)
			if m := stringValue(full["model"]); m != "" {
				model = m
			}
			if i := stringValue(full["id"]); i != "" {
				respID = i
			}
			for _, tc := range extractChatToolCalls(full) {
				if len(toolStates) > 0 {
					break
				}
				idx := len(toolOrder)
				st := &streamToolCallState{
					OutputIndex: nextOutputIndex,
					ItemID:      tc.ID,
					CallID:      tc.CallID,
					Name:        tc.Name,
				}
				nextOutputIndex++
				st.Arguments.WriteString(tc.Arguments)
				toolStates[idx] = st
				toolOrder = append(toolOrder, idx)
			}
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		p.logf("upstream stream scan error: %v", scanErr)
	}
	p.logf("stream parse summary chunks=%d extracted_text_len=%d samples=%s",
		chunkCount, len(text), truncateForLog(strings.Join(chunkSamples, " || "), 16*1024))
	p.logf("stream parse flags saw_done=%t saw_content_delta=%t reasoning_len=%d first_chunk=%q last_chunk=%q",
		sawDone, sawContentDelta, len(fullReasoning.String()), firstValidChunk, lastValidChunk)
	if scanErr != nil && chunkCount == 0 {
		p.warnf("upstream stream failed before first chunk")
		writeSSE(w, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "upstream_stream_error",
				"message": "upstream stream parse failed before first chunk: " + scanErr.Error(),
			},
		})
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	if text == "" {
		p.warnf("stream response extracted empty text")
	}
	if reasoningStarted {
		reasoningText := fullReasoning.String()
		writeSSE(w, map[string]any{
			"type":          "response.reasoning_summary_text.done",
			"item_id":       reasoningItemID,
			"output_index":  reasoningOutputIndex,
			"summary_index": 0,
			"text":          reasoningText,
		})
		writeSSE(w, map[string]any{
			"type":         "response.output_item.done",
			"output_index": reasoningOutputIndex,
			"item": map[string]any{
				"id":      reasoningItemID,
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": reasoningText}},
			},
		})
	}
	for _, idx := range toolOrder {
		st := toolStates[idx]
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
		writeSSE(w, map[string]any{
			"type":         "response.function_call_arguments.done",
			"item_id":      st.ItemID,
			"output_index": st.OutputIndex,
			"arguments":    args,
		})
		writeSSE(w, map[string]any{
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
	if messageStarted {
		writeSSE(w, map[string]any{
			"type":          "response.output_text.done",
			"item_id":       msgItemID,
			"text":          text,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"logprobs":      []any{},
		})
		writeSSE(w, map[string]any{
			"type":          "response.content_part.done",
			"item_id":       msgItemID,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": text,
			},
		})
		writeSSE(w, map[string]any{
			"type":         "response.output_item.done",
			"output_index": messageOutputIndex,
			"item": map[string]any{
				"id":     msgItemID,
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
	outputItems := make([]map[string]any, 0, 2+len(toolOrder))
	if reasoningStarted {
		outputItems = append(outputItems, map[string]any{
			"id":      reasoningItemID,
			"type":    "reasoning",
			"summary": []map[string]any{{"type": "summary_text", "text": fullReasoning.String()}},
		})
	}
	for _, idx := range toolOrder {
		st := toolStates[idx]
		if st == nil {
			continue
		}
		args := st.Arguments.String()
		if args == "" {
			args = "{}"
		}
		callID := st.CallID
		if callID == "" {
			callID = st.ItemID
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
	if messageStarted {
		outputItems = append(outputItems, map[string]any{
			"id":     msgItemID,
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
		"id":          respID,
		"object":      "response",
		"status":      "completed",
		"model":       model,
		"output_text": text,
		"output":      outputItems,
	}
	if len(lastUsage) > 0 {
		resp["usage"] = lastUsage
		p.logf("stream usage present response_id=%s model=%s %s", respID, model, formatUsageForLog(lastUsage))
	} else {
		p.logf("stream usage missing response_id=%s model=%s chunks=%d saw_done=%t", respID, model, chunkCount, sawDone)
		p.warnf("upstream stream completed without token usage")
	}
	writeSSE(w, map[string]any{
		"type":     "response.completed",
		"response": resp,
	})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func chatUsageToResponsesUsage(payload map[string]any) (map[string]any, bool) {
	usage := mapValue(payload["usage"])
	if len(usage) == 0 {
		usage = mapValue(mapValue(payload["response"])["usage"])
	}
	if len(usage) == 0 {
		return nil, false
	}
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
	cached := intFromAny(usage["cached_tokens"])
	if cached == 0 {
		cached = intFromAny(usage["cached_input_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["prompt_tokens_details"])["cached_tokens"])
	}
	if cached == 0 {
		cached = intFromAny(mapValue(usage["input_tokens_details"])["cached_tokens"])
	}

	reasoning := intFromAny(usage["reasoning_tokens"])
	if reasoning == 0 {
		reasoning = intFromAny(usage["reasoning_output_tokens"])
	}
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(usage["completion_tokens_details"])["reasoning_tokens"])
	}
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(usage["output_tokens_details"])["reasoning_tokens"])
	}

	if input == 0 && output == 0 && total == 0 && cached == 0 && reasoning == 0 {
		return nil, false
	}

	// Codex expects all TokenUsage fields at top level (all required per Rust struct)
	out := map[string]any{
		"input_tokens":            input,
		"output_tokens":           output,
		"total_tokens":            total,
		"cached_input_tokens":     cached,
		"reasoning_output_tokens": reasoning,
	}
	// Keep *_details for backward compatibility
	if cached > 0 {
		out["input_tokens_details"] = map[string]any{
			"cached_tokens": cached,
		}
	}
	if reasoning > 0 {
		out["output_tokens_details"] = map[string]any{
			"reasoning_tokens": reasoning,
		}
	}
	return out, true
}

func mergeResponsesUsage(base map[string]any, incoming map[string]any) map[string]any {
	if len(base) == 0 {
		return incoming
	}
	// Always include all required TokenUsage fields (Codex expects them all)
	out := map[string]any{}

	// Start with base values, prefer non-zero incoming values
	out["input_tokens"] = intFromAny(base["input_tokens"])
	if v := intFromAny(incoming["input_tokens"]); v > 0 {
		out["input_tokens"] = v
	}

	out["output_tokens"] = intFromAny(base["output_tokens"])
	if v := intFromAny(incoming["output_tokens"]); v > 0 {
		out["output_tokens"] = v
	}

	out["total_tokens"] = intFromAny(base["total_tokens"])
	if v := intFromAny(incoming["total_tokens"]); v > 0 {
		out["total_tokens"] = v
	}

	out["cached_input_tokens"] = intFromAny(base["cached_input_tokens"])
	if v := intFromAny(incoming["cached_input_tokens"]); v > 0 {
		out["cached_input_tokens"] = v
	}
	if v := intFromAny(mapValue(incoming["input_tokens_details"])["cached_tokens"]); v > 0 {
		out["cached_input_tokens"] = v
		out["input_tokens_details"] = map[string]any{"cached_tokens": v}
	}

	out["reasoning_output_tokens"] = intFromAny(base["reasoning_output_tokens"])
	if v := intFromAny(incoming["reasoning_output_tokens"]); v > 0 {
		out["reasoning_output_tokens"] = v
	}
	if v := intFromAny(mapValue(incoming["output_tokens_details"])["reasoning_tokens"]); v > 0 {
		out["reasoning_output_tokens"] = v
		out["output_tokens_details"] = map[string]any{"reasoning_tokens": v}
	}

	return out
}
func formatUsageForLog(usage map[string]any) string {
	input := intFromAny(usage["input_tokens"])
	output := intFromAny(usage["output_tokens"])
	total := intFromAny(usage["total_tokens"])
	cached := intFromAny(usage["cached_input_tokens"])
	if cached == 0 {
		cached = intFromAny(mapValue(usage["input_tokens_details"])["cached_tokens"])
	}
	reasoning := intFromAny(usage["reasoning_output_tokens"])
	if reasoning == 0 {
		reasoning = intFromAny(mapValue(usage["output_tokens_details"])["reasoning_tokens"])
	}
	return fmt.Sprintf("usage input=%d output=%d total=%d cached=%d reasoning=%d", input, output, total, cached, reasoning)
}

func writeSSE(w io.Writer, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}
