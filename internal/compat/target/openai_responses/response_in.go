package openai_responses

import "spark/internal/compat/ir"

func Response(raw map[string]any) ir.Response {
	if nested := mapValue(raw["response"]); len(nested) > 0 {
		raw = nested
	}
	resp := ir.Response{
		ID:         stringValue(raw["id"]),
		Model:      stringValue(raw["model"]),
		StopReason: stopReasonFromStatus(stringValue(raw["status"])),
		Usage:      usageFromResponses(raw["usage"]),
		Raw:        raw,
	}
	resp.Output = responseOutputBlocks(raw["output"])
	if len(resp.Output) == 0 {
		if text := stringValue(raw["output_text"]); text != "" {
			resp.Output = append(resp.Output, ir.Text(text))
		}
	}
	return resp
}

func responseOutputBlocks(raw any) []ir.ContentBlock {
	items := listValue(raw)
	out := make([]ir.ContentBlock, 0, len(items))
	for _, itemRaw := range items {
		item := mapValue(itemRaw)
		if len(item) == 0 {
			continue
		}
		switch stringValue(item["type"]) {
		case "message":
			out = append(out, responseMessageBlocks(item)...)
		case "reasoning":
			if reasoning := reasoningText(item); reasoning != "" {
				out = append(out, ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       reasoning,
						Visibility: ir.ReasoningVisibilityInternal,
					},
					Raw: item,
				})
			}
		case "function_call":
			if call := responseFunctionCall(item); call != nil {
				out = append(out, ir.ContentBlock{Type: ir.BlockToolCall, ToolCall: call, Raw: item})
			}
		}
	}
	return out
}

func responseMessageBlocks(item map[string]any) []ir.ContentBlock {
	content := listValue(item["content"])
	out := make([]ir.ContentBlock, 0, len(content))
	for _, rawPart := range content {
		part := mapValue(rawPart)
		if len(part) == 0 {
			continue
		}
		switch stringValue(part["type"]) {
		case "output_text", "text":
			if text := stringValue(part["text"]); text != "" {
				out = append(out, ir.Text(text))
			}
		case "reasoning", "summary_text":
			if text := stringValue(part["text"]); text != "" {
				out = append(out, ir.Reasoning(text))
			}
		}
	}
	if len(out) == 0 {
		if text := normalizeContent(item["content"]); text != "" {
			out = append(out, ir.Text(text))
		}
	}
	return out
}

func responseFunctionCall(item map[string]any) *ir.ToolCall {
	name := stringValue(item["name"])
	if name == "" {
		return nil
	}
	callID := stringValue(item["call_id"])
	if callID == "" {
		callID = stringValue(item["id"])
	}
	args := stringValue(item["arguments"])
	if args == "" {
		args = "{}"
	}
	return &ir.ToolCall{
		ID:        callID,
		Type:      ir.ToolTypeFunction,
		Name:      name,
		Arguments: args,
		Raw:       item,
	}
}

func reasoningText(item map[string]any) string {
	if text := stringValue(item["text"]); text != "" {
		return text
	}
	if text := stringValue(item["content"]); text != "" {
		return text
	}
	parts := make([]ir.ContentBlock, 0, 1)
	for _, summaryRaw := range listValue(item["summary"]) {
		summary := mapValue(summaryRaw)
		if text := stringValue(summary["text"]); text != "" {
			parts = append(parts, ir.Reasoning(text))
		}
	}
	return responseReasoningText(parts)
}
