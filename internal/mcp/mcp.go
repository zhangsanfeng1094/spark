// Package mcp provides server-level MCP testing and actionable status
// classification shared by the TUI and CLI. "Test" replaces the old "Probe"
// wording in user-facing surfaces; transport handling is unchanged.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"spark/internal/config"
)

const TestTimeout = 8 * time.Second

type Stage string

const (
	StageSpawn      Stage = "spawn"
	StageInitialize Stage = "initialize"
	StageToolsList  Stage = "tools/list"
)

// Result is the outcome of one MCP server test.
type Result struct {
	Stage      Stage
	Err        string
	ToolsCount int
	ToolNames  []string
	Latency    time.Duration
	ProbedAt   time.Time
}

// StatusKind classifies a server×agent cell for UI display.
type StatusKind int

const (
	StatusDisabled   StatusKind = iota // off for that agent
	StatusNotChecked                   // no test result in this session
	StatusError
	StatusOK
)

// Status is an actionable, user-facing status derived from config + last test.
type Status struct {
	Kind        StatusKind
	Headline    string // short badge text, e.g. "Auth required"
	Detail      string
	Suggestions []string
}

// FailureReason is a coarse actionable classification of a test error.
type FailureReason string

const (
	ReasonNone            FailureReason = ""
	ReasonAuthRequired    FailureReason = "auth_required"
	ReasonCommandNotFound FailureReason = "command_not_found"
	ReasonTimeout         FailureReason = "timeout"
	ReasonUnreachable     FailureReason = "unreachable"
	ReasonInvalidConfig   FailureReason = "invalid_config"
	ReasonOther           FailureReason = "error"
)

// ClassifyFailure maps a test error string to an actionable reason.
func ClassifyFailure(errStr string) FailureReason {
	s := strings.ToLower(errStr)
	switch {
	case s == "":
		return ReasonNone
	case strings.Contains(s, "401") || strings.Contains(s, "unauthorized") || strings.Contains(s, "403") || strings.Contains(s, "forbidden"):
		return ReasonAuthRequired
	case strings.Contains(s, "executable file not found") || strings.Contains(s, "command not found") || strings.Contains(s, "no such file"):
		return ReasonCommandNotFound
	case strings.Contains(s, "timeout") || strings.Contains(s, "timed out") || strings.Contains(s, "context deadline exceeded"):
		return ReasonTimeout
	case strings.Contains(s, "connection refused") || strings.Contains(s, "no such host") || strings.Contains(s, "connection reset") ||
		strings.Contains(s, "unreachable") || strings.Contains(s, "dns") || strings.Contains(s, "proxyconnect"):
		return ReasonUnreachable
	case strings.Contains(s, "missing transport") || strings.Contains(s, "either command or url") || strings.Contains(s, "invalid config"):
		return ReasonInvalidConfig
	default:
		return ReasonOther
	}
}

// FailureHeadline returns the actionable headline for a failure reason.
func FailureHeadline(reason FailureReason) string {
	switch reason {
	case ReasonAuthRequired:
		return "Auth required"
	case ReasonCommandNotFound:
		return "Command not found"
	case ReasonTimeout:
		return "Timeout"
	case ReasonUnreachable:
		return "Unreachable"
	case ReasonInvalidConfig:
		return "Invalid config"
	default:
		return "Error"
	}
}

