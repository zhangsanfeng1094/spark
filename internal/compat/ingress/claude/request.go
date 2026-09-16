package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/compat/engine"
	"spark/internal/compat/ingress/media"
	"spark/internal/config"
)

// ToBifrostRequest converts an Anthropic Messages request map to a BifrostChatRequest.
func ToBifrostRequest(raw map[string]any, profile *config.Profile) *schemas.BifrostChatRequest {
	provider, _, _ := engine.MapProfileToProvider(profile)

	model := stringValue(raw["model"])
	if model == "" && profile != nil {
		model = profile.DefaultModel
	}
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	messages := convertAnthropicMessages(raw)
	tools := convertAnthropicTools(raw["tools"])
	params := convertAnthropicParams(raw)
	if len(tools) > 0 {
		params.Tools = tools
	}

	return &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    messages,
		Params:   params,
	}
}

func convertAnthropicMessages(raw map[string]any) []schemas.ChatMessage {
	messages := make([]schemas.ChatMessage, 0, 8)

	// System prompt
	if sys := raw["system"]; sys != nil {
		sysText := normalizeAnthropicContent(sys)
		if sysText != "" {
			messages = append(messages, schemas.ChatMessage{
				Role: schemas.ChatMessageRoleSystem,
				Content: &schemas.ChatMessageContent{
					ContentStr: &sysText,
				},
			})
		}
	}

	rawMsgs, ok := raw["messages"].([]any)
	if !ok {
		return messages
	}

	// First pass: collect tool call id -> tool name mappings from assistant tool_use blocks
	toolCallNames := make(map[string]string)
	for _, item := range rawMsgs {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var parts []any
		if arr, ok := msg["content"].([]any); ok {
			parts = arr
		} else if m, ok := msg["content"].(map[string]any); ok {
			parts = []any{m}
		}
		for _, part := range parts {
			if p, ok := part.(map[string]any); ok {
				if stringValue(p["type"]) == "tool_use" {
					id := stringValue(p["id"])
					name := stringValue(p["name"])
					if id != "" && name != "" {
						toolCallNames[id] = name
					}
				}
			}
		}
	}

	for _, item := range rawMsgs {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		roleStr := stringValue(msg["role"])
		var role schemas.ChatMessageRole
		if roleStr == "assistant" {
			role = schemas.ChatMessageRoleAssistant
		} else {
			role = schemas.ChatMessageRoleUser
		}

		content := msg["content"]
		switch c := content.(type) {
		case string:
			messages = append(messages, schemas.ChatMessage{
				Role: role,
				Content: &schemas.ChatMessageContent{
					ContentStr: &c,
				},
			})
		case map[string]any:
			handleBlockList(role, []any{c}, toolCallNames, &messages)
		case []any:
			handleBlockList(role, c, toolCallNames, &messages)
		}
	}

	if len(messages) == 0 {
		empty := ""
		messages = append(messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &empty,
			},
		})
	}
	return messages
}

