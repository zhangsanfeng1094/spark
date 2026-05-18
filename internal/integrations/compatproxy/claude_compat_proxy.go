package compatproxy

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

	"spark/internal/compat/gateway"
	"spark/internal/integrations/proxyutil"
)

type AnthropicProxy struct {
	server         *http.Server
	listener       net.Listener
	baseURL        string
	upstreamBase   string
	upstreamKey    string
	preferredModel string
	client         *http.Client
	logFile        io.WriteCloser
	logMu          sync.Mutex
	logPath        string
	reasoningCache gateway.ReasoningCache
}

func StartAnthropicProxy(upstreamBase, upstreamKey, preferredModel string) (*AnthropicProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	logFile, logPath, err := openAnthropicCompatLogFile()
	if err != nil {
		return nil, err
	}
	p := &AnthropicProxy{
		listener:       ln,
		baseURL:        "http://" + ln.Addr().String(),
		upstreamBase:   strings.TrimRight(upstreamBase, "/"),
		upstreamKey:    upstreamKey,
		preferredModel: strings.TrimSpace(preferredModel),
		client:         proxyutil.NewStreamingHTTPClient(),
		logFile:        logFile,
		logPath:        logPath,
	}
	installUsageRecorder("claude", p.logf)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", p.handleMessages)
	mux.HandleFunc("/messages", p.handleMessages)
	p.server = &http.Server{Handler: mux}
	go func() {
		_ = p.server.Serve(ln)
	}()
	return p, nil
}

func (p *AnthropicProxy) BaseURL() string { return p.baseURL }

func (p *AnthropicProxy) LogPath() string { return p.logPath }

func (p *AnthropicProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return err
}

func (p *AnthropicProxy) logf(format string, args ...any) {
	line := fmt.Sprintf("[anthropic-compat] "+format, args...)
	p.logMu.Lock()
	defer p.logMu.Unlock()
	if p.logFile != nil {
		_, _ = fmt.Fprintf(p.logFile, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

func (p *AnthropicProxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	reasoning := gateway.ChatReasoningAdapter{
		UpstreamBase: p.upstreamBase,
		Cache:        &p.reasoningCache,
	}
	handler := gateway.AnthropicMessagesHandler{
		PreferredModel:        p.preferredModel,
		Logf:                  p.logf,
		PostChatCompletions:   p.postChatCompletions,
		ApplyReasoningContent: reasoning.ApplyToChatRequest,
		RememberReasoning:     reasoning.RememberForToolCallIDs,
	}
	handler.ServeHTTP(w, r)
}

func (p *AnthropicProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
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
