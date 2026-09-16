package gemini

import "testing"

func TestParseGeminiPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path   string
		model  string
		stream bool
		ok     bool
	}{
		{path: "/v1beta/models/gemini-3.7-flash:streamGenerateContent", model: "gemini-3.7-flash", stream: true, ok: true},
		{path: "/v1beta/models/gemini-3.7-flash:generateContent", model: "gemini-3.7-flash", stream: false, ok: true},
		{path: "/models/gemini-pro:generateContent", model: "gemini-pro", stream: false, ok: true},
		{path: "/v1/models/gemini-pro:streamGenerateContent", model: "gemini-pro", stream: true, ok: true},
		{path: "/v1beta/publishers/google/models/gemini-pro:generateContent", model: "gemini-pro", stream: false, ok: true},
		{path: "/v1beta/models/gemini-3.7-flash:streamGenerateContent?alt=sse", model: "gemini-3.7-flash", stream: true, ok: true},
		{path: "/v1/messages", ok: false},
		{path: "/v1beta/models/gemini-pro", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			model, stream, ok := ParseGeminiPath(tt.path)
			if ok != tt.ok || stream != tt.stream || model != tt.model {
				t.Fatalf("ParseGeminiPath(%q)=(%q,%v,%v) want (%q,%v,%v)", tt.path, model, stream, ok, tt.model, tt.stream, tt.ok)
			}
		})
	}
}
