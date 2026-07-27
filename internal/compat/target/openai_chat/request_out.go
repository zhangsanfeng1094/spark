package openai_chat

import (
	"encoding/json"

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
		"role": string(msg.Role),
	}
	if msg.Role == ir.RoleAssistant {
		chatMsg["content"] = msg.Text()
	} else {
		chatMsg["content"] = chatRequestContent(msg)
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

// chatRequestContent renders an IR message's content for OpenAI chat
// completions. It returns a plain string when no block carries an Anthropic
// cache_control breakpoint, and an array of typed blocks otherwise so the
// cache hint survives the protocol hop for gateways that translate chat
// completions back to Anthropic Messages (new-api/one-api/OpenRouter/...).
//
// Anthropic's explicit cache breakpoint format places `cache_control`
// directly on individual content blocks in the content array (see
// https://docs.claude.com/en/docs/build-with-claude/prompt-caching).
// We mirror that structure so a chat->anthropic gateway can rebuild the
// system/content arrays with block-level cache_control and keep cache keys
// aligned with the native /v1/messages path.
//
// Per Anthropic docs, thinking blocks cannot be explicitly cached, so we
// never emit cache_control on reasoning blocks (they are still cached
// indirectly as part of an assistant turn when replayed in later requests).
func chatRequestContent(msg ir.Message) any {
	hasCacheControl := false
	for _, block := range msg.Content {
		if len(block.CacheControl) > 0 && block.Type != ir.BlockReasoning {
			hasCacheControl = true
			break
		}
	}
	if !hasCacheControl {
		return msg.Text()
	}
	blocksOut := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch block.Type {
		case ir.BlockText:
			if block.Text == "" {
				continue
			}
			entry := map[string]any{"type": "text", "text": block.Text}
			if len(block.CacheControl) > 0 {
				entry["cache_control"] = block.CacheControl
			}
			blocksOut = append(blocksOut, entry)
		case ir.BlockToolResult:
			if block.ToolResult == nil {
				continue
			}
			entry := map[string]any{
				"type": "tool_result",
				"text": block.ToolResult.Output,
			}
			if block.ToolResult.ToolCallID != "" {
				entry["tool_use_id"] = block.ToolResult.ToolCallID
			}
			if len(block.CacheControl) > 0 {
				entry["cache_control"] = block.CacheControl
			}
			blocksOut = append(blocksOut, entry)
		case ir.BlockReasoning:
			if block.Reasoning == nil || block.Reasoning.Text == "" {
				continue
			}
			// Thinking blocks are not cacheable per Anthropic spec; emit
			// them without a cache_control marker so the gateway keeps
			// them in the assistant turn (they're cached indirectly via
			// the surrounding prefix that an explicit breakpoint covers).
			blocksOut = append(blocksOut, map[string]any{
				"type":     "thinking",
				"thinking": block.Reasoning.Text,
			})
		case ir.BlockToolCall:
			if block.ToolCall == nil {
				continue
			}
			entry := map[string]any{
				"type":  "tool_use",
				"id":    block.ToolCall.ID,
				"name":  block.ToolCall.Name,
				"input": jsonArguments(block.ToolCall.Arguments),
			}
			if len(block.CacheControl) > 0 {
				entry["cache_control"] = block.CacheControl
			}
			blocksOut = append(blocksOut, entry)
		}
	}
	if len(blocksOut) == 0 {
		return msg.Text()
	}
	return blocksOut
}

func jsonArguments(args string) any {
	var parsed any
	if err := json.Unmarshal([]byte(args), &parsed); err == nil {
		return parsed
	}
	return map[string]any{}
}

func chatToolResultMessages(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != ir.BlockToolResult || block.ToolResult == nil {
			continue
		}
		entry := map[string]any{
			"role":         "tool",
			"tool_call_id": block.ToolResult.ToolCallID,
		}
		if len(block.CacheControl) > 0 {
			entry["content"] = []map[string]any{{
				"type":          "text",
				"text":          block.ToolResult.Output,
				"cache_control": block.CacheControl,
			}}
		} else {
			entry["content"] = block.ToolResult.Output
		}
		out = append(out, entry)
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
		entry := map[string]any{
			"type":     "function",
			"function": fn,
		}
		if len(tool.CacheControl) > 0 {
			entry["cache_control"] = tool.CacheControl
		}
		out = append(out, entry)
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
