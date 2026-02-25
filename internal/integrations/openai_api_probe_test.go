package integrations

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"spark/internal/config"
)

func TestDetectOpenAIAPITypeWithClient(t *testing.T) {
	t.Run("responses supported", func(t *testing.T) {
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/responses" {
					return fakeResponse(req, http.StatusOK, `{"id":"resp_1","object":"response"}`), nil
				}
				return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
			}),
		}

		got, err := detectOpenAIAPITypeWithClient("https://example.com", "", "gpt-4o-mini", client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != config.OpenAIAPITypeResponses {
			t.Fatalf("expected responses, got %q", got)
		}
	})

	t.Run("fallback to chat completions", func(t *testing.T) {
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/responses" {
					return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				}
				if req.URL.Path == "/chat/completions" {
					return fakeResponse(req, http.StatusOK, `{"id":"chatcmpl_1","object":"chat.completion"}`), nil
				}
				return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
			}),
		}

		got, err := detectOpenAIAPITypeWithClient("https://example.com", "", "gpt-4o-mini", client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != config.OpenAIAPITypeChatCompletions {
			t.Fatalf("expected chat_completions, got %q", got)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
