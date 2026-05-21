package gateway

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

type RouteSelection struct {
	Route      Route
	Translator RequestTranslator
	Stream     StreamWriter
	NonStream  NonStreamWriter
}

type UnsupportedRouteError struct {
	Route Route
}

func (e UnsupportedRouteError) Error() string {
	return fmt.Sprintf("unsupported compatibility route: client=%s target=%s", e.Route.Client, e.Route.Target)
}

func (r Route) normalized() Route {
	if r.Client == "" {
		r.Client = ClientCodexResponses
	}
	if r.Target == "" {
		r.Target = TargetOpenAIChat
	}
	return r
}

func SelectRoute(route Route) (RouteSelection, error) {
	route = route.normalized()
	if route.Client == ClientCodexResponses && route.Target == TargetOpenAIChat {
		return RouteSelection{
			Route:      route,
			Translator: CodexResponsesTranslator{},
			Stream:     WriteCodexResponsesStreamFromOpenAIChat,
			NonStream:  CodexResponsesFromOpenAIChatResponse,
		}, nil
	}
	if route.Client == ClientCodexResponses && route.Target == TargetAnthropicMessages {
		return RouteSelection{
			Route:      route,
			Translator: CodexResponsesToAnthropicMessagesTranslator{},
			Stream:     WriteCodexResponsesStreamFromAnthropicMessages,
			NonStream:  CodexResponsesFromAnthropicMessagesResponse,
		}, nil
	}
	return RouteSelection{}, UnsupportedRouteError{Route: route}
}
