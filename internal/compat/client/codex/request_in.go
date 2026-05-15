package codex

import (
	"fmt"
	"time"

	"spark/internal/compat/ir"
)

func ResponsesInbound(req map[string]any) ir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := ir.Request{
		Model:    model,
		Messages: responsesInputToIRMessages(req["input"]),
		Tools:    responsesTools(req["tools"]),
		Stream:   boolValue(req["stream"]),
		Source:   ir.ProtocolOpenAIResponses,
		Raw:      req,
	}
	if max, ok := intValue(req["max_output_tokens"]); ok {
		out.Generation.MaxTokens = &max
	}
	if temp, ok := float64Value(req["temperature"]); ok {
		out.Generation.Temperature = temp
	} else if _, ok := req["temperature"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["temperature"] = req["temperature"]
	}
	if topP, ok := float64Value(req["top_p"]); ok {
		out.Generation.TopP = topP
	} else if _, ok := req["top_p"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["top_p"] = req["top_p"]
	}
	if stop, ok := req["stop"]; ok {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["stop"] = stop
	}
	if reasoning := responsesReasoningConfig(req["reasoning"]); reasoning.HasControls() {
		out.Generation.Reasoning = reasoning
	}
	if out.Stream {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}
	if choice, ok := responsesToolChoice(req["tool_choice"]); ok {
		out.ToolChoice = choice
	}
	if len(out.Messages) == 0 {
		out.Messages = []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("")}}}
	}
	return out
}

func responsesReasoningConfig(raw any) ir.ReasoningConfig {
	config := mapValue(raw)
	if len(config) == 0 {
		return ir.ReasoningConfig{}
	}
	out := ir.ReasoningConfig{
		Raw: map[string]any{"reasoning": raw},
	}
	if effort, ok := ir.ParseReasoningEffort(stringValue(config["effort"])); ok {
		out.Effort = effort
	}
	if summary := stringValue(config["summary"]); summary != "" {
		out.Summary = ir.ReasoningSummary(summary)
	}
	return out
}

func ensureRaw(raw map[string]any) map[string]any {
	if raw != nil {
		return raw
	}
	return map[string]any{}
}

type pendingToolCall struct {
	Name      string
	Args      string
	Reasoning string
}

func responsesInputToIRMessages(input any) []ir.Message {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case string:
		return []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text(v)}}}
	case []any:
		out := make([]ir.Message, 0, len(v))
		pendingCalls := map[string]pendingToolCall{}
		lastReasoning := ""
		appendSyntheticToolCall := func(callID, name, arguments, reasoning string) {
			if callID == "" {
				return
			}
			if name == "" {
				name = "unknown_tool"
			}
			if arguments == "" {
				arguments = "{}"
			}
			blocks := make([]ir.ContentBlock, 0, 2)
			if reasoning != "" {
				blocks = append(blocks, ir.Reasoning(reasoning))
			}
			blocks = append(blocks, ir.ContentBlock{
				Type: ir.BlockToolCall,
				ToolCall: &ir.ToolCall{
					ID:        callID,
					Type:      ir.ToolTypeFunction,
					Name:      name,
					Arguments: arguments,
				},
			})
			out = append(out, ir.Message{Role: ir.RoleAssistant, Content: blocks})
		}

		for _, item := range v {
			msg, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch stringValue(msg["type"]) {
			case "reasoning":
				if reasoning := responseReasoningContent(msg); reasoning != "" {
					lastReasoning = reasoning
				}
				continue
			case "function_call":
				callID := stringValue(msg["call_id"])
				if callID == "" {
					callID = stringValue(msg["id"])
				}
				if callID == "" {
					continue
				}
				reasoning := responseReasoningContent(msg)
				if reasoning == "" {
					reasoning = lastReasoning
				}
				pendingCalls[callID] = pendingToolCall{
					Name:      stringValue(msg["name"]),
					Args:      stringValue(msg["arguments"]),
					Reasoning: reasoning,
				}
				continue
			case "function_call_output":
				callID := stringValue(msg["call_id"])
				if callID == "" {
					callID = stringValue(msg["tool_call_id"])
				}
				if callID == "" {
					continue
				}
				output := normalizeMessageContent(msg["output"])
				if output == "" {
					output = normalizeMessageContent(msg["content"])
				}
				if output == "" {
					output = "{}"
				}
				call := pendingCalls[callID]
				reasoning := call.Reasoning
				if reasoning == "" {
					reasoning = lastReasoning
				}
				appendSyntheticToolCall(callID, call.Name, call.Args, reasoning)
				out = append(out, toolResultMessage(callID, output))
				delete(pendingCalls, callID)
				continue
			}

			role := responsesRole(msg["role"])
			if role == ir.RoleAssistant {
				reasoning := responseReasoningContent(msg)
				if reasoning == "" {
					reasoning = lastReasoning
				}
				if toolCalls := responsesAssistantToolCalls(msg["tool_calls"], reasoning, pendingCalls); len(toolCalls) > 0 {
					continue
				}
			}

			content := normalizeMessageContent(msg["content"])
			if content == "" {
				content = stringValue(msg["text"])
			}
			if role == ir.RoleTool {
				callID := stringValue(msg["tool_call_id"])
				if callID == "" {
					callID = stringValue(msg["call_id"])
				}
				if callID == "" {
					continue
				}
				if content == "" {
					content = "{}"
				}
				call := pendingCalls[callID]
				reasoning := call.Reasoning
				if reasoning == "" {
					reasoning = lastReasoning
				}
				appendSyntheticToolCall(callID, call.Name, call.Args, reasoning)
				out = append(out, toolResultMessage(callID, content))
				delete(pendingCalls, callID)
				continue
			}
			if content == "" {
				continue
			}
			out = append(out, ir.Message{Role: role, Content: []ir.ContentBlock{ir.Text(content)}})
			if role == ir.RoleUser || role == ir.RoleSystem {
				lastReasoning = ""
			}
		}
		return out
	default:
		return []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text(fmt.Sprint(v))}}}
	}
}

