package openai

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

// ToBifrostRequest converts an OpenAI Chat Completion request map/body to a BifrostChatRequest.
func ToBifrostRequest(raw map[string]any, rawBody []byte, profile *config.Profile) *schemas.BifrostChatRequest {
	reqModel := stringValue(raw["model"])
	preferred := ""
	if profile != nil {
		preferred = profile.DefaultModel
	}
	model := engine.ResolveProfileModel(reqModel, profile, preferred)
	if model == "" {
		model = "gpt-4o"
	}

	provider, _, _ := engine.MapProfileAndModelToProvider(profile, model)

	messages := convertOpenAIMessages(raw["messages"])
	params := convertOpenAIParams(raw, rawBody)

	return &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    messages,
		Params:   params,
	}
}

type openAIToolResult struct {
	callID    string
	toolName  string
	plainText string
	images    []*media.ExtractedImage
}

func convertOpenAIMessages(raw any) []schemas.ChatMessage {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var rawList []any
	if err := json.Unmarshal(data, &rawList); err != nil || len(rawList) == 0 {
		return nil
	}

	// 1. First pass: collect tool call id -> tool name mappings
	toolCallNames := make(map[string]string)
	for _, item := range rawList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tcList, ok := m["tool_calls"].([]any); ok {
			for _, tcItem := range tcList {
				if tcMap, ok := tcItem.(map[string]any); ok {
					id := stringValue(tcMap["id"])
					var name string
					if fn, ok := tcMap["function"].(map[string]any); ok {
						name = stringValue(fn["name"])
					}
					if name == "" {
						name = stringValue(tcMap["name"])
					}
					if id != "" && name != "" {
						toolCallNames[id] = name
					}
				}
			}
		}
	}

	messages := make([]schemas.ChatMessage, 0, len(rawList))
	var pendingToolResults []openAIToolResult

	flushPendingToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		// 1. Emit all tool messages contiguously (multimodal blocks attached directly to the tool turn)
		for _, tr := range pendingToolResults {
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
				},
			}
			if tr.toolName != "" {
				toolMsg.Name = &tr.toolName
			}
			messages = append(messages, toolMsg)
		}

		pendingToolResults = nil
	}

	for _, item := range rawList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(stringValue(m["role"]))
		switch role {
		case "tool":
			callID := stringValue(m["tool_call_id"])
			if callID == "" {
				callID = stringValue(m["id"])
			}
			toolName := toolCallNames[callID]
			if toolName == "" {
				toolName = stringValue(m["name"])
			}
			outputRaw := m["content"]
			if outputRaw == nil {
				outputRaw = m["output"]
			}
			cleanText, imgs := media.ExtractToolImagesAndText(outputRaw)
			pendingToolResults = append(pendingToolResults, openAIToolResult{
				callID:    callID,
				toolName:  toolName,
				plainText: cleanText,
				images:    imgs,
			})
			continue

		default:
			flushPendingToolResults()
		}

		switch role {
		case "system", "developer":
			contentStr := normalizeContent(m["content"])
			if contentStr != "" {
				msg := schemas.ChatMessage{
					Role: schemas.ChatMessageRoleSystem,
					Content: &schemas.ChatMessageContent{
						ContentStr: &contentStr,
					},
				}
				if name := stringValue(m["name"]); name != "" {
					msg.Name = &name
				}
				messages = append(messages, msg)
			}

		case "assistant":
			var toolCalls []schemas.ChatAssistantMessageToolCall
			if tcRaw, ok := m["tool_calls"].([]any); ok {
				toolCalls = getAssistantToolCalls(tcRaw)
			}
			var reasoningPtr *string
			if r := getReasoningContent(m); r != "" {
				reasoningPtr = &r
			}
			contentStr := normalizeContent(m["content"])
			var contentObj *schemas.ChatMessageContent
			if contentStr != "" {
				contentObj = &schemas.ChatMessageContent{ContentStr: &contentStr}
			}
			if len(toolCalls) > 0 || reasoningPtr != nil {
				messages = append(messages, schemas.ChatMessage{
					Role:    schemas.ChatMessageRoleAssistant,
					Content: contentObj,
					ChatAssistantMessage: &schemas.ChatAssistantMessage{
						Reasoning: reasoningPtr,
						ToolCalls: toolCalls,
					},
				})
			} else if contentObj != nil {
				messages = append(messages, schemas.ChatMessage{
					Role:    schemas.ChatMessageRoleAssistant,
					Content: contentObj,
				})
			}

		default:
			// User role
			userMsg := parseOpenAIUserMessage(m["content"])
			if name := stringValue(m["name"]); name != "" {
				userMsg.Name = &name
			}
			messages = append(messages, userMsg)
		}
	}

	flushPendingToolResults()

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

func parseOpenAIUserMessage(contentRaw any) schemas.ChatMessage {
	if contentRaw == nil {
		empty := ""
		return schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &empty,
			},
		}
	}

	switch c := contentRaw.(type) {
	case string:
		trimmed := strings.TrimSpace(c)
		if strings.HasPrefix(trimmed, "data:image/") {
			if img := media.ParseImage(trimmed); img != nil {
				return schemas.ChatMessage{
					Role: schemas.ChatMessageRoleUser,
					Content: &schemas.ChatMessageContent{
						ContentBlocks: []schemas.ChatContentBlock{img.ToChatContentBlock(nil)},
					},
				}
			}
		}
		return schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &c,
			},
		}

	case map[string]any:
		return parseOpenAIUserBlocks([]any{c})

	case []any:
		return parseOpenAIUserBlocks(c)

	default:
		s := fmt.Sprintf("%v", contentRaw)
		return schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &s,
			},
		}
	}
}

