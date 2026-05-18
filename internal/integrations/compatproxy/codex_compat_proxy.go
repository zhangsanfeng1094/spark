package compatproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"spark/internal/compat/gateway"
	"spark/internal/integrations/proxyutil"
)

type ResponsesProxy struct {
	server       *http.Server
	listener     net.Listener
	baseURL      string
	upstreamBase string
	upstreamKey  string
	mode         ResponsesProxyMode
	client       *http.Client
	quietStderr  bool
	logFile      io.WriteCloser
	logMu        sync.Mutex
	logPath      string
}

type ResponsesProxyMode string

const (
	ResponsesProxyModeChatCompletionsOnly ResponsesProxyMode = "chat_completions_only"
	ResponsesProxyModePreferResponses     ResponsesProxyMode = "prefer_responses"
)

func StartResponsesProxy(upstreamBase, upstreamKey string, quietStderr bool, mode ResponsesProxyMode) (*ResponsesProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if mode == "" {
		mode = ResponsesProxyModeChatCompletionsOnly
	}
	p := &ResponsesProxy{
		listener:     ln,
		baseURL:      "http://" + ln.Addr().String() + "/v1",
		upstreamBase: strings.TrimRight(upstreamBase, "/"),
		upstreamKey:  upstreamKey,
		mode:         mode,
		client:       proxyutil.NewStreamingHTTPClient(),
		quietStderr:  quietStderr,
	}
	logFile, logPath, err := openCompatLogFile()
	if err != nil {
		return nil, err
	}
	p.logFile = logFile
	p.logPath = logPath
	installUsageRecorder("codex", p.logf)
	p.logf("proxy started mode=%s upstream=%s listen=%s", p.mode, p.upstreamBase, p.baseURL)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", p.handleResponses)
	p.server = &http.Server{Handler: mux}

	go func() {
		_ = p.server.Serve(ln)
	}()
	return p, nil
}

func (p *ResponsesProxy) BaseURL() string {
	return p.baseURL
}

func (p *ResponsesProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return err
}

func (p *ResponsesProxy) LogPath() string {
	return p.logPath
}

func (p *ResponsesProxy) logf(format string, args ...any) {
	line := fmt.Sprintf("[compat] "+format, args...)
	p.logMu.Lock()
	defer p.logMu.Unlock()
	if p.logFile != nil {
		_, _ = fmt.Fprintf(p.logFile, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

func (p *ResponsesProxy) warnf(summary string) {
	if p.quietStderr {
		return
	}
	fmt.Fprintf(os.Stderr, "[compat] %s (details: %s)\n", summary, p.logPath)
}

func (p *ResponsesProxy) handleResponses(w http.ResponseWriter, r *http.Request) {
	reasoning := gateway.ChatReasoningAdapter{UpstreamBase: p.upstreamBase}
	handler := gateway.CodexResponsesHandler{
		Mode:          string(p.mode),
		Route:         gateway.Route{Client: gateway.ClientCodexResponses, Target: gateway.TargetOpenAIChat},
		UpstreamBase:  p.upstreamBase,
		Logf:          p.logf,
		Warnf:         p.warnf,
		PostResponses: p.postResponses,
		Executor:      newCodexChatExecutor(p),
		PrepareChat:   reasoning.ApplyToChatRequest,
	}
	handler.ServeHTTP(w, r)
}

func (p *ResponsesProxy) postResponses(ctx context.Context, req map[string]any) (*http.Response, error) {
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
	p.logf("upstream POST %s payload_structure=%s", p.upstreamBase+"/responses", gateway.StructureJSONForLog(req))
	return p.client.Do(upReq)
}

func (p *ResponsesProxy) postChatCompletions(ctx context.Context, chatReq map[string]any) (*http.Response, error) {
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
	p.logf("upstream POST %s payload_structure=%s", p.upstreamBase+"/chat/completions", gateway.StructureJSONForLog(chatReq))
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
		c := gateway.NormalizeMessageContent(msgs[i]["content"])
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

func (p *ResponsesProxy) forwardNonStream(w http.ResponseWriter, upResp *http.Response) {
	gateway.ForwardCodexNonStream(w, upResp, p.warnf, p.logf)
}

func (p *ResponsesProxy) forwardStream(w http.ResponseWriter, upResp *http.Response) {
	gateway.ForwardCodexStream(w, upResp, p.warnf, p.logf)
}