// Summarize builds the display status for a server definition plus its most
// recent test result (nil = "Not checked").
func Summarize(server *config.McpServerConfig, result *Result) Status {
	if server == nil {
		return Status{Kind: StatusError, Headline: "Invalid config", Detail: "server not defined in configuration"}
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		return Status{
			Kind:        StatusError,
			Headline:    FailureHeadline(ReasonInvalidConfig),
			Detail:      "missing transport: command or url is required",
			Suggestions: []string{"Set a command for stdio or a URL for HTTP/SSE"},
		}
	}
	if result == nil {
		return Status{
			Kind:        StatusNotChecked,
			Headline:    "Not checked",
			Detail:      "no test run in this session",
			Suggestions: []string{"Press T to test this server"},
		}
	}
	if result.Err != "" {
		reason := ClassifyFailure(result.Err)
		return Status{
			Kind:        StatusError,
			Headline:    FailureHeadline(reason),
			Detail:      result.Err,
			Suggestions: suggestionsFor(reason, server),
		}
	}
	headline := fmt.Sprintf("OK · %d tools", result.ToolsCount)
	return Status{
		Kind:     StatusOK,
		Headline: headline,
		Detail:   fmt.Sprintf("latency %s", result.Latency.Round(time.Millisecond)),
	}
}

func suggestionsFor(reason FailureReason, server *config.McpServerConfig) []string {
	switch reason {
	case ReasonAuthRequired:
		return []string{"Server returned 401/403 — add credentials to env (e.g. API key) and test again"}
	case ReasonCommandNotFound:
		cmd := strings.TrimSpace(server.Command)
		if cmd == "" {
			cmd = "the command"
		}
		return []string{fmt.Sprintf("%q not found in PATH — install it or fix Command on the definition", cmd)}
	case ReasonTimeout:
		return []string{"Server accepted the connection but never answered initialize in time"}
	case ReasonUnreachable:
		return []string{"Endpoint did not answer — check the URL, network, or whether the remote server is running"}
	case ReasonInvalidConfig:
		return []string{"Fix the server definition: a stdio server needs command, a remote server needs url"}
	default:
		return []string{"Check server logs or command line arguments"}
	}
}

// Test runs a server-level MCP test: spawn/HTTP initialize followed by
// tools/list. It never fabricates data: tool counts come from the actual
// tools/list response.
func Test(server *config.McpServerConfig) *Result {
	start := time.Now()
	if server == nil {
		return &Result{Stage: StageSpawn, Err: "server config missing", ProbedAt: time.Now()}
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		return &Result{Stage: StageSpawn, Err: "missing transport: command or url is required", ProbedAt: time.Now()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), TestTimeout)
	defer cancel()

	var result *Result
	if strings.TrimSpace(server.URL) != "" {
		result = testHTTP(ctx, server)
	} else {
		result = testStdio(ctx, server)
	}
	if result == nil {
		result = &Result{Stage: StageSpawn, Err: "test failed without result"}
	}
	result.Latency = time.Since(start)
	result.ProbedAt = time.Now()
	return result
}

// AgentLabel returns the display label for a canonical agent name.
func AgentLabel(agent string) string {
	switch agent {
	case "":
		return "Server"
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	case "one":
		return "One"
	case "grok":
		return "Grok"
	case "agy":
		return "Agy"
	default:
		return strings.ToUpper(agent[:1]) + agent[1:]
	}
}

// EnabledAgentNames returns the sorted agent names with the server enabled.
func EnabledAgentNames(cfg *config.RootConfig, name string) []string {
	var out []string
	for _, agent := range config.McpAgents() {
		if cfg.McpAgentEnabled(name, agent) {
			out = append(out, agent)
		}
	}
	return out
}

// FormatAgentList renders "codex, claude" or "none".
func FormatAgentNames(agents []string) string {
	if len(agents) == 0 {
		return "none"
	}
	return strings.Join(agents, ", ")
}

