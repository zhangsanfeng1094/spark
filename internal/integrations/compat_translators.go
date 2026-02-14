package integrations

type responsesRequestTranslator struct{}

func newResponsesRequestTranslator() RequestTranslator {
	return responsesRequestTranslator{}
}

func (responsesRequestTranslator) ToChat(req map[string]any) (map[string]any, error) {
	return responsesToChatCompletions(req), nil
}

type anthropicRequestTranslator struct{}

func newAnthropicRequestTranslator() RequestTranslator {
	return anthropicRequestTranslator{}
}

func (anthropicRequestTranslator) ToChat(req map[string]any) (map[string]any, error) {
	return anthropicToChatCompletions(req), nil
}

type anthropicResponseTranslator struct{}

func newAnthropicResponseTranslator() NonStreamResponseTranslator {
	return anthropicResponseTranslator{}
}

func (anthropicResponseTranslator) FromChat(chatResp map[string]any, requestedModel string) (map[string]any, error) {
	return chatToAnthropicMessage(chatResp, requestedModel), nil
}

// chatChunkTranslator wraps existing chunk extractors used by compat streams.
type chatChunkTranslator struct{}

func newChatChunkTranslator() StreamTranslator {
	return chatChunkTranslator{}
}

func (chatChunkTranslator) TextDelta(chunk map[string]any) string {
	return extractChatDelta(chunk)
}

func (chatChunkTranslator) ReasoningDelta(chunk map[string]any) string {
	return extractChatReasoningDelta(chunk)
}

func (chatChunkTranslator) ToolCallDeltas(chunk map[string]any) []chatToolCallDelta {
	return extractChatToolCallDeltas(chunk)
}

func (chatChunkTranslator) ToolCalls(chunk map[string]any) []chatToolCall {
	return extractChatToolCalls(chunk)
}

func (chatChunkTranslator) FullText(resp map[string]any) string {
	return extractChatText(resp)
}
