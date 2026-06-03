package core

import "fmt"

type ClientProtocol string
type TargetProtocol string

const (
	ClientCodexResponses ClientProtocol = "codex_responses"

	TargetOpenAIChat            TargetProtocol = "openai_chat"
	TargetOpenAIResponses       TargetProtocol = "openai_responses"
	TargetAnthropicMessages     TargetProtocol = "anthropic_messages"
	TargetGeminiGenerateContent TargetProtocol = "gemini_generate_content"
)

type Route struct {
	Client ClientProtocol
	Target TargetProtocol
}

type UnsupportedRouteError struct {
	Route Route
}

func (e UnsupportedRouteError) Error() string {
	return fmt.Sprintf("unsupported compatibility route: client=%s target=%s", e.Route.Client, e.Route.Target)
}

func (r Route) Normalize() Route {
	if r.Client == "" {
		r.Client = ClientCodexResponses
	}
	if r.Target == "" {
		r.Target = TargetOpenAIChat
	}
	return r
}