// SortedServerNames returns server names sorted alphabetically.
func SortedServerNames(cfg *config.RootConfig) []string {
	names := make([]string, 0, len(cfg.McpServers))
	for name := range cfg.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// --- transport plumbing (moved from internal/tui probe; behavior unchanged) ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func testStdio(ctx context.Context, server *config.McpServerConfig) *Result {
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Env = envPairs(server.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &Result{Stage: StageSpawn, Err: err.Error()}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &Result{Stage: StageSpawn, Err: err.Error()}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return &Result{Stage: StageSpawn, Err: err.Error()}
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	if err := writeFrame(stdin, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "spark", "version": "dev"},
		},
	}); err != nil {
		return &Result{Stage: StageInitialize, Err: err.Error()}
	}

	if _, err := readResponse(ctx, reader, 1); err != nil {
		return &Result{Stage: StageInitialize, Err: enrichError(err, stderr.String())}
	}

	_ = writeFrame(stdin, jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
		Params:  map[string]any{},
	})

	resp, err := callStdio(ctx, stdin, reader, 2, "tools/list", map[string]any{})
	if err != nil {
		return &Result{Stage: StageToolsList, Err: enrichError(err, stderr.String())}
	}
	count, names := extractToolInfo(resp.Result)
	return &Result{Stage: StageToolsList, ToolsCount: count, ToolNames: names}
}

func callStdio(ctx context.Context, stdin io.Writer, reader *bufio.Reader, id int, method string, params any) (*jsonRPCResponse, error) {
	if err := writeFrame(stdin, jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	return readResponse(ctx, reader, id)
}

func writeFrame(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data))
	return err
}

func readResponse(ctx context.Context, reader *bufio.Reader, id int) (*jsonRPCResponse, error) {
	type outcome struct {
		resp *jsonRPCResponse
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		for {
			message, err := readFrame(reader)
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			var resp jsonRPCResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				continue
			}
			if resp.ID != id {
				continue
			}
			if resp.Error != nil {
				ch <- outcome{err: fmt.Errorf("rpc %d: %s", resp.Error.Code, resp.Error.Message)}
				return
			}
			ch <- outcome{resp: &resp}
			return
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.resp, res.err
	}
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid content-length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func testHTTP(ctx context.Context, server *config.McpServerConfig) *Result {
	client := &http.Client{Timeout: TestTimeout}
	endpoint := strings.TrimSpace(server.URL)
	sessionID := ""

	_, session, err := callHTTP(ctx, client, endpoint, sessionID, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "spark", "version": "dev"},
	})
	if err != nil {
		return &Result{Stage: StageInitialize, Err: err.Error()}
	}
	if session != "" {
		sessionID = session
	}

	resp, _, err := callHTTP(ctx, client, endpoint, sessionID, 2, "tools/list", map[string]any{})
	if sessionID != "" {
		go closeHTTPSession(endpoint, sessionID)
	}
	if err != nil {
		return &Result{Stage: StageToolsList, Err: err.Error()}
	}
	count, names := extractToolInfo(resp.Result)
	return &Result{Stage: StageToolsList, ToolsCount: count, ToolNames: names}
}

func callHTTP(ctx context.Context, client *http.Client, endpoint, sessionID string, id int, method string, params any) (*jsonRPCResponse, string, error) {
	payload, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	respBody := body
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream") {
		respBody = extractSSEData(body)
	}
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, "", fmt.Errorf("invalid MCP response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, "", fmt.Errorf("rpc %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return &rpcResp, res.Header.Get("Mcp-Session-Id"), nil
}

func extractSSEData(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	var data []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(data) == 0 {
		return body
	}
	return []byte(strings.Join(data, "\n"))
}

func extractToolInfo(raw json.RawMessage) (int, []string) {
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Tools != nil {
		names := make([]string, 0, len(payload.Tools))
		for _, t := range payload.Tools {
			if t.Name != "" {
				names = append(names, t.Name)
			}
		}
		return len(payload.Tools), names
	}
	return 0, nil
}

func closeHTTPSession(endpoint, sessionID string) {
	if endpoint == "" || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Mcp-Session-Id", sessionID)
	res, err := http.DefaultClient.Do(req)
	if err == nil && res != nil {
		res.Body.Close()
	}
}

func enrichError(err error, stderr string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return msg
	}
	return msg + " (stderr: " + strings.ReplaceAll(stderr, "\n", " | ") + ")"
}

func envPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(env))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	return pairs
}
