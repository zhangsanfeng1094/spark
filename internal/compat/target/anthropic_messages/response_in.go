package anthropic_messages

import (
	"fmt"

	"spark/internal/compat/ir"
)

func MessageResponse(raw map[string]any) ir.Response {
	return ir.Response{
		ID:         stringValue(raw["id"]),
		Model:      stringValue(raw["model"]),
		Output:     anthropicResponseContent(listValue(raw["content"])),
		StopReason: stopReasonFromAnthropic(stringValue(raw["stop_reason"])),
		Usage:      usageFromAnthropic(raw["usage"]),
		Raw:        raw,
	}
}

func anthropicResponseContent(items []any) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, len(items))
	for idx, item := range items {
		block := mapValue(item)
		switch stringValue(block["type"]) {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				out = append(out, ir.Text(text))
			}
		case "thinking":
			if text := stringValue(block["thinking"]); text != "" {
				out = append(out, ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       text,
						Signature:  stringValue(block["signature"]),
						Visibility: ir.ReasoningVisibilityInternal,
					},
					Raw: block,
				})
			}
		case "redacted_thinking":
			if text := stringValue(block["data"]); text != "" {
				out = append(out, ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       text,
						Visibility: ir.ReasoningVisibilityInternal,
					},
					Raw: block,
				})
			}
		case "tool_use":
			name := stringValue(block["name"])
			if name == "" {
				continue
			}
			id := stringValue(block["id"])
			if id == "" {
				id = fmt.Sprintf("toolu_%d", idx)
			}
			out = append(out, ir.ContentBlock{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        id,
					Type:      ir.ToolTypeFunction,
					Name:      name,
					Arguments: jsonString(block["input"]),
					Raw:       block,
				},
				Raw: block,
			})
		}
	}
	return out
}
