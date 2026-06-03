package httpjson

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRequestReturnsRawBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"glm-5.1"}`))
	got, raw, err := DecodeRequest(req)
	if err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if raw != `{"model":"glm-5.1"}` {
		t.Fatalf("raw body mismatch: %q", raw)
	}
	if got["model"] != "glm-5.1" {
		t.Fatalf("decoded body mismatch: %#v", got)
	}
}

func TestWriteUpstreamErrorWrapsPlainText(t *testing.T) {
	upResp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader("invalid json")),
	}
	rec := httptest.NewRecorder()

	WriteUpstreamError(rec, upResp)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status mismatch: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("expected json error body, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid json") {
		t.Fatalf("expected original error message, got %q", rec.Body.String())
	}
}

func TestWriteUpstreamErrorPreservesJSONBody(t *testing.T) {
	upResp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad key"}}`)),
	}
	rec := httptest.NewRecorder()

	WriteUpstreamError(rec, upResp)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status mismatch: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"bad key"`) {
		t.Fatalf("expected original json error, got %q", rec.Body.String())
	}
}
