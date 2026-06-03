package openai_responses

import (
	"time"

	"spark/internal/compat/ir"
	"spark/internal/usage"
)

func StreamEvents(chunk map[string]any) []ir.StreamEvent {
	events := make([]ir.StreamEvent, 0, 3)
	if rawResp := mapValue(chunk["response"]); len(rawResp) > 0 {
		if irUsage := usageFromResponses(rawResp["usage"]); hasUsage(irUsage) {
			usage.RecordIR(irUsage, stringValue(rawResp["model"]), true, time.Now().UTC())
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventUsage,
				Index: -1,
				Usage: &irUsage,
				Raw:   chunk,
			})
		}
		if stringValue(chunk["type"]) == "response.completed" {
			events = append(events, ir.StreamEvent{
				Type:       ir.StreamEventResponseDone,
				Index:      -1,
				StopReason: stopReasonFromStatus(stringValue(rawResp["status"])),
				Raw:        chunk,
			})
		}
	}
	switch stringValue(chunk["type"]) {
	case "response.output_text.delta":
		if delta := stringValue(chunk["delta"]); delta != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: intValue(chunk["output_index"]),
				Delta: ir.Text(delta),
				Raw:   chunk,
			})
		}
	case "response.reasoning_summary_text.delta":
		if delta := stringValue(chunk["delta"]); delta != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: intValue(chunk["output_index"]),
				Delta: ir.Reasoning(delta),
				Raw:   chunk,
			})
		}
	case "response.function_call_arguments.delta":
		if delta := stringValue(chunk["delta"]); delta != "" {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: intValue(chunk["output_index"]),
				Delta: ir.ContentBlock{
					Type: ir.BlockToolCall,
					ToolCall: &ir.ToolCall{
						Type:      ir.ToolTypeFunction,
						Arguments: delta,
					},
					Raw: chunk,
				},
				Raw: chunk,
			})
		}
	case "response.output_item.added", "response.output_item.done":
		if call := streamFunctionCall(mapValue(chunk["item"])); call != nil {
			events = append(events, ir.StreamEvent{
				Type:  ir.StreamEventContentDelta,
				Index: intValue(chunk["output_index"]),
				Delta: ir.ContentBlock{Type: ir.BlockToolCall, ToolCall: call, Raw: chunk},
				Raw:   chunk,
			})
		}
	}
	return events
}

func streamFunctionCall(item map[string]any) *ir.ToolCall {
	if stringValue(item["type"]) != "function_call" {
		return nil
	}
	name := stringValue(item["name"])
	args := stringValue(item["arguments"])
	if name == "" && args == "" {
		return nil
	}
	callID := stringValue(item["call_id"])
	if callID == "" {
		callID = stringValue(item["id"])
	}
	return &ir.ToolCall{
		ID:        callID,
		Type:      ir.ToolTypeFunction,
		Name:      name,
		Arguments: args,
		Raw:       item,
	}
}

func hasUsage(u ir.Usage) bool {
	return u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 || u.CacheReadInputTokens != 0 || len(u.Raw) > 0
}
