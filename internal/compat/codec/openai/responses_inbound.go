package openai

import (
	"fmt"
	"time"

	"spark/internal/compatir"
)

func ResponsesInbound(req map[string]any) compatir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := compatir.Request{
		Model:    model,
		Messages: responsesInputToMessages(req["input"]),
		Tools:    responsesTools(req["tools"]),
		Stream:   boolValue(req["stream"]),
		Source:   compatir.ProtocolOpenAIResponses,
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
		out.Messages = []compatir.Message{{Role: compatir.RoleUser, Content: []compatir.ContentBlock{compatir.Text("")}}}
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

func responsesInputToMessages(input any) []compatir.Message {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case string:
		return []compatir.Message{{Role: compatir.RoleUser, Content: []compatir.ContentBlock{compatir.Text(v)}}}
	case []any:
		out := make([]compatir.Message, 0, len(v))
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
			blocks := make([]compatir.ContentBlock, 0, 2)
			if reasoning != "" {
				blocks = append(blocks, compatir.Reasoning(reasoning))
			}
			blocks = append(blocks, compatir.ContentBlock{
				Type: compatir.BlockToolCall,
				ToolCall: &compatir.ToolCall{
					ID:        callID,
					Type:      compatir.ToolTypeFunction,
					Name:      name,
					Arguments: arguments,
				},
			})
			out = append(out, compatir.Message{Role: compatir.RoleAssistant, Content: blocks})
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
			if role == compatir.RoleAssistant {
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
			if role == compatir.RoleTool {
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
			out = append(out, compatir.Message{Role: role, Content: []compatir.ContentBlock{compatir.Text(content)}})
			if role == compatir.RoleUser || role == compatir.RoleSystem {
				lastReasoning = ""
			}
		}
		return out
	default:
		return []compatir.Message{{Role: compatir.RoleUser, Content: []compatir.ContentBlock{compatir.Text(fmt.Sprint(v))}}}
	}
}

func responsesRole(raw any) compatir.Role {
	switch role := stringValue(raw); role {
	case "developer":
		return compatir.RoleSystem
	case "system":
		return compatir.RoleSystem
	case "assistant":
		return compatir.RoleAssistant
	case "tool":
		return compatir.RoleTool
	case "user", "":
		return compatir.RoleUser
	default:
		return compatir.RoleUser
	}
}

func responsesAssistantToolCalls(raw any, reasoning string, pending map[string]pendingToolCall) []compatir.ToolCall {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	calls := make([]compatir.ToolCall, 0, len(items))
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
		calls = append(calls, compatir.ToolCall{
			ID:        id,
			Type:      compatir.ToolTypeFunction,
			Name:      name,
			Arguments: args,
			Raw:       tc,
		})
	}
	return calls
}

func toolResultMessage(callID, output string) compatir.Message {
	return compatir.Message{
		Role: compatir.RoleTool,
		Content: []compatir.ContentBlock{
			{
				Type: compatir.BlockToolResult,
				ToolResult: &compatir.ToolResult{
					ToolCallID: callID,
					Output:     output,
				},
			},
		},
	}
}

func responsesTools(raw any) []compatir.Tool {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]compatir.Tool, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || stringValue(m["type"]) != "function" {
			continue
		}
		tool := compatir.Tool{
			Type: compatir.ToolTypeFunction,
			Function: compatir.FunctionTool{
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

func responsesToolChoice(raw any) (compatir.ToolChoice, bool) {
	switch v := raw.(type) {
	case string:
		switch v {
		case "auto":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceAuto}, true
		case "none":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceNone}, true
		case "required":
			return compatir.ToolChoice{Mode: compatir.ToolChoiceRequired}, true
		default:
			return compatir.ToolChoice{}, false
		}
	case map[string]any:
		if stringValue(v["type"]) != "function" {
			return compatir.ToolChoice{}, false
		}
		name := stringValue(v["name"])
		if name == "" {
			if fn, ok := v["function"].(map[string]any); ok {
				name = stringValue(fn["name"])
			}
		}
		if name == "" {
			return compatir.ToolChoice{}, false
		}
		return compatir.ToolChoice{Mode: compatir.ToolChoiceFunction, Name: name}, true
	default:
		return compatir.ToolChoice{}, false
	}
}
