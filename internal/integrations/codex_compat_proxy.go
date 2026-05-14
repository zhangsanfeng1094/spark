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

	openaiadapter "spark/internal/compat/codec/openai"
	openaitarget "spark/internal/compat/target/openai"
)

type responsesCompatProxy struct {
	server       *http.Server
	listener     net.Listener
	baseURL      string
	upstreamBase string
	upstreamKey  string
	mode         responsesProxyMode
	client       *http.Client
	quietStderr  bool
	logFile      io.WriteCloser
	logMu        sync.Mutex
	logPath      string
}

type responsesProxyMode string

const (
	responsesProxyModeChatCompletionsOnly responsesProxyMode = "chat_completions_only"
	responsesProxyModePreferResponses     responsesProxyMode = "prefer_responses"
)

func startResponsesCompatProxy(upstreamBase, upstreamKey string, quietStderr bool, mode responsesProxyMode) (*responsesCompatProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if mode == "" {
		mode = responsesProxyModeChatCompletionsOnly
	}
	p := &responsesCompatProxy{
		listener:     ln,
		baseURL:      "http://" + ln.Addr().String() + "/v1",
		upstreamBase: strings.TrimRight(upstreamBase, "/"),
		upstreamKey:  upstreamKey,
		mode:         mode,
		client:       newStreamingHTTPClient(),
		quietStderr:  quietStderr,
	}
	logFile, logPath, err := openCompatLogFile()
	if err != nil {
		return nil, err
	}
	p.logFile = logFile
	p.logPath = logPath
	p.logf("proxy started mode=%s upstream=%s listen=%s", p.mode, p.upstreamBase, p.baseURL)
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

	if p.mode == responsesProxyModePreferResponses {
		upResp, err := p.postResponses(r.Context(), req)
		if err != nil {
			p.logf("upstream responses request failed: %v", err)
			p.warnf("upstream request failed")
			writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
			return
		}
		if upResp.StatusCode < 400 {
			p.logf("route=request->responses_passthrough status=%d", upResp.StatusCode)
			defer upResp.Body.Close()
			p.forwardResponsesPassthrough(w, upResp)
			return
		}
		errBody, _ := io.ReadAll(upResp.Body)
		_ = upResp.Body.Close()
		if !shouldFallbackFromResponses(upResp.StatusCode, errBody) {
			p.warnf(fmt.Sprintf("forward responses upstream status %d", upResp.StatusCode))
			writeUpstreamErrorAsJSON(w, &http.Response{
				StatusCode: upResp.StatusCode,
				Body:       io.NopCloser(bytes.NewReader(errBody)),
				Header:     upResp.Header,
			})
			return
		}
		p.logf(
			"responses passthrough fallback triggered status=%d body=%s",
			upResp.StatusCode,
			truncateForLog(string(errBody), 16*1024),
		)
		p.logf("route=request->chat_fallback reason=responses_not_supported status=%d", upResp.StatusCode)
	}

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
	p.logf("route=request->chat_completions status=%d", upResp.StatusCode)
	defer upResp.Body.Close()

	stream, _ := req["stream"].(bool)
	writer := newCodexResponseWriter(p)
	writer.Write(w, upResp, stream)
}

func (p *responsesCompatProxy) postResponses(ctx context.Context, req map[string]any) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.upstreamBase+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept-Encoding", "identity")
	if p.upstreamKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+p.upstreamKey)
	}
	p.logf("upstream POST %s payload=%s", p.upstreamBase+"/responses", truncateForLog(string(body), 16*1024))
	return p.client.Do(upReq)
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

func shouldFallbackFromResponses(status int, data []byte) bool {
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

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func (p *responsesCompatProxy) forwardResponsesPassthrough(w http.ResponseWriter, upResp *http.Response) {
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
				p.logf("responses passthrough stream read failed: %v", err)
				return
			}
		}
	}
	if _, err := io.Copy(w, upResp.Body); err != nil {
		p.logf("responses passthrough copy failed: %v", err)
	}
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

	irResp := openaitarget.ChatResponse(chatResp)
	out := openaiadapter.ResponsesClientResponse(irResp)
	text := stringValue(out["output_text"])
	p.logf("non-stream extracted text length=%d", len(text))
	model := stringValue(out["model"])
	if model == "" {
		model = "unknown"
	}
	id := stringValue(out["id"])
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if usage := mapValue(out["usage"]); len(usage) > 0 {
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

	result := openaiadapter.WriteResponsesStream(w, upResp.Body, flusher.Flush)
	if result.ScanErr != nil {
		p.logf("upstream stream scan error: %v", result.ScanErr)
	}
	p.logf("stream parse summary chunks=%d extracted_text_len=%d samples=%s",
		result.ChunkCount, result.ExtractedTextLen, truncateForLog(strings.Join(result.ChunkSamples, " || "), 16*1024))
	p.logf("stream parse flags saw_done=%t saw_content_delta=%t reasoning_len=%d first_chunk=%q last_chunk=%q",
		result.SawDone, result.SawContentDelta, result.ReasoningLen, result.FirstValidChunk, result.LastValidChunk)
	if result.HandledError {
		p.warnf("upstream stream failed before first chunk")
		return
	}
	if result.ExtractedTextLen == 0 {
		p.warnf("stream response extracted empty text")
	}
	if len(result.Usage) > 0 {
		p.logf("stream usage present response_id=%s model=%s %s", result.ResponseID, result.Model, formatUsageForLog(result.Usage))
	} else {
		p.logf("stream usage missing response_id=%s model=%s chunks=%d saw_done=%t", result.ResponseID, result.Model, result.ChunkCount, result.SawDone)
		p.warnf("upstream stream completed without token usage")
	}
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
