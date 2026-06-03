package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"spark/internal/compat/gateway"
	reasoningfeature "spark/internal/compat/gateway/features/reasoning"
)

type AnthropicProxy struct {
	*compatProxyServer
	upstreamBase   string
	upstreamKey    string
	preferredModel string
	reasoningCache reasoningfeature.ReasoningCache
}

func StartAnthropicProxy(upstreamBase, upstreamKey, preferredModel string) (*AnthropicProxy, error) {
	server, err := newCompatProxyServer(openAnthropicCompatLogFile, "anthropic-compat", false)
	if err != nil {
		return nil, err
	}
	p := &AnthropicProxy{
		compatProxyServer: server,
		upstreamBase:      strings.TrimRight(upstreamBase, "/"),
		upstreamKey:       upstreamKey,
		preferredModel:    strings.TrimSpace(preferredModel),
	}
	p.restore = installUsageRecorder("claude", p.preferredModel, p.logf)
	p.handleFunc("/v1/messages", p.handleMessages)
	p.handleFunc("/messages", p.handleMessages)
	p.start()
	return p, nil
}

func (p *AnthropicProxy) logf(format string, args ...any) {
	if p == nil || p.compatProxyServer == nil {
		return
	}
	p.compatProxyServer.logf(format, args...)
}

func (p *AnthropicProxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	handler := gateway.NewAnthropicMessagesToOpenAIChatHandler(gateway.AnthropicMessagesOptions{
		PreferredModel:      p.preferredModel,
		UpstreamBase:        p.upstreamBase,
		ReasoningCache:      &p.reasoningCache,
		Logf:                p.logf,
		PostChatCompletions: p.postChatCompletions,
	})
	handler.ServeHTTP(w, r)
}

func (p *AnthropicProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
	doPost := func(payload map[string]any) (*http.Response, error) {
		return p.postUpstreamJSON(ctx, p.upstreamBase, p.upstreamKey, "/chat/completions", payload, p.logf)
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
