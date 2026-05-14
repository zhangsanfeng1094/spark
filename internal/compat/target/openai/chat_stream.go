package openai

import "spark/internal/compatir"

func ChatStreamEvents(chunk map[string]any) []compatir.StreamEvent {
	events := make([]compatir.StreamEvent, 0, 4)
	if usage := usageFromChat(chunk["usage"]); usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		events = append(events, compatir.StreamEvent{
			Type:  compatir.StreamEventUsage,
			Index: -1,
			Usage: &usage,
			Raw:   chunk,
		})
	}
	for choiceIndex, rawChoice := range listValue(chunk["choices"]) {
		choice := mapValue(rawChoice)
		delta := mapValue(choice["delta"])
		if reasoning := reasoningText(delta["reasoning_content"]); reasoning != "" {
			events = append(events, compatir.StreamEvent{
				Type:  compatir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: compatir.ContentBlock{
					Type: compatir.BlockReasoning,
					Reasoning: &compatir.ReasoningBlock{
						Text:       reasoning,
						Visibility: compatir.ReasoningVisibilityInternal,
					},
				},
				Raw: choice,
			})
		} else if reasoning := reasoningText(delta["reasoning"]); reasoning != "" {
			events = append(events, compatir.StreamEvent{
				Type:  compatir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: compatir.ContentBlock{
					Type: compatir.BlockReasoning,
					Reasoning: &compatir.ReasoningBlock{
						Text:       reasoning,
						Visibility: compatir.ReasoningVisibilityInternal,
					},
				},
				Raw: choice,
			})
		}
		if text := stringValue(delta["content"]); text != "" {
			events = append(events, compatir.StreamEvent{
				Type:  compatir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: compatir.Text(text),
				Raw:   choice,
			})
		}
		for _, toolCall := range chatToolCallsFromAny(delta["tool_calls"]) {
			events = append(events, compatir.StreamEvent{
				Type:  compatir.StreamEventContentDelta,
				Index: choiceIndex,
				Delta: toolCall,
				Raw:   choice,
			})
		}
		if finishReason := stringValue(choice["finish_reason"]); finishReason != "" {
			events = append(events, compatir.StreamEvent{
				Type:       compatir.StreamEventResponseDone,
				Index:      choiceIndex,
				StopReason: stopReasonFromChat(finishReason),
				Raw:        choice,
			})
		}
	}
	return events
}
