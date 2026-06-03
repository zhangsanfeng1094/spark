package httpjson

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func DecodeRequest(r *http.Request) (map[string]any, string, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	rawBody := string(data)
	var req map[string]any
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, rawBody, err
	}
	if req == nil {
		req = map[string]any{}
	}
	return req, rawBody, nil
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	if msg == "" {
		msg = http.StatusText(status)
	}
	errBody := map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errBody)
}

func WriteUpstreamError(w http.ResponseWriter, upResp *http.Response) {
	data, _ := io.ReadAll(upResp.Body)
	msg := strings.TrimSpace(string(data))

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		_ = json.NewEncoder(w).Encode(decoded)
		return
	}

	if msg == "" {
		msg = http.StatusText(upResp.StatusCode)
	}
	WriteError(w, upResp.StatusCode, msg)
}
