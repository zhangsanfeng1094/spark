package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"spark/internal/config"
)

const mcpProbeTimeout = 8 * time.Second

type mcpJSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpJSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func probeMCPServer(name string, server *config.McpServerConfig) *mcpProbeResult {
	start := time.Now()
	if server == nil {
		return &mcpProbeResult{
			Stage:    mcpProbeStageSpawn,
			Err:      "server config missing",
			Latency:  time.Since(start),
			ProbedAt: time.Now(),
		}
	}

	if detail, _ := validateMCPServerConfig(server); detail != "" {
		return &mcpProbeResult{
			Stage:    mcpProbeStageSpawn,
			Err:      detail,
			Latency:  time.Since(start),
			ProbedAt: time.Now(),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), mcpProbeTimeout)
	defer cancel()

	var result *mcpProbeResult
	if isHTTPMCPServer(server) {
		result = probeHTTPMCPServer(ctx, server)
	} else {
		result = probeStdioMCPServer(ctx, server)
	}
	if result == nil {
		result = &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: "probe failed without result"}
	}
	result.Latency = time.Since(start)
	result.ProbedAt = time.Now()
	_ = name
	return result
}

func probeStdioMCPServer(ctx context.Context, server *config.McpServerConfig) *mcpProbeResult {
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Env = envPairs(server.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: err.Error()}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: err.Error()}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageSpawn, Err: err.Error()}
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	if err := writeMCPFrame(stdin, mcpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "spark",
				"version": "dev",
			},
		},
	}); err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageInitialize, Err: err.Error()}
	}

	if _, err := readMCPResponse(ctx, reader, 1); err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageInitialize, Err: enrichProbeError(err, stderr.String())}
	}

	_ = writeMCPFrame(stdin, mcpJSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
		Params:  map[string]any{},
	})

	resp, err := callStdioMCP(ctx, stdin, reader, 2, "tools/list", map[string]any{})
	if err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageToolsList, Err: enrichProbeError(err, stderr.String())}
	}
	return &mcpProbeResult{Stage: mcpProbeStageToolsList, ToolsCount: extractToolCount(resp.Result)}
}

func callStdioMCP(ctx context.Context, stdin io.Writer, reader *bufio.Reader, id int, method string, params any) (*mcpJSONRPCResponse, error) {
	if err := writeMCPFrame(stdin, mcpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return nil, err
	}
	return readMCPResponse(ctx, reader, id)
}

func writeMCPFrame(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	_, err = io.WriteString(w, frame)
	return err
}

func readMCPResponse(ctx context.Context, reader *bufio.Reader, id int) (*mcpJSONRPCResponse, error) {
	type result struct {
		resp *mcpJSONRPCResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			message, err := readMCPFrame(reader)
			if err != nil {
				ch <- result{err: err}
				return
			}
			var resp mcpJSONRPCResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				continue
			}
			if resp.ID != id {
				continue
			}
			if resp.Error != nil {
				ch <- result{err: fmt.Errorf("rpc %d: %s", resp.Error.Code, resp.Error.Message)}
				return
			}
			ch <- result{resp: &resp}
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

func readMCPFrame(reader *bufio.Reader) ([]byte, error) {
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

func probeHTTPMCPServer(ctx context.Context, server *config.McpServerConfig) *mcpProbeResult {
	client := &http.Client{Timeout: mcpProbeTimeout}
	sessionID := ""

	initResp, session, err := callHTTPMCP(ctx, client, strings.TrimSpace(server.URL), sessionID, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "spark",
			"version": "dev",
		},
	})
	if err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageInitialize, Err: err.Error()}
	}
	if session != "" {
		sessionID = session
	}
	_ = initResp

	resp, _, err := callHTTPMCP(ctx, client, strings.TrimSpace(server.URL), sessionID, 2, "tools/list", map[string]any{})
	if sessionID != "" {
		go closeHTTPSession(strings.TrimSpace(server.URL), sessionID)
	}
	if err != nil {
		return &mcpProbeResult{Stage: mcpProbeStageToolsList, Err: err.Error()}
	}
	return &mcpProbeResult{Stage: mcpProbeStageToolsList, ToolsCount: extractToolCount(resp.Result)}
}

func callHTTPMCP(ctx context.Context, client *http.Client, endpoint, sessionID string, id int, method string, params any) (*mcpJSONRPCResponse, string, error) {
	payload, err := json.Marshal(mcpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
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
	var rpcResp mcpJSONRPCResponse
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

func extractToolCount(raw json.RawMessage) int {
	var payload struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Tools != nil {
		return len(payload.Tools)
	}
	return 0
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

func enrichProbeError(err error, stderr string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return msg
	}
	stderr = strings.ReplaceAll(stderr, "\n", " | ")
	return msg + " (stderr: " + stderr + ")"
}