func handleBlockList(role schemas.ChatMessageRole, parts []any, toolCallNames map[string]string, messages *[]schemas.ChatMessage) {
	if role == schemas.ChatMessageRoleAssistant {
		var textBuf strings.Builder
		var reasoningText string
		var toolCalls []schemas.ChatAssistantMessageToolCall

		for idx, part := range parts {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			pType := stringValue(p["type"])
			switch pType {
			case "text":
				textBuf.WriteString(stringValue(p["text"]))
			case "thinking":
				reasoningText = stringValue(p["thinking"])
			case "tool_use":
				callID := stringValue(p["id"])
				name := stringValue(p["name"])
				var argsStr string
				if inputObj, ok := p["input"]; ok {
					if b, err := json.Marshal(inputObj); err == nil {
						argsStr = string(b)
					}
				}
				callType := "function"
				toolCalls = append(toolCalls, schemas.ChatAssistantMessageToolCall{
					Index: uint16(idx),
					ID:    &callID,
					Type:  &callType,
					Function: schemas.ChatAssistantMessageToolCallFunction{
						Name:      &name,
						Arguments: argsStr,
					},
				})
			}
		}

		var reasoningPtr *string
		if reasoningText != "" {
			reasoningPtr = &reasoningText
		}
		fullText := textBuf.String()
		var contentObj *schemas.ChatMessageContent
		if fullText != "" {
			contentObj = &schemas.ChatMessageContent{ContentStr: &fullText}
		}
		if len(toolCalls) > 0 || reasoningPtr != nil {
			*messages = append(*messages, schemas.ChatMessage{
				Role:    role,
				Content: contentObj,
				ChatAssistantMessage: &schemas.ChatAssistantMessage{
					Reasoning: reasoningPtr,
					ToolCalls: toolCalls,
				},
			})
		} else if contentObj != nil {
			*messages = append(*messages, schemas.ChatMessage{
				Role:    role,
				Content: contentObj,
			})
		}
		return
	}

	// User role: can contain text, image, document, and tool_result blocks
	var userBlocks []schemas.ChatContentBlock
	var toolResults []parsedToolResult

	for _, part := range parts {
		p, ok := part.(map[string]any)
		if !ok {
			if str, ok := part.(string); ok && str != "" {
				userBlocks = append(userBlocks, schemas.ChatContentBlock{
					Type: schemas.ChatContentBlockTypeText,
					Text: &str,
				})
			}
			continue
		}

		pType := stringValue(p["type"])
		cc := parseAnthropicCacheControl(p)

		switch pType {
		case "tool_result":
			toolResults = append(toolResults, parseAnthropicToolResult(p, toolCallNames))
		case "text":
			txt := stringValue(p["text"])
			if txt != "" {
				userBlocks = append(userBlocks, schemas.ChatContentBlock{
					Type:         schemas.ChatContentBlockTypeText,
					Text:         &txt,
					CacheControl: cc,
				})
			}
		case "image":
			if img := media.ParseImage(p); img != nil {
				userBlocks = append(userBlocks, img.ToChatContentBlock(cc))
			}
		case "document":
			if doc := media.ParseDocument(p, cc); doc != nil {
				userBlocks = append(userBlocks, *doc)
			}
		default:
			if img := media.ParseImage(p); img != nil {
				userBlocks = append(userBlocks, img.ToChatContentBlock(cc))
			} else if doc := media.ParseDocument(p, cc); doc != nil {
				userBlocks = append(userBlocks, *doc)
			} else if txt := stringValue(p["text"]); txt != "" {
				userBlocks = append(userBlocks, schemas.ChatContentBlock{
					Type:         schemas.ChatContentBlockTypeText,
					Text:         &txt,
					CacheControl: cc,
				})
			}
		}
	}

	// 1. Emit normal user blocks if any
	if len(userBlocks) > 0 {
		hasNonText := false
		for _, b := range userBlocks {
			if b.Type != schemas.ChatContentBlockTypeText {
				hasNonText = true
				break
			}
		}
		if !hasNonText && len(userBlocks) == 1 && userBlocks[0].CacheControl == nil {
			*messages = append(*messages, schemas.ChatMessage{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentStr: userBlocks[0].Text,
				},
			})
		} else {
			*messages = append(*messages, schemas.ChatMessage{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentBlocks: userBlocks,
				},
			})
		}
	}

	// 2. Emit tool messages (multimodal blocks attached directly to the tool turn)
	for _, tr := range toolResults {
		var msgContent *schemas.ChatMessageContent
		if len(tr.images) > 0 {
			blocks := make([]schemas.ChatContentBlock, 0, 1+len(tr.images))
			if tr.plainText != "" && tr.plainText != "{}" {
				blocks = append(blocks, schemas.ChatContentBlock{
					Type: schemas.ChatContentBlockTypeText,
					Text: &tr.plainText,
				})
			}
			for _, img := range tr.images {
				blocks = append(blocks, img.ToChatContentBlock(nil))
			}
			msgContent = &schemas.ChatMessageContent{
				ContentBlocks: blocks,
			}
		} else {
			msgContent = &schemas.ChatMessageContent{
				ContentStr: &tr.plainText,
			}
		}

		toolMsg := schemas.ChatMessage{
			Role:    schemas.ChatMessageRoleTool,
			Content: msgContent,
			ChatToolMessage: &schemas.ChatToolMessage{
				ToolCallID: &tr.callID,
				IsError:    tr.isError,
			},
		}
		if tr.toolName != "" {
			toolMsg.Name = &tr.toolName
		}
		*messages = append(*messages, toolMsg)
	}

	// If no userBlocks and no toolResults were produced, but parts was not empty
	if len(userBlocks) == 0 && len(toolResults) == 0 && len(parts) > 0 {
		empty := ""
		*messages = append(*messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &empty,
			},
		})
	}
}

