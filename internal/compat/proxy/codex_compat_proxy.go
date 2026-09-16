package proxy

import (
	"context"
	"strings"

	"spark/internal/compat/engine"
	codexingress "spark/internal/compat/ingress/codex"
	"spark/internal/config"
)

type ResponsesProxy struct {
	*compatProxyServer
	engine       *engine.Engine
	profile      *config.Profile
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

	profile := &config.Profile{
		OpenAIBaseURL: strings.TrimRight(upstreamBase, "/"),
		APIKey:        upstreamKey,
		DefaultModel:  preferredModel,
	}
	if mode == ResponsesProxyModeAnthropicMessagesOnly {
		profile.OpenAIAPIType = config.OpenAIAPITypeAnthropicMessages
		profile.AnthropicBaseURL = strings.TrimRight(upstreamBase, "/")
	}

	eng, err := engine.New(context.Background(), nil, engine.NewSparkLLMPlugin(server.logf))
	if err != nil {
		_ = server.Close()
		return nil, err
	}

	provider, cleanedBase, _ := engine.MapProfileToProvider(profile)
	_ = eng.ConfigureProvider(provider, cleanedBase, upstreamKey)

	p := &ResponsesProxy{
		compatProxyServer: server,
		engine:            eng,
		profile:           profile,
		upstreamBase:      strings.TrimRight(upstreamBase, "/"),
		upstreamKey:       upstreamKey,
		mode:              mode,
	}

	handler := codexingress.NewHandler(eng, profile, p.logf)
	handler.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		sessionID := ""
		if md, ok := req["client_metadata"].(map[string]any); ok {
			if sid, ok := md["x-codex-window-id"].(string); ok && sid != "" {
				sessionID = sid
			}
		}
		if sessionID == "" {
			return p.logf
		}
		return p.sessionLogf(sessionID)
	})
	p.handleFunc("/v1/responses", handler.ServeHTTP)
	p.handleFunc("/responses", handler.ServeHTTP)

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
