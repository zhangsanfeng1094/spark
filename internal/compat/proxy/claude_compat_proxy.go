package proxy

import (
	"context"
	"strings"

	"spark/internal/compat/engine"
	claudeingress "spark/internal/compat/ingress/claude"
	"spark/internal/config"
)

type AnthropicProxy struct {
	*compatProxyServer
	engine         *engine.Engine
	profile        *config.Profile
	upstreamBase   string
	upstreamKey    string
	preferredModel string
}

func StartAnthropicProxy(upstreamBase, upstreamKey, preferredModel string) (*AnthropicProxy, error) {
	server, err := newCompatProxyServer(openAnthropicCompatLogFile, "anthropic-compat", false)
	if err != nil {
		return nil, err
	}

	profile := &config.Profile{
		OpenAIBaseURL: strings.TrimRight(upstreamBase, "/"),
		APIKey:        upstreamKey,
		DefaultModel:  strings.TrimSpace(preferredModel),
	}

	eng, err := engine.New(context.Background(), nil, engine.NewSparkLLMPlugin(server.logf))
	if err != nil {
		_ = server.Close()
		return nil, err
	}

	provider, cleanedBase, _ := engine.MapProfileToProvider(profile)
	_ = eng.ConfigureProvider(provider, cleanedBase, upstreamKey)

	p := &AnthropicProxy{
		compatProxyServer: server,
		engine:            eng,
		profile:           profile,
		upstreamBase:      strings.TrimRight(upstreamBase, "/"),
		upstreamKey:       upstreamKey,
		preferredModel:    strings.TrimSpace(preferredModel),
	}

	handler := claudeingress.NewHandler(eng, profile, p.preferredModel, p.logf)
	handler.SetSessionLogf(func(req map[string]any) func(format string, args ...any) {
		sessionID := ""
		if md, ok := req["metadata"].(map[string]any); ok {
			if sid, ok := md["session_id"].(string); ok && sid != "" {
				sessionID = sid
			}
		}
		if sessionID == "" {
			return p.logf
		}
		return p.sessionLogf(sessionID)
	})
	p.handleFunc("/v1/messages", handler.ServeHTTP)
	p.handleFunc("/messages", handler.ServeHTTP)

	p.start()
	return p, nil
}

func (p *AnthropicProxy) logf(format string, args ...any) {
	if p == nil || p.compatProxyServer == nil {
		return
	}
	p.compatProxyServer.logf(format, args...)
}
