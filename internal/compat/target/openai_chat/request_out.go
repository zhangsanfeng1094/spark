package openai_chat

import (
	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
)

type ChatOutbound struct {
	Reasoning policy.ReasoningPolicy
}

func (o ChatOutbound) BuildRequest(req ir.Request) map[string]any {
	model := req.Model
	if model == "" {
		model = "unknown"
	}
	out := map[string]any{
		"model":    model,
		"messages": o.chatMessages(req.Messages),
		"stream":   req.Stream,
	}
	if len(req.Tools) > 0 {
		if tools := chatTools(req.Tools); len(tools) > 0 {
			out["tools"] = tools
		}
	}
	if toolChoice, ok := chatToolChoice(req.ToolChoice); ok {
		out["tool_choice"] = toolChoice
	}
	if req.Generation.MaxTokens != nil {
		out["max_tokens"] = *req.Generation.MaxTokens
	}
	if req.Generation.Temperature != nil {
		out["temperature"] = *req.Generation.Temperature
	}
	if req.Generation.TopP != nil {
		out["top_p"] = *req.Generation.TopP
	}
	if len(req.Generation.Stop) > 0 {
		out["stop"] = req.Generation.Stop
	}
	applyChatReasoningControls(out, req.Generation.Reasoning, o.Reasoning)
	for key, value := range req.Generation.Raw {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	if req.Stream {
		ensureStreamUsageOption(out)
	}
	return out
}

func ensureStreamUsageOption(out map[string]any) {
	streamOptions, _ := out["stream_options"].(map[string]any)
	if streamOptions == nil {
		streamOptions = map[string]any{}
		out["stream_options"] = streamOptions
	}
	streamOptions["include_usage"] = true
}

func applyChatReasoningControls(out map[string]any, reasoning ir.ReasoningConfig, p policy.ReasoningPolicy) {
	controls, _ := p.ChatReasoningControls(reasoning)
	for key, value := range controls {
		out[key] = value
	}
}

func (o ChatOutbound) chatMessages(messages []ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		out = append(out, o.chatMessagesForIRMessage(msg)...)
	}
	if len(out) == 0 {
		return []map[string]any{{"role": "user", "content": ""}}
	}
	return out
}

func (o ChatOutbound) chatMessagesForIRMessage(msg ir.Message) []map[string]any {
	if msg.Role == ir.RoleTool {
		toolMessages := chatToolResultMessages(msg)
		if len(toolMessages) > 0 {
			return toolMessages
		}
	}

	chatMsg := map[string]any{
		"role":    string(msg.Role),
		"content": msg.Text(),
	}
	if msg.Role == ir.RoleAssistant {
		if reasoning, ok := o.Reasoning.ChatReasoningContent(msg); ok {
			chatMsg[o.Reasoning.ChatReasoningField()] = reasoning
		}
		if toolCalls := chatToolCalls(msg); len(toolCalls) > 0 {
			chatMsg["tool_calls"] = toolCalls
		}
	}
	return []map[string]any{chatMsg}
}

func chatToolResultMessages(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != ir.BlockToolResult || block.ToolResult == nil {
			continue
		}
		out = append(out, map[string]any{
			"role":         "tool",
			"tool_call_id": block.ToolResult.ToolCallID,
			"content":      block.ToolResult.Output,
		})
	}
	return out
}

func chatToolCalls(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil {
			continue
		}
		callType := block.ToolCall.Type
		if callType == "" {
			callType = ir.ToolTypeFunction
		}
		args := block.ToolCall.Arguments
		if args == "" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"id":   block.ToolCall.ID,
			"type": string(callType),
			"function": map[string]any{
				"name":      block.ToolCall.Name,
				"arguments": args,
			},
		})
	}
	return out
}

func chatTools(tools []ir.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if len(tool.Raw) > 0 && tool.Type != ir.ToolTypeFunction {
			out = append(out, tool.Raw)
			continue
		}
		if tool.Type != "" && tool.Type != ir.ToolTypeFunction {
			continue
		}
		fn := map[string]any{}
		if tool.Function.Name != "" {
			fn["name"] = tool.Function.Name
		}
		if tool.Function.Description != "" {
			fn["description"] = tool.Function.Description
		}
		if tool.Function.Parameters != nil {
			fn["parameters"] = tool.Function.Parameters
		}
		if len(fn) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func chatToolChoice(choice ir.ToolChoice) (any, bool) {
	switch choice.Mode {
	case ir.ToolChoiceAuto, ir.ToolChoiceNone, ir.ToolChoiceRequired:
		return string(choice.Mode), true
	case ir.ToolChoiceFunction:
		if choice.Name == "" {
			return nil, false
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": choice.Name,
			},
		}, true
	default:
		if choice.Raw != nil {
			return choice.Raw, true
		}
		return nil, false
	}
}
