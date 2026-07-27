package openai_chat

import "spark/internal/compat/ir"

func ChatResponse(raw map[string]any) ir.Response {
	irUsage := usageFromChat(raw["usage"])
	resp := ir.Response{
		ID:    stringValue(raw["id"]),
		Model: stringValue(raw["model"]),
		Usage: irUsage,
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

func chatMessageContent(message map[string]any) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, 4)
	if reasoning, ok := firstReasoningText(message); ok {
		out = append(out, ir.ContentBlock{
			Type: ir.BlockReasoning,
			Reasoning: &ir.ReasoningBlock{
				Text:       reasoning,
				Visibility: ir.ReasoningVisibilityInternal,
			},
		})
	}
	if text := normalizeContent(message["content"]); text != "" {
		out = append(out, ir.Text(text))
	}
	out = append(out, chatToolCallsFromAny(message["tool_calls"])...)
	return out
}

func chatToolCallsFromAny(raw any) []ir.ContentBlock {
	items := listValue(raw)
	out := make([]ir.ContentBlock, 0, len(items))
	for _, item := range items {
		call := mapValue(item)
		fn := mapValue(call["function"])
		callType := ir.ToolType(stringValue(call["type"]))
		if callType == "" {
			callType = ir.ToolTypeFunction
		}
		out = append(out, ir.ContentBlock{
			Type: ir.BlockToolCall,
			ToolCall: &ir.ToolCall{
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