func parseOpenAIUserBlocks(parts []any) schemas.ChatMessage {
	var blocks []schemas.ChatContentBlock

	for _, part := range parts {
		switch p := part.(type) {
		case string:
			trimmed := strings.TrimSpace(p)
			if strings.HasPrefix(trimmed, "data:image/") {
				if img := media.ParseImage(trimmed); img != nil {
					blocks = append(blocks, img.ToChatContentBlock(nil))
					continue
				}
			}
			if p != "" {
				blocks = append(blocks, schemas.ChatContentBlock{
					Type: schemas.ChatContentBlockTypeText,
					Text: &p,
				})
			}

		case map[string]any:
			pType := stringValue(p["type"])
			switch pType {
			case "input_text", "text":
				txt := stringValue(p["text"])
				if txt != "" {
					blocks = append(blocks, schemas.ChatContentBlock{
						Type: schemas.ChatContentBlockTypeText,
						Text: &txt,
					})
				}
			case "input_image", "image_url", "image":
				if img := media.ParseImage(p); img != nil {
					blocks = append(blocks, img.ToChatContentBlock(nil))
				}
			case "input_file", "document", "file":
				if doc := media.ParseDocument(p, nil); doc != nil {
					blocks = append(blocks, *doc)
				}
			default:
				if img := media.ParseImage(p); img != nil {
					blocks = append(blocks, img.ToChatContentBlock(nil))
				} else if doc := media.ParseDocument(p, nil); doc != nil {
					blocks = append(blocks, *doc)
				} else if txt := stringValue(p["text"]); txt != "" {
					blocks = append(blocks, schemas.ChatContentBlock{
						Type: schemas.ChatContentBlockTypeText,
						Text: &txt,
					})
				}
			}
		}
	}

	if len(blocks) == 0 {
		empty := ""
		return schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &empty,
			},
		}
	}

	hasNonText := false
	for _, b := range blocks {
		if b.Type != schemas.ChatContentBlockTypeText {
			hasNonText = true
			break
		}
	}

	if !hasNonText && len(blocks) == 1 {
		return schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: blocks[0].Text,
			},
		}
	}

	return schemas.ChatMessage{
		Role: schemas.ChatMessageRoleUser,
		Content: &schemas.ChatMessageContent{
			ContentBlocks: blocks,
		},
	}
}

func getAssistantToolCalls(raw []any) []schemas.ChatAssistantMessageToolCall {
	if len(raw) == 0 {
		return nil
	}
	calls := make([]schemas.ChatAssistantMessageToolCall, 0, len(raw))
	for idx, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		callID := stringValue(m["id"])
		name := ""
		args := ""
		if fn, ok := m["function"].(map[string]any); ok {
			name = stringValue(fn["name"])
			args = stringValue(fn["arguments"])
		}
		if name == "" {
			name = stringValue(m["name"])
		}
		if args == "" {
			args = stringValue(m["arguments"])
		}
		callType := "function"
		calls = append(calls, schemas.ChatAssistantMessageToolCall{
			Index: uint16(idx),
			ID:    &callID,
			Type:  &callType,
			Function: schemas.ChatAssistantMessageToolCallFunction{
				Name:      &name,
				Arguments: args,
			},
		})
	}
	return calls
}

func normalizeContent(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var sb strings.Builder
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	case map[string]any:
		b, _ := json.Marshal(c)
		return string(b)
	default:
		if v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
}

func getReasoningContent(m map[string]any) string {
	if r := stringValue(m["reasoning_content"]); r != "" {
		return r
	}
	if r := stringValue(m["reasoning"]); r != "" {
		return r
	}
	if r := stringValue(m["thought"]); r != "" {
		return r
	}
	if r := stringValue(m["thoughts"]); r != "" {
		return r
	}
	if r := stringValue(m["think"]); r != "" {
		return r
	}
	if r := stringValue(m["thinking"]); r != "" {
		return r
	}
	if r := stringValue(m["reasoning_text"]); r != "" {
		return r
	}
	return ""
}

func convertOpenAIParams(raw map[string]any, rawBody []byte) *schemas.ChatParameters {
	params := &schemas.ChatParameters{}
	if len(rawBody) > 0 {
		_ = schemas.Unmarshal(rawBody, params)
	}

	// Fallback for max_tokens when max_completion_tokens is not populated
	if params.MaxCompletionTokens == nil {
		if mt, ok := raw["max_tokens"].(float64); ok && mt > 0 {
			params.MaxCompletionTokens = bifrost.Ptr(int(mt))
		}
	}

	isStream := boolValue(raw["stream"])
	if isStream {
		if params.StreamOptions == nil {
			params.StreamOptions = &schemas.ChatStreamOptions{
				IncludeUsage: bifrost.Ptr(true),
			}
		} else if params.StreamOptions.IncludeUsage == nil {
			params.StreamOptions.IncludeUsage = bifrost.Ptr(true)
		}
	}

	return params
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
