package gemini

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

func TestShouldUseUpstreamChatStream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		profile *config.Profile
		want    bool
	}{
		{name: "nil profile", want: true},
		{name: "empty api type", profile: &config.Profile{}, want: true},
		{name: "chat completions", profile: &config.Profile{OpenAIAPIType: config.OpenAIAPITypeChatCompletions}, want: true},
		{name: "responses and chat", profile: &config.Profile{OpenAIAPIType: "responses,chat_completions"}, want: true},
		{name: "responses only", profile: &config.Profile{OpenAIAPIType: config.OpenAIAPITypeResponses}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseUpstreamChatStream(tt.profile); got != tt.want {
				t.Fatalf("shouldUseUpstreamChatStream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripUnsupportedChatParams(t *testing.T) {
	t.Parallel()
	choice := "auto"
	req := &schemas.BifrostChatRequest{Params: &schemas.ChatParameters{
		Tools:      []schemas.ChatTool{{Type: schemas.ChatToolTypeFunction}},
		ToolChoice: &schemas.ChatToolChoice{ChatToolChoiceStr: &choice},
		Reasoning:  &schemas.ChatReasoning{},
	}}
	if !stripUnsupportedChatParams(req) {
		t.Fatal("expected strip")
	}
	if req.Params.Tools != nil || req.Params.ToolChoice != nil || req.Params.Reasoning != nil {
		t.Fatalf("params still set: %+v", req.Params)
	}
	if stripUnsupportedChatParams(req) {
		t.Fatal("second strip should be a no-op")
	}
}

func TestIsAuthUnavailable(t *testing.T) {
	t.Parallel()
	msg := "auth_unavailable: no auth available (providers=antigravity, model=gemini-3.7-flash-high)"
	if !isAuthUnavailable(&schemas.BifrostError{Error: &schemas.ErrorField{Message: msg}}) {
		t.Fatal("expected auth unavailable")
	}
	if isAuthUnavailable(&schemas.BifrostError{Error: &schemas.ErrorField{Message: "model not found"}}) {
		t.Fatal("did not expect auth unavailable")
	}
}
