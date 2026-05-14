package integrations

import (
	"encoding/json"
	"io"
	"net/http"

	anthropicadapter "spark/internal/compat/codec/anthropic"
	openaitarget "spark/internal/compat/target/openai"
	"spark/internal/compatir"
)

type codexResponseWriter struct {
	proxy *responsesCompatProxy
}

func newCodexResponseWriter(proxy *responsesCompatProxy) codexResponseWriter {
	return codexResponseWriter{proxy: proxy}
}

func (w codexResponseWriter) Write(wr http.ResponseWriter, upResp *http.Response, stream bool) {
	if stream {
		w.proxy.forwardStream(wr, upResp)
		return
	}
	w.proxy.forwardNonStream(wr, upResp)
}

type anthropicResponseWriter struct {
	proxy *anthropicCompatProxy
}

func newAnthropicResponseWriter(proxy *anthropicCompatProxy) anthropicResponseWriter {
	return anthropicResponseWriter{proxy: proxy}
}

func (w anthropicResponseWriter) WriteStream(wr http.ResponseWriter, upBody io.Reader, requestedModel string) {
	w.proxy.forwardAnthropicStream(wr, upBody, requestedModel)
}

func (w anthropicResponseWriter) WriteNonStream(wr http.ResponseWriter, upResp *http.Response, requestedModel string) {
	data, err := io.ReadAll(upResp.Body)
	if err != nil {
		writeAnthropicError(wr, http.StatusBadGateway, "invalid upstream response")
		return
	}
	var chatResp map[string]any
	if err := json.Unmarshal(data, &chatResp); err != nil {
		w.proxy.logf("upstream invalid json=%s", truncateForLog(string(data), 16*1024))
		writeAnthropicError(wr, http.StatusBadGateway, "invalid upstream response")
		return
	}
	w.proxy.logf("upstream response=%s", mustJSONForLog(chatResp))
	irResp := openaitarget.ChatResponse(chatResp)
	w.proxy.rememberReasoningForToolCallIDs(toolCallIDsFromBlocks(irResp.Output), reasoningTextFromBlocks(irResp.Output))
	msg := anthropicadapter.MessagesClientResponse(irResp, requestedModel)
	wr.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(wr).Encode(msg)
}

func toolCallIDsFromBlocks(blocks []compatir.ContentBlock) []string {
	ids := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != compatir.BlockToolCall || block.ToolCall == nil {
			continue
		}
		if block.ToolCall.ID != "" {
			ids = append(ids, block.ToolCall.ID)
		}
	}
	return ids
}

func reasoningTextFromBlocks(blocks []compatir.ContentBlock) string {
	for _, block := range blocks {
		if block.Type == compatir.BlockReasoning && block.Reasoning != nil {
			return block.Reasoning.Text
		}
	}
	return ""
}
