package gateway

import (
	"net/http"
	"strings"
)

func ShouldFallbackFromResponses(status int, data []byte) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType, http.StatusNotImplemented:
		return true
	}
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(data)))
	if msg == "" {
		return false
	}
	unsupportedSignals := []string{
		"unknown parameter",
		"unknown field",
		"unexpected field",
		"unrecognized field",
	}
	hasUnsupportedSignal := false
	for _, s := range unsupportedSignals {
		if strings.Contains(msg, s) {
			hasUnsupportedSignal = true
			break
		}
	}
	if hasUnsupportedSignal {
		if strings.Contains(msg, "input") || strings.Contains(msg, "instructions") || strings.Contains(msg, "max_output_tokens") {
			return true
		}
	}
	if strings.Contains(msg, "responses") && (strings.Contains(msg, "not support") || strings.Contains(msg, "unsupported")) {
		return true
	}
	return false
}
