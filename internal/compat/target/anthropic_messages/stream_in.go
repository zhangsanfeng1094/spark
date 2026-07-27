package anthropic_messages

import (
	"time"

	"spark/internal/compat/ir"
	"spark/internal/usage"
)

func MessageStreamEvents(chunk map[string]any) []ir.StreamEvent {
	events := make([]ir.StreamEvent, 0, 3)
	eventType := stringValue(chunk["type"])
	if eventType == "" {
		eventType = stringValue(chunk["event"])
	}
	if irUsage := usageFromAnthropic(chunk["usage"]); hasUsage(irUsage) {
		usage.RecordIR(irUsage, stringValue(chunk["model"]), true, time.Now().UTC())
		events = append(events, ir.StreamEvent{
			Type:  ir.StreamEventUsage,
			Index: -1,
			Usage: &irUsage,
			Raw:   chunk,
		})
	}
	switch eventType {
	case "message_start":
		message := mapValue(chunk["message"])
		if irUsage := usageFromAnthropic(message["usage"]); hasUsage(irUsage) {
			usage.RecordIR(irUsage, stringValue(message["model"]), true, time.Now().UTC())
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventUsage,
				Index: -1,
				Usage: &irUsage,
				Raw:   chunk,
			})
		}
	case "content_block_start":
		if event := contentBlockStartEvent(chunk); event.Type != "" {
			events = append(events, event)
		}
	case "content_block_delta":
		if event := contentBlockDeltaEvent(chunk); event.Type != "" {
			events = append(events, event)
		}
	case "message_delta":
		delta := mapValue(chunk["delta"])
		if stop := stopReasonFromAnthropic(stringValue(delta["stop_reason"])); stop != ir.StopReasonUnknown {
			events = append(events, ir.StreamEvent{
				Type:       ir.StreamEventResponseDone,
				Index:      0,
				StopReason: stop,
				Raw:        chunk,
			})
		}
	}
	return events
}

func contentBlockStartEvent(chunk map[string]any) ir.StreamEvent {
	index := intValue(chunk["index"])
	block := mapValue(chunk["content_block"])
	switch stringValue(block["type"]) {
	case "text":
		if text := stringValue(block["text"]); text != "" {
			return ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: index,
				Delta: ir.Text(text),
				Raw:   chunk,
			}
		}
	case "thinking":
		// content_block_start opens a thinking block carrying the (possibly
		// empty for display:omitted) thinking text and a signature slot
		// that signature_delta fills later. Emit a delta with whatever we
		// have so downstream can echo the block start; the signature is
		// finalized in the signature_delta event.
		text := stringValue(block["thinking"])
		sig := stringValue(block["signature"])
		if text == "" && sig == "" {
			return ir.StreamEvent{}
		}
		return ir.StreamEvent{
			Type:  ir.StreamEventContentDelta,
			Index: index,
			Delta: ir.ContentBlock{
				Type: ir.BlockReasoning,
				Reasoning: &ir.ReasoningBlock{
					Text:       text,
					Signature:  sig,
					Display:   ir.ReasoningDisplay(stringValue(block["display"])),
					Visibility: ir.ReasoningVisibilityInternal,
				},
			},
			Raw: chunk,
		}
	case "redacted_thinking":
		if data := stringValue(block["data"]); data != "" {
			return ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: index,
				Delta: ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       data,
						Redacted:   true,
						Visibility: ir.ReasoningVisibilityInternal,
					},
					Raw: block,
				},
				Raw: chunk,
			}
		}
	case "tool_use":
		name := stringValue(block["name"])
		if name == "" {
			return ir.StreamEvent{}
		}
		args := ""
		if input := mapValue(block["input"]); len(input) > 0 {
			args = jsonString(input)
		}
		return ir.StreamEvent{
			Type:  ir.StreamEventContentDelta,
			Index: index,
			Delta: ir.ContentBlock{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        stringValue(block["id"]),
					Type:      ir.ToolTypeFunction,
					Name:      name,
					Arguments: args,
					Raw:       block,
				},
				Raw: map[string]any{"index": index},
			},
			Raw: chunk,
		}
	}
	return ir.StreamEvent{}
}

func contentBlockDeltaEvent(chunk map[string]any) ir.StreamEvent {
	index := intValue(chunk["index"])
	delta := mapValue(chunk["delta"])
	switch stringValue(delta["type"]) {
	case "text_delta":
		text := stringValue(delta["text"])
		if text == "" {
			return ir.StreamEvent{}
		}
		return ir.StreamEvent{
			Type:  ir.StreamEventContentDelta,
			Index: index,
			Delta: ir.Text(text),
			Raw:   chunk,
		}
	case "thinking_delta":
		text := stringValue(delta["thinking"])
		if text == "" {
			return ir.StreamEvent{}
		}
		return ir.StreamEvent{
			Type:  ir.StreamEventContentDelta,
			Index: index,
			Delta: ir.ContentBlock{
				Type: ir.BlockReasoning,
				Reasoning: &ir.ReasoningBlock{
					Text:       text,
					Visibility: ir.ReasoningVisibilityInternal,
				},
			},
			Raw: chunk,
		}
	case "input_json_delta":
		partial := stringValue(delta["partial_json"])
		if partial == "" {
			return ir.StreamEvent{}
		}
		return ir.StreamEvent{
			Type:  ir.StreamEventContentDelta,
			Index: index,
			Delta: ir.ContentBlock{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					Type:      ir.ToolTypeFunction,
					Arguments: partial,
				},
				Raw: map[string]any{"index": index},
			},
			Raw: chunk,
		}
	}
	return ir.StreamEvent{}
}

func hasUsage(u ir.Usage) bool {
	return u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 || u.CacheCreationInputTokens != 0 || u.CacheReadInputTokens != 0
}
