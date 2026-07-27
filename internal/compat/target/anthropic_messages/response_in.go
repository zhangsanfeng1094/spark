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
			// A thinking block carries encrypted reasoning signature for
			// multi-turn continuity. The `thinking` text may be empty when
			// display:"omitted" was requested; keep the block whenever a
			// signature is present so the round-trip preserves it.
			text := stringValue(block["thinking"])
			sig := stringValue(block["signature"])
			if text == "" && sig == "" {
				continue
			}
			out = append(out, ir.ContentBlock{
				Type: ir.BlockReasoning,
				Reasoning: &ir.ReasoningBlock{
					Text:       text,
					Signature:  sig,
					Display:    ir.ReasoningDisplay(stringValue(block["display"])),
					Visibility: ir.ReasoningVisibilityInternal,
				},
				Raw: block,
			})
		case "redacted_thinking":
			if data := stringValue(block["data"]); data != "" {
				out = append(out, ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       data,
						Redacted:   true,
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
