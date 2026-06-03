package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"spark/internal/compat/gateway"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

type ResponsesProxy struct {
	*compatProxyServer
	upstreamBase string
	upstreamKey  string
	mode         ResponsesProxyMode
}

type ResponsesProxyMode string

const (
	ResponsesProxyModeChatCompletionsOnly   ResponsesProxyMode = "chat_completions_only"
	ResponsesProxyModePreferResponses       ResponsesProxyMode = "prefer_responses"
	ResponsesProxyModeAnthropicMessagesOnly ResponsesProxyMode = "anthropic_messages_only"
)

func StartResponsesProxy(upstreamBase, upstreamKey string, quietStderr bool, mode ResponsesProxyMode, preferredModels ...string) (*ResponsesProxy, error) {
	if mode == "" {
		mode = ResponsesProxyModeChatCompletionsOnly
	}
	server, err := newCompatProxyServer(openCompatLogFile, "compat", quietStderr)
	if err != nil {
		return nil, err
	}
	preferredModel := ""
	if len(preferredModels) > 0 {
		preferredModel = strings.TrimSpace(preferredModels[0])
	}
	p := &ResponsesProxy{
		compatProxyServer: server,
		upstreamBase:      strings.TrimRight(upstreamBase, "/"),
		upstreamKey:       upstreamKey,
		mode:              mode,
	}
	p.restore = installUsageRecorder("codex", preferredModel, p.logf)
	p.handleFunc("/v1/responses", p.handleResponses)
	p.start()
	p.logf("proxy started mode=%s upstream=%s listen=%s", p.mode, p.upstreamBase, p.BaseURL())
	return p, nil
}

func (p *ResponsesProxy) BaseURL() string {
	return p.compatProxyServer.BaseURL() + "/v1"
}

func (p *ResponsesProxy) logf(format string, args ...any) {
	if p == nil || p.compatProxyServer == nil {
		return
	}
	p.compatProxyServer.logf(format, args...)
}

func (p *ResponsesProxy) warnf(summary string) {
	if p == nil || p.compatProxyServer == nil {
		return
	}
	p.compatProxyServer.warnf(summary)
}

func (p *ResponsesProxy) handleResponses(w http.ResponseWriter, r *http.Request) {
	executor := newCodexChatExecutor(p)
	if p.mode == ResponsesProxyModeAnthropicMessagesOnly {
		executor = newCodexAnthropicMessagesExecutor(p)
	}
	handler := gateway.NewCodexResponsesHandler(gateway.CodexResponsesOptions{
		Mode:          string(p.mode),
		UpstreamBase:  p.upstreamBase,
		Logf:          p.logf,
		Warnf:         p.warnf,
		PostResponses: p.postResponses,
		Executor:      executor,
	})
	handler.ServeHTTP(w, r)
}

func (p *ResponsesProxy) postResponses(ctx context.Context, req map[string]any) (*http.Response, error) {
	return p.postUpstreamJSON(ctx, p.upstreamBase, p.upstreamKey, "/responses", req, p.logf)
}

func (p *ResponsesProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	return p.postUpstreamJSON(ctx, p.upstreamBase, p.upstreamKey, "/chat/completions", chatReq, p.logf)
}

func (p *ResponsesProxy) postAnthropicMessages(ctx context.Context, req map[string]any) (*http.Response, error) {
	return p.postAnthropicMessagesJSON(ctx, p.upstreamBase, p.upstreamKey, req, p.logf)
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
		c := openai_chat_target.NormalizeMessageContent(msgs[i]["content"])
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

func (p *ResponsesProxy) forwardResponsesPassthrough(w http.ResponseWriter, upResp *http.Response) {
	gateway.ForwardResponsesPassthrough(w, upResp, p.logf)
}
