package openai

import "spark/internal/compatir"

func ChatResponse(raw map[string]any) compatir.Response {
	resp := compatir.Response{
		ID:    stringValue(raw["id"]),
		Model: stringValue(raw["model"]),
		Usage: usageFromChat(raw["usage"]),
		Raw:   raw,
	}
	choices := listValue(raw["choices"])
	if len(choices) == 0 {
		return resp
	}
	choice := mapValue(choices[0])
	resp.StopReason = stopReasonFromChat(stringValue(choice["finish_reason"]))
	resp.Output = chatMessageContent(mapValue(choice["message"]))
	return resp
}

func chatMessageContent(message map[string]any) []compatir.ContentBlock {
	out := make([]compatir.ContentBlock, 0, 4)
	if reasoning := reasoningText(message["reasoning_content"]); reasoning != "" {
		out = append(out, compatir.ContentBlock{
			Type: compatir.BlockReasoning,
			Reasoning: &compatir.ReasoningBlock{
				Text:       reasoning,
				Visibility: compatir.ReasoningVisibilityInternal,
			},
		})
	} else if reasoning := reasoningText(message["reasoning"]); reasoning != "" {
		out = append(out, compatir.ContentBlock{
			Type: compatir.BlockReasoning,
			Reasoning: &compatir.ReasoningBlock{
				Text:       reasoning,
				Visibility: compatir.ReasoningVisibilityInternal,
			},
		})
	}
	if text := normalizeContent(message["content"]); text != "" {
		out = append(out, compatir.Text(text))
	}
	out = append(out, chatToolCallsFromAny(message["tool_calls"])...)
	return out
}

func chatToolCallsFromAny(raw any) []compatir.ContentBlock {
	items := listValue(raw)
	out := make([]compatir.ContentBlock, 0, len(items))
	for _, item := range items {
		call := mapValue(item)
		fn := mapValue(call["function"])
		callType := compatir.ToolType(stringValue(call["type"]))
		if callType == "" {
			callType = compatir.ToolTypeFunction
		}
		out = append(out, compatir.ContentBlock{
			Type: compatir.BlockToolCall,
			ToolCall: &compatir.ToolCall{
				ID:        stringValue(call["id"]),
				Type:      callType,
				Name:      stringValue(fn["name"]),
				Arguments: stringValue(fn["arguments"]),
				Raw:       call,
			},
			Raw: call,
		})
	}
	return out
}
