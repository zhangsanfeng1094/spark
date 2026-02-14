package integrations

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func writeUpstreamErrorAsJSON(w http.ResponseWriter, upResp *http.Response) {
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
	errBody := map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upResp.StatusCode)
	_ = json.NewEncoder(w).Encode(errBody)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
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