func parseAnthropicCacheControl(p map[string]any) *schemas.CacheControl {
	if cc, ok := p["cache_control"].(map[string]any); ok {
		if t := stringValue(cc["type"]); t != "" {
			return &schemas.CacheControl{Type: schemas.CacheControlType(t)}
		}
	}
	return nil
}

type parsedToolResult struct {
	callID    string
	toolName  string
	isError   *bool
	plainText string
	images    []*media.ExtractedImage
}

func parseAnthropicToolResult(p map[string]any, toolCallNames map[string]string) parsedToolResult {
	callID := stringValue(p["tool_use_id"])
	if callID == "" {
		callID = stringValue(p["id"])
	}

	toolName := ""
	if callID != "" {
		toolName = toolCallNames[callID]
	}
	if toolName == "" {
		toolName = stringValue(p["tool_name"])
	}
	if toolName == "" {
		toolName = stringValue(p["name"])
	}

	var isErrPtr *bool
	if isErr, ok := p["is_error"].(bool); ok && isErr {
		isErrPtr = &isErr
	}

	contentRaw := p["content"]
	cleanText, images := media.ExtractToolImagesAndText(contentRaw)
	if cleanText == "" && len(images) == 0 {
		cleanText = "{}"
	}

	return parsedToolResult{
		callID:    callID,
		toolName:  toolName,
		isError:   isErrPtr,
		plainText: cleanText,
		images:    images,
	}
}

func convertAnthropicTools(raw any) []schemas.ChatTool {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	tools := make([]schemas.ChatTool, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(m["name"])
		desc := stringValue(m["description"])
		if name == "" {
			continue
		}

		var paramObj *schemas.ToolFunctionParameters
		if schema, ok := m["input_schema"]; ok {
			if b, err := json.Marshal(schema); err == nil {
				var p schemas.ToolFunctionParameters
				if err := json.Unmarshal(b, &p); err == nil {
					paramObj = &p
				}
			}
		}

		var descPtr *string
		if desc != "" {
			descPtr = &desc
		}
		tools = append(tools, schemas.ChatTool{
			Type: schemas.ChatToolTypeFunction,
			Function: &schemas.ChatToolFunction{
				Name:        name,
				Description: descPtr,
				Parameters:  paramObj,
			},
		})
	}
	return tools
}

func convertAnthropicParams(raw map[string]any) *schemas.ChatParameters {
	params := &schemas.ChatParameters{}
	if maxTokens, ok := intValue(raw["max_tokens"]); ok && maxTokens > 0 {
		params.MaxCompletionTokens = &maxTokens
	}
	if temp, ok := float64Value(raw["temperature"]); ok {
		params.Temperature = &temp
	}
	if topP, ok := float64Value(raw["top_p"]); ok {
		params.TopP = &topP
	}
	if topK, ok := intValue(raw["top_k"]); ok && topK > 0 {
		params.TopK = &topK
	}
	if isStream := boolValue(raw["stream"]); isStream {
		params.StreamOptions = &schemas.ChatStreamOptions{
			IncludeUsage: bifrost.Ptr(true),
		}
	}
	if thinking, ok := raw["thinking"].(map[string]any); ok {
		tType := stringValue(thinking["type"])
		if tType == "enabled" || tType == "adaptive" {
			enabled := true
			reasoning := &schemas.ChatReasoning{
				Enabled: &enabled,
			}
			if budget, ok := intValue(thinking["budget_tokens"]); ok && budget > 0 {
				reasoning.MaxTokens = &budget
			}
			if tType == "adaptive" {
				if output, ok := raw["output_config"].(map[string]any); ok {
					if effort := stringValue(output["effort"]); effort != "" {
						reasoning.Effort = &effort
					}
				}
			}
			params.Reasoning = reasoning
		}
	}
	return params
}

func normalizeAnthropicContent(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var sb strings.Builder
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if t := stringValue(m["text"]); t != "" {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	default:
		if v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func float64Value(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