func responsesRole(raw any) ir.Role {
	switch role := stringValue(raw); role {
	case "developer":
		return ir.RoleSystem
	case "system":
		return ir.RoleSystem
	case "assistant":
		return ir.RoleAssistant
	case "tool":
		return ir.RoleTool
	case "user", "":
		return ir.RoleUser
	default:
		return ir.RoleUser
	}
}

func responsesAssistantToolCalls(raw any, reasoning string, pending map[string]pendingToolCall) []ir.ToolCall {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	calls := make([]ir.ToolCall, 0, len(items))
	for _, item := range items {
		tc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ := stringValue(tc["type"]); typ != "" && typ != "function" {
			continue
		}
		id := stringValue(tc["id"])
		if id == "" {
			id = fmt.Sprintf("call_%d", time.Now().UnixNano())
		}
		fn, _ := tc["function"].(map[string]any)
		name := stringValue(fn["name"])
		if name == "" {
			continue
		}
		args := stringValue(fn["arguments"])
		if args == "" {
			args = "{}"
		}
		pending[id] = pendingToolCall{Name: name, Args: args, Reasoning: reasoning}
		calls = append(calls, ir.ToolCall{
			ID:        id,
			Type:      ir.ToolTypeFunction,
			Name:      name,
			Arguments: args,
			Raw:       tc,
		})
	}
	return calls
}

func toolResultMessage(callID, output string) ir.Message {
	return ir.Message{
		Role: ir.RoleTool,
		Content: []ir.ContentBlock{
			{
				Type: ir.BlockToolResult,
				ToolResult: &ir.ToolResult{
					ToolCallID: callID,
					Output:     output,
				},
			},
		},
	}
}

func responsesTools(raw any) []ir.Tool {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ir.Tool, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || stringValue(m["type"]) != "function" {
			continue
		}
		tool := ir.Tool{
			Type: ir.ToolTypeFunction,
			Function: ir.FunctionTool{
				Name:        stringValue(m["name"]),
				Description: stringValue(m["description"]),
			},
			Raw: m,
		}
		if params, ok := m["parameters"]; ok {
			tool.Function.Parameters = params
		}
		if tool.Function.Name == "" && tool.Function.Description == "" && tool.Function.Parameters == nil {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func responsesToolChoice(raw any) (ir.ToolChoice, bool) {
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto":
			return ir.ToolChoice{Mode: ir.ToolChoiceAuto}, true
		case "none":
			return ir.ToolChoice{Mode: ir.ToolChoiceNone}, true
		case "required":
			return ir.ToolChoice{Mode: ir.ToolChoiceRequired}, true
		default:
			return ir.ToolChoice{}, false
		}
	case map[string]any:
		if stringValue(v["type"]) != "function" {
			return ir.ToolChoice{}, false
		}
		name := stringValue(v["name"])
		if name == "" {
			if fn, ok := v["function"].(map[string]any); ok {
				name = stringValue(fn["name"])
			}
		}
		if name == "" {
			return ir.ToolChoice{}, false
		}
		return ir.ToolChoice{Mode: ir.ToolChoiceFunction, Name: name}, true
	default:
		return ir.ToolChoice{}, false
	}
}
