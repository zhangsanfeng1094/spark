package bridge

import (
	"fmt"
	"io"

	"spark/internal/compat/gateway/core"
	"spark/internal/compat/policy"
)

type RouteSelection struct {
	Route      core.Route
	Translator core.RequestTranslator
	Stream     StreamWriter
	NonStream  NonStreamWriter
}

type SelectionOptions struct {
	Reasoning policy.ReasoningPolicy
}

type InvalidCodecError struct {
	Protocol string
	Field    string
}

func (e InvalidCodecError) Error() string {
	return fmt.Sprintf("invalid compatibility codec: protocol=%s missing=%s", e.Protocol, e.Field)
}

func SelectRoute(route core.Route) (RouteSelection, error) {
	return SelectRouteWithOptions(route, SelectionOptions{})
}

func SelectRouteWithOptions(route core.Route, opts SelectionOptions) (RouteSelection, error) {
	route = route.Normalize()
	client, err := selectClientCodec(route.Client)
	if err != nil {
		return RouteSelection{}, core.UnsupportedRouteError{Route: route}
	}
	target, err := selectTargetCodec(route.Target, opts)
	if err != nil {
		return RouteSelection{}, core.UnsupportedRouteError{Route: route}
	}
	if err := validateClientCodec(client); err != nil {
		return RouteSelection{}, err
	}
	if err := validateTargetCodec(target); err != nil {
		return RouteSelection{}, err
	}
	return composeRoute(route, client, target), nil
}

func validateClientCodec(codec ClientCodec) error {
	protocol := string(codec.Protocol)
	if codec.RequestInbound == nil {
		return InvalidCodecError{Protocol: protocol, Field: "RequestInbound"}
	}
	if codec.ResponseOutbound == nil {
		return InvalidCodecError{Protocol: protocol, Field: "ResponseOutbound"}
	}
	if codec.NewStreamWriter == nil {
		return InvalidCodecError{Protocol: protocol, Field: "NewStreamWriter"}
	}
	return nil
}

func validateTargetCodec(codec TargetCodec) error {
	protocol := string(codec.Protocol)
	if codec.RequestOutbound == nil {
		return InvalidCodecError{Protocol: protocol, Field: "RequestOutbound"}
	}
	if codec.ResponseInbound == nil {
		return InvalidCodecError{Protocol: protocol, Field: "ResponseInbound"}
	}
	if codec.StreamEvents == nil {
		return InvalidCodecError{Protocol: protocol, Field: "StreamEvents"}
	}
	return nil
}

func selectClientCodec(protocol core.ClientProtocol) (ClientCodec, error) {
	switch protocol {
	case core.ClientCodexResponses:
		return CodexResponsesClientCodec(), nil
	default:
		return ClientCodec{}, core.UnsupportedRouteError{Route: core.Route{Client: protocol}}
	}
}

func selectTargetCodec(protocol core.TargetProtocol, opts SelectionOptions) (TargetCodec, error) {
	switch protocol {
	case core.TargetOpenAIChat:
		reasoning := opts.Reasoning
		if reasoning.Mode == "" && reasoning.Field == "" && !reasoning.AllowReasoningEffort && !reasoning.AllowThinking {
			reasoning = policy.PreserveReasoningContent()
		}
		return OpenAIChatTargetCodec(reasoning), nil
	case core.TargetOpenAIResponses:
		reasoning := opts.Reasoning
		if reasoning.Mode == "" && reasoning.Field == "" && !reasoning.AllowReasoningEffort && !reasoning.AllowThinking {
			reasoning = policy.PreserveReasoningContent()
		}
		return OpenAIResponsesTargetCodec(reasoning), nil
	case core.TargetAnthropicMessages:
		return AnthropicMessagesTargetCodec(), nil
	default:
		return TargetCodec{}, core.UnsupportedRouteError{Route: core.Route{Target: protocol}}
	}
}

func composeRoute(route core.Route, client ClientCodec, target TargetCodec) RouteSelection {
	return RouteSelection{
		Route:      route.Normalize(),
		Translator: RequestBridge{Client: client, Target: target},
		Stream: func(writer io.Writer, reader io.Reader, flush func()) StreamResult {
			return WriteClientStreamFromTarget(client, target, writer, reader, flush)
		},
		NonStream: func(resp map[string]any) map[string]any {
			return ClientResponseFromTargetResponse(client, target, resp)
		},
	}
}
