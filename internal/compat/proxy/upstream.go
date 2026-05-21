package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"spark/internal/compat/gateway"
)

func (s *compatProxyServer) postUpstreamJSON(ctx context.Context, upstreamBase, upstreamKey, path string, payload map[string]any, logf func(format string, args ...any)) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(upstreamBase, "/") + path
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept-Encoding", "identity")
	if upstreamKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+upstreamKey)
	}
	if logf != nil {
		logf("upstream POST %s payload_structure=%s", url, gateway.StructureJSONForLog(payload))
	}
	return s.client.Do(upReq)
}

func (s *compatProxyServer) postAnthropicMessagesJSON(ctx context.Context, upstreamBase, upstreamKey string, payload map[string]any, logf func(format string, args ...any)) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(upstreamBase, "/")
	path := "/v1/messages"
	if strings.HasSuffix(base, "/v1") {
		path = "/messages"
	}
	url := base + path
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", "application/json")
	upReq.Header.Set("Accept-Encoding", "identity")
	upReq.Header.Set("anthropic-version", "2023-06-01")
	if upstreamKey != "" {
		upReq.Header.Set("x-api-key", upstreamKey)
	}
	if logf != nil {
		logf("upstream POST %s payload_structure=%s", url, gateway.StructureJSONForLog(payload))
	}
	return s.client.Do(upReq)
}
