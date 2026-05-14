package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	anthropicadapter "spark/internal/compat/codec/anthropic"
)

type anthropicCompatProxy struct {
	server              *http.Server
	listener            net.Listener
	baseURL             string
	upstreamBase        string
	upstreamKey         string
	preferredModel      string
	client              *http.Client
	logFile             io.WriteCloser
	logMu               sync.Mutex
	logPath             string
	reasoningMu         sync.Mutex
	reasoningByToolCall map[string]string
}

func startAnthropicCompatProxy(upstreamBase, upstreamKey, preferredModel string) (*anthropicCompatProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	logFile, logPath, err := openAnthropicCompatLogFile()
	if err != nil {
		return nil, err
	}
	p := &anthropicCompatProxy{
		listener:       ln,
		baseURL:        "http://" + ln.Addr().String(),
		upstreamBase:   strings.TrimRight(upstreamBase, "/"),
		upstreamKey:    upstreamKey,
		preferredModel: strings.TrimSpace(preferredModel),
		client:         newStreamingHTTPClient(),
		logFile:        logFile,
		logPath:        logPath,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", p.handleMessages)
	mux.HandleFunc("/messages", p.handleMessages)
	p.server = &http.Server{Handler: mux}
	go func() {
		_ = p.server.Serve(ln)
	}()
	return p, nil
}

func (p *anthropicCompatProxy) BaseURL() string { return p.baseURL }

func (p *anthropicCompatProxy) LogPath() string { return p.logPath }

func (p *anthropicCompatProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return err
}

func (p *anthropicCompatProxy) logf(format string, args ...any) {
	line := fmt.Sprintf("[anthropic-compat] "+format, args...)
	p.logMu.Lock()
	defer p.logMu.Unlock()
	if p.logFile != nil {
		_, _ = fmt.Fprintf(p.logFile, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

func (p *anthropicCompatProxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, rawBody, err := decodeResponsesRequest(r)
	if err != nil {
		p.logf("decode request failed: %v raw=%s", err, rawBody)
		writeAnthropicError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	p.logf("incoming request=%s", mustJSONForLog(req))

	reqTranslator := newAnthropicRequestTranslator()
	chatReq, err := reqTranslator.ToChat(req)
	if err != nil {
		p.logf("request translate failed: %v", err)
		writeAnthropicError(w, http.StatusBadRequest, "invalid request")
		return
	}
	stream := boolValue(req["stream"])
	chatReq["stream"] = stream
	if p.preferredModel != "" {
		incomingModel := stringValue(chatReq["model"])
		if incomingModel != p.preferredModel {
			p.logf("override chat model incoming=%q preferred=%q", incomingModel, p.preferredModel)
		}
		chatReq["model"] = p.preferredModel
	}
	p.applyReasoningContent(chatReq)
	p.logf("mapped chat request=%s", mustJSONForLog(chatReq))
	summary := chatReasoningSummary(chatReq)
	p.logf("mapped chat summary messages=%d assistant_reasoning_messages=%d assistant_reasoning_chars=%d assistant_tool_messages=%d top_level_thinking=%t stream=%t",
		summary.MessageCount,
		summary.AssistantReasoningMessages,
		summary.AssistantReasoningChars,
		summary.AssistantToolMessages,
		summary.TopLevelThinking,
		stream,
	)
	executor := newAnthropicChatExecutor(p)
	resp, err := executor.Do(r.Context(), chatReq)
	if err != nil {
		p.logf("upstream request failed: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		p.logf("upstream status=%d body=%s", resp.StatusCode, truncateForLog(string(data), 16*1024))
		writeAnthropicError(w, resp.StatusCode, string(bytes.TrimSpace(data)))
		return
	}
	writer := newAnthropicResponseWriter(p)
	requestedModel := stringValue(chatReq["model"])
	if stream {
		writer.WriteStream(w, resp.Body, requestedModel)
		return
	}
	writer.WriteNonStream(w, resp, requestedModel)
}

func (p *anthropicCompatProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	doPost := func(payload map[string]any) (*http.Response, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		url := p.upstreamBase + "/chat/completions"
		upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("Accept-Encoding", "identity")
		if p.upstreamKey != "" {
			upReq.Header.Set("Authorization", "Bearer "+p.upstreamKey)
		}
		return p.client.Do(upReq)
	}

	resp, err := doPost(chatReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}

	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	bodyText := string(data)
	lowerBody := strings.ToLower(bodyText)
	model := stringValue(chatReq["model"])

	if model == "" || !strings.Contains(lowerBody, "unknown model") {
		resp.Body = io.NopCloser(bytes.NewReader(data))
		resp.ContentLength = int64(len(data))
		return resp, nil
	}

	retryModel := retryUnknownModelVariant(model)
	if retryModel == "" || retryModel == model {
		resp.Body = io.NopCloser(bytes.NewReader(data))
		resp.ContentLength = int64(len(data))
		return resp, nil
	}

	p.logf("unknown model from upstream, retrying with variant original=%q retry=%q", model, retryModel)
	retryReq := make(map[string]any, len(chatReq))
	for k, v := range chatReq {
		retryReq[k] = v
	}
	retryReq["model"] = retryModel
	return doPost(retryReq)
}

func retryUnknownModelVariant(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return ""
	}
	// Claude Code may lower-case model IDs; some gateways are case-sensitive.
	if strings.ToLower(m) != m {
		return ""
	}
	if idx := strings.Index(m, "/"); idx > 0 && idx < len(m)-1 {
		return m[:idx+1] + strings.ToUpper(m[idx+1:])
	}
	return strings.ToUpper(m)
}

func (p *anthropicCompatProxy) writeAnthropicStreamFromMessage(w http.ResponseWriter, msg map[string]any) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, http.StatusInternalServerError, "stream not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	usage := mapValue(msg["usage"])
	startMessage := map[string]any{
		"id":      stringValue(msg["id"]),
		"type":    "message",
		"role":    "assistant",
		"model":   stringValue(msg["model"]),
		"content": []any{},
		"usage": map[string]any{
			"input_tokens":  intFromAny(usage["input_tokens"]),
			"output_tokens": 0,
		},
	}
	writeAnthropicSSE(w, "message_start", map[string]any{
		"type":    "message_start",
		"message": startMessage,
	})
	flusher.Flush()

	content, _ := msg["content"].([]any)
	for i, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType := stringValue(block["type"])
		switch blockType {
		case "text":
			writeAnthropicSSE(w, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": i,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			text := stringValue(block["text"])
			if text != "" {
				writeAnthropicSSE(w, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": i,
					"delta": map[string]any{
						"type": "text_delta",
						"text": text,
					},
				})
			}
			writeAnthropicSSE(w, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": i,
			})
		case "tool_use":
			writeAnthropicSSE(w, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": i,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    stringValue(block["id"]),
					"name":  stringValue(block["name"]),
					"input": mapValue(block["input"]),
				},
			})
			if data, err := json.Marshal(block["input"]); err == nil && len(data) > 0 {
				writeAnthropicSSE(w, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": i,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": string(data),
					},
				})
			}
			writeAnthropicSSE(w, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": i,
			})
		}
		flusher.Flush()
	}

	writeAnthropicSSE(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   msg["stop_reason"],
			"stop_sequence": msg["stop_sequence"],
		},
		"usage": map[string]any{
			"input_tokens":  intFromAny(usage["input_tokens"]),
			"output_tokens": intFromAny(usage["output_tokens"]),
		},
	})
	writeAnthropicSSE(w, "message_stop", map[string]any{
		"type": "message_stop",
	})
	flusher.Flush()
}

func writeAnthropicSSE(w io.Writer, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "event: "+event+"\n")
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}

func (p *anthropicCompatProxy) forwardAnthropicStream(w http.ResponseWriter, upBody io.Reader, requestedModel string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, http.StatusInternalServerError, "stream not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	result := anthropicadapter.WriteMessagesStream(w, upBody, requestedModel, flusher.Flush)
	p.logf("stream parse flags chunks=%d saw_done=%t message_started=%t reasoning_len=%d tool_call_ids=%d first_chunk=%q last_chunk=%q",
		result.ChunkCount, result.SawDone, result.MessageStarted, len(result.ReasoningText), len(result.ToolCallIDs), result.FirstValidChunk, result.LastValidChunk)
	if result.EmptyStream {
		writeAnthropicError(w, http.StatusBadGateway, "empty upstream stream")
		return
	}
	if result.ReasoningText != "" && len(result.ToolCallIDs) > 0 {
		p.rememberReasoningForToolCallIDs(result.ToolCallIDs, result.ReasoningText)
	}
}
