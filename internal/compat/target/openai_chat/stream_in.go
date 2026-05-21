package openai_chat

import (
	"time"

	"spark/internal/compat/ir"
	"spark/internal/usage"
)

func ChatStreamEvents(chunk map[string]any) []ir.StreamEvent {
	events := make([]ir.StreamEvent, 0, 4)
	if irUsage := usageFromChat(chunk["usage"]); irUsage.InputTokens != 0 || irUsage.OutputTokens != 0 || irUsage.TotalTokens != 0 || irUsage.CacheReadInputTokens != 0 {
		usage.RecordIR(irUsage, stringValue(chunk["model"]), true, time.Now().UTC())
		events = append(events, ir.StreamEvent{
			Type:  ir.StreamEventUsage,
			Index: -1,
			Usage: &irUsage,
			Raw:   chunk,
		})
	}
	for choiceIndex, rawChoice := range listValue(chunk["choices"]) {
		choice := mapValue(rawChoice)
		delta := mapValue(choice["delta"])
		if reasoning := reasoningText(delta["reasoning_content"]); reasoning != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       reasoning,
						Visibility: ir.ReasoningVisibilityInternal,
					},
				},
				Raw: choice,
			})
		} else if reasoning := reasoningText(delta["reasoning"]); reasoning != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: ir.ContentBlock{
					Type: ir.BlockReasoning,
					Reasoning: &ir.ReasoningBlock{
						Text:       reasoning,
						Visibility: ir.ReasoningVisibilityInternal,
					},
				},
				Raw: choice,
			})
		}
		if text := stringValue(delta["content"]); text != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: ir.Text(text),
				Raw:   choice,
			})
		}
		for _, toolCall := range chatToolCallsFromAny(delta["tool_calls"]) {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: toolCall,
				Raw:   choice,
			})
		}
		if finishReason := stringValue(choice["finish_reason"]); finishReason != "" {
			events = append(events, ir.StreamEvent{
				Type:       ir.StreamEventResponseDone,
				Index:      choiceIndex,
				StopReason: stopReasonFromChat(finishReason),
				Raw:        choice,
			})
		}
	}
	return events
}
