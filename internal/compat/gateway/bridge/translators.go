package bridge

import (
	anthropicadapter "spark/internal/compat/client/anthropic_messages"
	"spark/internal/compat/client/codex"
	"spark/internal/compat/gateway/core"
	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
	anthropic_messages_target "spark/internal/compat/target/anthropic_messages"
	openai_chat_target "spark/internal/compat/target/openai_chat"
	openai_responses_target "spark/internal/compat/target/openai_responses"
)

type RequestBridge struct {
	Client ClientCodec
	Target TargetCodec
}

func (b RequestBridge) Translate(req map[string]any) (map[string]any, error) {
	return b.Target.RequestOutbound(b.Client.RequestInbound(req)), nil
}

func CodexResponsesClientCodec() ClientCodec {
	return ClientCodec{
		Protocol:         core.ClientCodexResponses,
		RequestInbound:   codex.ResponsesInbound,
		ResponseOutbound: codex.ResponsesClientResponse,
		NewStreamWriter:  newCodexResponsesStreamWriter,
	}
}

func AnthropicMessagesClientCodec() ClientCodec {
	return ClientCodec{
		Protocol:       core.ClientProtocol(ir.ProtocolAnthropicMessages),
		RequestInbound: anthropicadapter.MessagesInbound,
	}
}

func OpenAIChatTargetCodec(reasoning policy.ReasoningPolicy) TargetCodec {
	return TargetCodec{
		Protocol:        core.TargetOpenAIChat,
		RequestOutbound: openAIChatRequestOutbound(reasoning),
		ResponseInbound: openai_chat_target.ChatResponse,
		StreamEvents:    openai_chat_target.ChatStreamEvents,
	}
}

func OpenAIResponsesTargetCodec(reasoning policy.ReasoningPolicy) TargetCodec {
	return TargetCodec{
		Protocol:        core.TargetOpenAIResponses,
		RequestOutbound: openAIResponsesRequestOutbound(reasoning),
		ResponseInbound: openai_responses_target.Response,
		StreamEvents:    openai_responses_target.StreamEvents,
	}
}

func AnthropicMessagesTargetCodec() TargetCodec {
	return TargetCodec{
		Protocol:           core.TargetAnthropicMessages,
		RequestOutbound:    anthropic_messages_target.MessagesOutbound{}.BuildRequest,
		ResponseInbound:    anthropic_messages_target.MessageResponse,
		StreamEvents:       anthropic_messages_target.MessageStreamEvents,
		PrepareStreamChunk: prepareAnthropicMessageStreamChunk,
	}
}

func OpenAIChatRequestBridge(client ClientCodec, reasoning policy.ReasoningPolicy) RequestBridge {
	return RequestBridge{
		Client: client,
		Target: OpenAIChatTargetCodec(reasoning),
	}
}

func OpenAIResponsesRequestBridge(client ClientCodec, reasoning policy.ReasoningPolicy) RequestBridge {
	return RequestBridge{
		Client: client,
		Target: OpenAIResponsesTargetCodec(reasoning),
	}
}

func openAIChatRequestOutbound(reasoning policy.ReasoningPolicy) func(ir.Request) map[string]any {
	return openai_chat_target.ChatOutbound{
		Reasoning: reasoning,
	}.BuildRequest
}

func openAIResponsesRequestOutbound(reasoning policy.ReasoningPolicy) func(ir.Request) map[string]any {
	return openai_responses_target.Outbound{
		Reasoning: reasoning,
	}.BuildRequest
}
