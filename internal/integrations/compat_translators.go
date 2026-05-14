package integrations

import (
	anthropicadapter "spark/internal/compat/codec/anthropic"
	openaiadapter "spark/internal/compat/codec/openai"
	"spark/internal/compat/policy"
	openaitarget "spark/internal/compat/target/openai"
)

type responsesRequestTranslator struct{}

func newResponsesRequestTranslator() RequestTranslator {
	return responsesRequestTranslator{}
}

func (responsesRequestTranslator) ToChat(req map[string]any) (map[string]any, error) {
	irReq := openaiadapter.ResponsesInbound(req)
	return openaitarget.ChatOutbound{
		Reasoning: policy.PreserveReasoningContent(),
	}.BuildRequest(irReq), nil
}

type anthropicRequestTranslator struct{}

func newAnthropicRequestTranslator() RequestTranslator {
	return anthropicRequestTranslator{}
}

func (anthropicRequestTranslator) ToChat(req map[string]any) (map[string]any, error) {
	irReq := anthropicadapter.MessagesInbound(req)
	return openaitarget.ChatOutbound{
		Reasoning: policy.PreserveReasoningContent(),
	}.BuildRequest(irReq), nil
}
