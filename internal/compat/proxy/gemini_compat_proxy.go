package proxy

import (
	"context"
	"strings"

	"spark/internal/compat/engine"
	geminiingress "spark/internal/compat/ingress/gemini"
	"spark/internal/config"
)

type GeminiProxy struct {
	*compatProxyServer
	engine         *engine.Engine
	profile        *config.Profile
	upstreamBase   string
	upstreamKey    string
	preferredModel string
}

func StartGeminiProxy(upstreamBase, upstreamKey, preferredModel string) (*GeminiProxy, error) {
	server, err := newCompatProxyServer(openGeminiCompatLogFile, "gemini-compat", false)
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

	p := &GeminiProxy{
		compatProxyServer: server,
		engine:            eng,
		profile:           profile,
		upstreamBase:      strings.TrimRight(upstreamBase, "/"),
		upstreamKey:       upstreamKey,
		preferredModel:    strings.TrimSpace(preferredModel),
	}

	handler := geminiingress.NewHandler(eng, profile, p.preferredModel, p.logf)
	p.handleFunc("/v1beta/", handler.ServeHTTP)
	p.handleFunc("/v1/", handler.ServeHTTP)
	p.handleFunc("/models/", handler.ServeHTTP)

	p.start()
	p.logf("proxy started upstream=%s listen=%s", p.upstreamBase, p.BaseURL())
	return p, nil
}

func (p *GeminiProxy) logf(format string, args ...any) {
	if p == nil || p.compatProxyServer == nil {
		return
	}
	p.compatProxyServer.logf(format, args...)
}
