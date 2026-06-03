package openai_responses

import (
	"fmt"
	"time"

	"spark/internal/compat/ir"
	"spark/internal/compat/policy"
)

type Outbound struct {
	Reasoning policy.ReasoningPolicy
}

func (o Outbound) BuildRequest(req ir.Request) map[string]any {
	model := req.Model
	if model == "" {
		model = "unknown"
	}
	out := map[string]any{
		"model":  model,
		"input":  responsesInput(req.Messages),
		"stream": req.Stream,
	}
	if instructions, input := splitInstructions(out["input"]); instructions != "" {
		out["instructions"] = instructions
		out["input"] = input
	}
	if len(req.Tools) > 0 {
		if tools := responsesTools(req.Tools); len(tools) > 0 {
			out["tools"] = tools
		}
	}
	if toolChoice, ok := responsesToolChoice(req.ToolChoice); ok {
		out["tool_choice"] = toolChoice
	}
	if req.Generation.MaxTokens != nil {
		out["max_output_tokens"] = *req.Generation.MaxTokens
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
	if reasoning := responsesReasoning(req.Generation.Reasoning, o.Reasoning); len(reasoning) > 0 {
		out["reasoning"] = reasoning
	}
	for key, value := range req.Generation.Raw {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	return out
}

func responsesInput(messages []ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		out = append(out, responsesItemsForMessage(msg)...)
	}
	if len(out) == 0 {
		return []map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": ""}}}}
	}
	return out
}

func splitInstructions(raw any) (string, any) {
	items, ok := raw.([]map[string]any)
	if !ok || len(items) == 0 {
		return "", raw
	}
	first := items[0]
	if stringValue(first["role"]) != "system" {
		return "", raw
	}
	instructions := normalizeContent(first["content"])
	if instructions == "" {
		return "", raw
	}
	return instructions, items[1:]
}

func responsesItemsForMessage(msg ir.Message) []map[string]any {
	if msg.Role == ir.RoleTool {
		return responsesToolResultItems(msg)
	}
	if msg.Role == ir.RoleAssistant {
		return responsesAssistantItems(msg)
	}
	role := string(msg.Role)
	if role == "" || role == "developer" {
		role = "user"
	}
	text := msg.Text()
	if text == "" {
		text = ""
	}
	return []map[string]any{{
		"role": role,
		"content": []map[string]any{{
			"type": inputTextType(role),
			"text": text,
		}},
	}}
}

func responsesAssistantItems(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content)+1)
	if reasoning := msg.ReasoningText(); reasoning != "" {
		out = append(out, map[string]any{
			"type":    "reasoning",
			"summary": []map[string]any{{"type": "summary_text", "text": reasoning}},
		})
	}
	if text := msg.Text(); text != "" {
		out = append(out, map[string]any{
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": text,
			}},
		})
	}
	for i, block := range msg.Content {
		if block.Type != ir.BlockToolCall || block.ToolCall == nil || block.ToolCall.Name == "" {
			continue
		}
		callID := block.ToolCall.ID
		if callID == "" {
			callID = fmt.Sprintf("fc_%d_%d", time.Now().UnixNano(), i)
		}
		args := block.ToolCall.Arguments
		if args == "" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"type":      "function_call",
			"id":        callID,
			"call_id":   callID,
			"name":      block.ToolCall.Name,
			"arguments": args,
		})
	}
	return out
}

func responsesToolResultItems(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type != ir.BlockToolResult || block.ToolResult == nil || block.ToolResult.ToolCallID == "" {
			continue
		}
		output := block.ToolResult.Output
		if output == "" {
			output = "{}"
		}
		out = append(out, map[string]any{
			"type":    "function_call_output",
			"call_id": block.ToolResult.ToolCallID,
			"output":  output,
		})
	}
	return out
}

func inputTextType(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

func responsesTools(tools []ir.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if len(tool.Raw) > 0 && stringValue(tool.Raw["type"]) == "function" {
			out = append(out, tool.Raw)
			continue
		}
		if tool.Type != "" && tool.Type != ir.ToolTypeFunction {
			continue
		}
		item := map[string]any{"type": "function"}
		if tool.Function.Name != "" {
			item["name"] = tool.Function.Name
		}
		if tool.Function.Description != "" {
			item["description"] = tool.Function.Description
		}
		if tool.Function.Parameters != nil {
			item["parameters"] = tool.Function.Parameters
		}
		if len(item) > 1 {
			out = append(out, item)
		}
	}
	return out
}

func responsesToolChoice(choice ir.ToolChoice) (any, bool) {
	switch choice.Mode {
	case ir.ToolChoiceAuto, ir.ToolChoiceNone, ir.ToolChoiceRequired:
		return string(choice.Mode), true
	case ir.ToolChoiceFunction:
		if choice.Name == "" {
			return nil, false
		}
		return map[string]any{"type": "function", "name": choice.Name}, true
	default:
		if choice.Raw != nil {
			return choice.Raw, true
		}
		return nil, false
	}
}

func responsesReasoning(reasoning ir.ReasoningConfig, p policy.ReasoningPolicy) map[string]any {
	if !reasoning.HasControls() {
		return nil
	}
	out := map[string]any{}
	if reasoning.Effort != "" && p.AllowReasoningEffort {
		out["effort"] = string(reasoning.Effort)
	}
	if reasoning.Summary != "" {
		out["summary"] = string(reasoning.Summary)
	}
	return out
}
