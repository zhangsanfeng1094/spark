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

	t.Run("fallback to gemini generate content", func(t *testing.T) {
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/responses", "/chat/completions":
					return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				case "/v1beta/models/gemini-2.5-flash:generateContent":
					if req.Header.Get("x-goog-api-key") != "key-123" {
						t.Fatalf("missing gemini api key header: %#v", req.Header)
					}
					if req.Header.Get("Authorization") != "" {
						t.Fatalf("unexpected bearer auth for gemini probe: %#v", req.Header)
					}
					return fakeResponse(req, http.StatusOK, `{"candidates":[]}`), nil
				default:
					return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		}

		got, err := detectOpenAIAPITypeWithClient("https://generativelanguage.googleapis.com/v1beta", "key-123", "gemini-2.5-flash", client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != config.OpenAIAPITypeGeminiGenerateContent {
			t.Fatalf("expected gemini_generate_content, got %q", got)
		}
	})

	t.Run("fallback to anthropic messages", func(t *testing.T) {
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/responses", "/chat/completions", "/models/claude-sonnet-4-20250514:generateContent":
					return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				case "/v1/messages":
					if req.Header.Get("x-api-key") != "key-123" {
						t.Fatalf("missing anthropic api key header: %#v", req.Header)
					}
					if req.Header.Get("anthropic-version") == "" {
						t.Fatalf("missing anthropic version header: %#v", req.Header)
					}
					if req.Header.Get("Authorization") != "" {
						t.Fatalf("unexpected bearer auth for anthropic probe: %#v", req.Header)
					}
					return fakeResponse(req, http.StatusOK, `{"id":"msg_1","type":"message"}`), nil
				default:
					return fakeResponse(req, http.StatusNotFound, `{"error":"not found"}`), nil
				}
			}),
		}

		got, err := detectOpenAIAPITypeWithClient("https://api.anthropic.com", "key-123", "claude-sonnet-4-20250514", client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != config.OpenAIAPITypeAnthropicMessages {
			t.Fatalf("expected anthropic_messages, got %q", got)
		}
	})
}

func TestOpenAIProbePayloadGeminiGenerateContent(t *testing.T) {
	path, payload, err := openAIProbePayload("gemini-2.5-flash", config.OpenAIAPITypeGeminiGenerateContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("path mismatch: %q", path)
	}
	contents, ok := payload["contents"].([]map[string]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents mismatch: %#v", payload["contents"])
	}
	parts, ok := contents[0]["parts"].([]map[string]any)
	if !ok || len(parts) != 1 || parts[0]["text"] != "ping" {
		t.Fatalf("parts mismatch: %#v", contents[0]["parts"])
	}
	configPayload, ok := payload["generationConfig"].(map[string]any)
	if !ok || configPayload["maxOutputTokens"] != 1024 {
		t.Fatalf("generation config mismatch: %#v", payload["generationConfig"])
	}
}

func TestOpenAIProbePayloadAnthropicMessages(t *testing.T) {
	path, payload, err := openAIProbePayload("claude-sonnet-4-20250514", config.OpenAIAPITypeAnthropicMessages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/v1/messages" {
		t.Fatalf("path mismatch: %q", path)
	}
	messages, ok := payload["messages"].([]map[string]string)
	if !ok || len(messages) != 1 || messages[0]["role"] != "user" || messages[0]["content"] != "ping" {
		t.Fatalf("messages mismatch: %#v", payload["messages"])
	}
	if payload["max_tokens"] != 1024 {
		t.Fatalf("max_tokens mismatch: %#v", payload["max_tokens"])
	}
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
