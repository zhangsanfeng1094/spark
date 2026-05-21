package anthropic_messages

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"spark/internal/compat/ir"
)

type MessagesOutbound struct{}

func (MessagesOutbound) BuildRequest(req ir.Request) map[string]any {
	model := req.Model
	if model == "" {
		model = "unknown"
	}
	maxTokens := defaultMaxTokens
	if req.Generation.MaxTokens != nil && *req.Generation.MaxTokens > 0 {
		maxTokens = *req.Generation.MaxTokens
	}

	out := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   anthropicMessages(req.Messages),
		"stream":     req.Stream,
	}
	if system := anthropicSystem(req.Messages); system != "" {
		out["system"] = system
	}
	if tools := anthropicTools(req.Tools); len(tools) > 0 {
		out["tools"] = tools
	}
	if toolChoice, ok := anthropicToolChoice(req.ToolChoice); ok {
		out["tool_choice"] = toolChoice
	}
	if req.Generation.Temperature != nil {
		out["temperature"] = *req.Generation.Temperature
	}
	if req.Generation.TopP != nil {
		out["top_p"] = *req.Generation.TopP
	}
	if len(req.Generation.Stop) > 0 {
		out["stop_sequences"] = req.Generation.Stop
	} else if stop, ok := req.Generation.Raw["stop"]; ok {
		out["stop_sequences"] = stop
	}
	if thinking, ok := anthropicThinking(req.Generation.Reasoning); ok {
		out["thinking"] = thinking
	}
	return out
}

func anthropicSystem(messages []ir.Message) string {
	parts := make([]string, 0, 2)
	for _, msg := range messages {
		if msg.Role == ir.RoleSystem || msg.Role == ir.RoleDeveloper {
			if text := msg.Text(); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func anthropicMessages(messages []ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == ir.RoleSystem || msg.Role == ir.RoleDeveloper {
			continue
		}
		out = append(out, anthropicMessagesForIRMessage(msg)...)
	}
	if len(out) == 0 {
		return []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": ""}}}}
	}
	return out
}

func anthropicMessagesForIRMessage(msg ir.Message) []map[string]any {
	if msg.Role == ir.RoleTool {
		blocks := anthropicToolResultBlocks(msg)
		if len(blocks) > 0 {
			return []map[string]any{{"role": "user", "content": blocks}}
		}
	}
	role := "user"
	if msg.Role == ir.RoleAssistant {
		role = "assistant"
	}
	content := anthropicContentBlocks(msg.Content, msg.Role)
	if len(content) == 0 {
		content = []map[string]any{{"type": "text", "text": msg.Text()}}
	}
	return []map[string]any{{"role": role, "content": content}}
}

func anthropicContentBlocks(blocks []ir.ContentBlock, role ir.Role) []map[string]any {
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ir.BlockText:
			out = append(out, map[string]any{"type": "text", "text": block.Text})
		case ir.BlockReasoning:
			if role == ir.RoleAssistant && block.Reasoning != nil && block.Reasoning.Text != "" {
				thinking := map[string]any{"type": "thinking", "thinking": block.Reasoning.Text}
				if block.Reasoning.Signature != "" {
					thinking["signature"] = block.Reasoning.Signature
				}
				out = append(out, thinking)
			}
		case ir.BlockToolCall:
			if role == ir.RoleAssistant && block.ToolCall != nil && block.ToolCall.Name != "" {
				out = append(out, anthropicToolUseBlock(*block.ToolCall))
			}
		case ir.BlockToolResult:
			if block.ToolResult != nil {
				out = append(out, anthropicToolResultBlock(*block.ToolResult))
			}
		case ir.BlockImage:
			if block.Image != nil {
				if image := anthropicImageBlock(*block.Image); len(image) > 0 {
					out = append(out, image)
				}
			}
		case ir.BlockDocument:
			if block.Document != nil && block.Document.Text != "" {
				out = append(out, map[string]any{"type": "text", "text": block.Document.Text})
			}
		}
	}
	return out
}

func anthropicToolResultBlocks(msg ir.Message) []map[string]any {
	out := make([]map[string]any, 0, len(msg.Content))
	for _, block := range msg.Content {
		if block.Type == ir.BlockToolResult && block.ToolResult != nil {
			out = append(out, anthropicToolResultBlock(*block.ToolResult))
		}
	}
	return out
}

func anthropicToolUseBlock(tc ir.ToolCall) map[string]any {
	id := tc.ID
	if id == "" {
		id = "toolu_unknown"
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    id,
		"name":  tc.Name,
		"input": parseJSONMap(tc.Arguments),
	}
}

func anthropicToolResultBlock(result ir.ToolResult) map[string]any {
	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": result.ToolCallID,
		"content":     result.Output,
	}
	if result.IsError {
		block["is_error"] = true
	}
	return block
}

func anthropicImageBlock(image ir.ImageBlock) map[string]any {
	if image.URL != "" {
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "url",
				"url":  image.URL,
			},
		}
	}
	if len(image.Data) > 0 {
		mediaType := image.MimeType
		if mediaType == "" {
			mediaType = "image/png"
		}
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       base64.StdEncoding.EncodeToString(image.Data),
			},
		}
	}
	return nil
}

func anthropicTools(tools []ir.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if len(tool.Raw) > 0 && tool.Type != ir.ToolTypeFunction {
			out = append(out, tool.Raw)
			continue
		}
		if tool.Type != "" && tool.Type != ir.ToolTypeFunction {
			continue
		}
		if tool.Function.Name == "" {
			continue
		}
		inputSchema := tool.Function.Parameters
		if inputSchema == nil {
			inputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		outTool := map[string]any{
			"name":         tool.Function.Name,
			"input_schema": inputSchema,
		}
		if tool.Function.Description != "" {
			outTool["description"] = tool.Function.Description
		}
		out = append(out, outTool)
	}
	return out
}

func anthropicToolChoice(choice ir.ToolChoice) (any, bool) {
	switch choice.Mode {
	case ir.ToolChoiceAuto:
		return map[string]any{"type": "auto"}, true
	case ir.ToolChoiceNone:
		return map[string]any{"type": "none"}, true
	case ir.ToolChoiceRequired:
		return map[string]any{"type": "any"}, true
	case ir.ToolChoiceFunction:
		if choice.Name == "" {
			return nil, false
		}
		return map[string]any{"type": "tool", "name": choice.Name}, true
	default:
		if choice.Raw != nil {
			return choice.Raw, true
		}
		return nil, false
	}
}

func anthropicThinking(reasoning ir.ReasoningConfig) (map[string]any, bool) {
	if raw := mapValue(reasoning.Raw["thinking"]); len(raw) > 0 {
		return raw, true
	}
	if reasoning.Enabled != nil && !*reasoning.Enabled {
		return map[string]any{"type": "disabled"}, true
	}
	budget := 0
	if reasoning.BudgetTokens != nil {
		budget = *reasoning.BudgetTokens
	} else {
		budget = reasoningBudget(reasoning.Effort)
	}
	if budget <= 0 {
		return nil, false
	}
	return map[string]any{"type": "enabled", "budget_tokens": budget}, true
}

func jsonString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
