package codex

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

// ToBifrostRequest converts a Codex /v1/responses raw request body map into a BifrostChatRequest.
func ToBifrostRequest(raw map[string]any, profile *config.Profile) *schemas.BifrostChatRequest {
	provider, _, _ := engine.MapProfileToProvider(profile)

	model := stringValue(raw["model"])
	if model == "" && profile != nil {
		model = profile.DefaultModel
	}
	if model == "" {
		model = "gpt-4o"
	}

	messages := convertCodexInput(raw)
	tools := convertCodexTools(raw["tools"])
	params := convertCodexParams(raw)
	if len(tools) > 0 {
		params.Tools = tools
	}

	req := &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    messages,
		Params:   params,
	}
	return req
}

func convertCodexInput(raw map[string]any) []schemas.ChatMessage {
	messages := make([]schemas.ChatMessage, 0, 8)

	// Instructions -> system message
	if instructions := stringValue(raw["instructions"]); instructions != "" {
		messages = append(messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleSystem,
			Content: &schemas.ChatMessageContent{
				ContentStr: &instructions,
			},
		})
	}

	input := raw["input"]
	if input == nil {
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

	switch v := input.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "data:image/") {
			if img := media.ParseImage(trimmed); img != nil {
				messages = append(messages, schemas.ChatMessage{
					Role: schemas.ChatMessageRoleUser,
					Content: &schemas.ChatMessageContent{
						ContentBlocks: []schemas.ChatContentBlock{img.ToChatContentBlock(nil)},
					},
				})
				return messages
			}
		}
		messages = append(messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: &v,
			},
		})
	case map[string]any:
		return convertCodexInputList([]any{v}, messages)
	case []any:
		return convertCodexInputList(v, messages)
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

type codexToolResult struct {
	callID    string
	toolName  string
	plainText string
	images    []*media.ExtractedImage
}

func convertCodexInputList(inputList []any, messages []schemas.ChatMessage) []schemas.ChatMessage {
	// Pre-scan tool call IDs -> tool names
	toolCallNames := make(map[string]string)
	for _, item := range inputList {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := stringValue(msg["type"])
		if itemType == "function_call" {
			callID := stringValue(msg["call_id"])
			if callID == "" {
				callID = stringValue(msg["id"])
			}
			name := stringValue(msg["name"])
			if callID != "" && name != "" {
				toolCallNames[callID] = name
			}
		} else if itemType == "message" || msg["role"] != nil {
			if tcList, ok := msg["tool_calls"].([]any); ok {
				for _, tcItem := range tcList {
					if tcMap, ok := tcItem.(map[string]any); ok {
						id := stringValue(tcMap["id"])
						var name string
						if fn, ok := tcMap["function"].(map[string]any); ok {
							name = stringValue(fn["name"])
						}
						if id != "" && name != "" {
							toolCallNames[id] = name
						}
					}
				}
			}
		}
	}

	var pendingCalls []schemas.ChatAssistantMessageToolCall
	var pendingReasoning string
	var pendingToolResults []codexToolResult

	flushPendingCalls := func() {
		if len(pendingCalls) == 0 {
			return
		}
		var lastAssistant *schemas.ChatMessage
		if len(messages) > 0 && messages[len(messages)-1].Role == schemas.ChatMessageRoleAssistant {
			lastAssistant = &messages[len(messages)-1]
		}

		if lastAssistant != nil {
			if lastAssistant.ChatAssistantMessage == nil {
				lastAssistant.ChatAssistantMessage = &schemas.ChatAssistantMessage{}
			}
			if lastAssistant.ChatAssistantMessage.Reasoning == nil && pendingReasoning != "" {
				reasoning := pendingReasoning
				lastAssistant.ChatAssistantMessage.Reasoning = &reasoning
			}
			lastAssistant.ChatAssistantMessage.ToolCalls = append(lastAssistant.ChatAssistantMessage.ToolCalls, pendingCalls...)
		} else {
			var reasoningPtr *string
			if pendingReasoning != "" {
				reasoning := pendingReasoning
				reasoningPtr = &reasoning
			}
			messages = append(messages, schemas.ChatMessage{
				Role: schemas.ChatMessageRoleAssistant,
				ChatAssistantMessage: &schemas.ChatAssistantMessage{
					Reasoning: reasoningPtr,
					ToolCalls: pendingCalls,
				},
			})
		}
		pendingCalls = nil
		pendingReasoning = ""
	}

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

	ensureToolCallPreceding := func(callID string, toolName string) {
		if callID == "" {
			return
		}
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == schemas.ChatMessageRoleUser {
				break
			}
			if messages[i].Role == schemas.ChatMessageRoleAssistant && messages[i].ChatAssistantMessage != nil {
				for _, tc := range messages[i].ChatAssistantMessage.ToolCalls {
					if tc.ID != nil && *tc.ID == callID {
						return
					}
				}
			}
		}

		// Not found in preceding assistant messages: synthesize it.
		callType := "function"
		name := toolName
		if name == "" {
			name = "unknown_tool"
		}
		syntheticCall := schemas.ChatAssistantMessageToolCall{
			ID:   &callID,
			Type: &callType,
			Function: schemas.ChatAssistantMessageToolCallFunction{
				Name:      &name,
				Arguments: "{}",
			},
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == schemas.ChatMessageRoleAssistant {
			if messages[len(messages)-1].ChatAssistantMessage == nil {
				messages[len(messages)-1].ChatAssistantMessage = &schemas.ChatAssistantMessage{}
			}
			messages[len(messages)-1].ChatAssistantMessage.ToolCalls = append(messages[len(messages)-1].ChatAssistantMessage.ToolCalls, syntheticCall)
		} else {
			messages = append(messages, schemas.ChatMessage{
				Role: schemas.ChatMessageRoleAssistant,
				ChatAssistantMessage: &schemas.ChatAssistantMessage{
					ToolCalls: []schemas.ChatAssistantMessageToolCall{syntheticCall},
				},
			})
		}
	}

	for _, item := range inputList {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := stringValue(msg["type"])
		switch itemType {
		case "reasoning":
			flushPendingToolResults()
			if reasoning := getReasoningContent(msg); reasoning != "" {
				if pendingReasoning != "" {
					pendingReasoning += "\n"
				}
				pendingReasoning += reasoning
			}
			continue

		case "function_call":
			flushPendingToolResults()
			callID := stringValue(msg["call_id"])
			if callID == "" {
				callID = stringValue(msg["id"])
			}
			if callID == "" {
				continue
			}
			name := stringValue(msg["name"])
			args := stringValue(msg["arguments"])
			if args == "" {
				args = "{}"
			}
			callType := "function"
			pendingCalls = append(pendingCalls, schemas.ChatAssistantMessageToolCall{
				Index: uint16(len(pendingCalls)),
				ID:    &callID,
				Type:  &callType,
				Function: schemas.ChatAssistantMessageToolCallFunction{
					Name:      &name,
					Arguments: args,
				},
			})
			continue

		case "function_call_output":
			flushPendingCalls()

			callID := stringValue(msg["call_id"])
			if callID == "" {
				callID = stringValue(msg["tool_call_id"])
			}
			if callID == "" {
				continue
			}
			toolName := toolCallNames[callID]
			if toolName == "" {
				toolName = stringValue(msg["name"])
			}
			ensureToolCallPreceding(callID, toolName)

			outputRaw := msg["output"]
			if outputRaw == nil {
				outputRaw = msg["content"]
			}
			cleanText, imgs := media.ExtractToolImagesAndText(outputRaw)

			pendingToolResults = append(pendingToolResults, codexToolResult{
				callID:    callID,
				toolName:  toolName,
				plainText: cleanText,
				images:    imgs,
			})
			continue
		}

		// Regular message item (role = user, assistant, system, tool)
		role := stringValue(msg["role"])
		if role == "tool" {
			flushPendingCalls()

			callID := stringValue(msg["tool_call_id"])
			if callID == "" {
				callID = stringValue(msg["call_id"])
			}
			toolName := toolCallNames[callID]
			if toolName == "" {
				toolName = stringValue(msg["name"])
			}
			if callID != "" {
				ensureToolCallPreceding(callID, toolName)
			}
			outputRaw := msg["content"]
			if outputRaw == nil {
				outputRaw = msg["output"]
			}
			cleanText, imgs := media.ExtractToolImagesAndText(outputRaw)

			pendingToolResults = append(pendingToolResults, codexToolResult{
				callID:    callID,
				toolName:  toolName,
				plainText: cleanText,
				images:    imgs,
			})
			continue
		}

		// Non-tool message arrives: flush pending calls and tool results
		flushPendingCalls()
		flushPendingToolResults()

		var chatRole schemas.ChatMessageRole
		switch role {
		case "system", "developer":
			chatRole = schemas.ChatMessageRoleSystem
		case "assistant":
			chatRole = schemas.ChatMessageRoleAssistant
		default:
			chatRole = schemas.ChatMessageRoleUser
		}

		if chatRole == schemas.ChatMessageRoleAssistant {
			reasoning := getReasoningContent(msg)
			if reasoning == "" && pendingReasoning != "" {
				reasoning = pendingReasoning
				pendingReasoning = ""
			}
			var reasoningPtr *string
			if reasoning != "" {
				reasoningPtr = &reasoning
			}

			toolCalls := getAssistantToolCalls(msg["tool_calls"])
			contentStr := normalizeContent(msg["content"])
			var contentObj *schemas.ChatMessageContent
			if contentStr != "" {
				contentObj = &schemas.ChatMessageContent{ContentStr: &contentStr}
			}

			if len(toolCalls) > 0 || reasoningPtr != nil {
				messages = append(messages, schemas.ChatMessage{
					Role:    chatRole,
					Content: contentObj,
					ChatAssistantMessage: &schemas.ChatAssistantMessage{
						Reasoning: reasoningPtr,
						ToolCalls: toolCalls,
					},
				})
			} else if contentObj != nil {
				messages = append(messages, schemas.ChatMessage{
					Role:    chatRole,
					Content: contentObj,
				})
			}
		} else if chatRole == schemas.ChatMessageRoleSystem {
			contentStr := normalizeContent(msg["content"])
			if contentStr != "" {
				messages = append(messages, schemas.ChatMessage{
					Role: chatRole,
					Content: &schemas.ChatMessageContent{
						ContentStr: &contentStr,
					},
				})
			}
		} else {
			// User role: can be string, or array/map with text, image, document
			userMsg := parseCodexUserMessage(msg["content"])
			messages = append(messages, userMsg)
		}
	}

	flushPendingCalls()
	flushPendingToolResults()

	return messages
}

func parseCodexUserMessage(contentRaw any) schemas.ChatMessage {
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
		return parseCodexUserBlocks([]any{c})

	case []any:
		return parseCodexUserBlocks(c)

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

func parseCodexUserBlocks(parts []any) schemas.ChatMessage {
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

func getAssistantToolCalls(raw any) []schemas.ChatAssistantMessageToolCall {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	calls := make([]schemas.ChatAssistantMessageToolCall, 0, len(items))
	for idx, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		callID := stringValue(m["id"])
		if callID == "" {
			callID = stringValue(m["call_id"])
		}
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

func convertCodexTools(raw any) []schemas.ChatTool {
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
		name := ""
		desc := ""
		var paramObj *schemas.ToolFunctionParameters
		var strictPtr *bool

		if fn, ok := m["function"].(map[string]any); ok {
			name = stringValue(fn["name"])
			desc = stringValue(fn["description"])
			if s, ok := fn["strict"].(bool); ok {
				strictPtr = &s
			}
			if params, ok := fn["parameters"]; ok {
				if b, err := json.Marshal(params); err == nil {
					var p schemas.ToolFunctionParameters
					if err := json.Unmarshal(b, &p); err == nil {
						paramObj = &p
					}
				}
			}
		} else {
			name = stringValue(m["name"])
			desc = stringValue(m["description"])
			if s, ok := m["strict"].(bool); ok {
				strictPtr = &s
			}
			if params, ok := m["parameters"]; ok {
				if b, err := json.Marshal(params); err == nil {
					var p schemas.ToolFunctionParameters
					if err := json.Unmarshal(b, &p); err == nil {
						paramObj = &p
					}
				}
			}
		}

		if name == "" {
			continue
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
				Strict:      strictPtr,
			},
		})
	}
	return tools
}

func convertCodexParams(raw map[string]any) *schemas.ChatParameters {
	params := &schemas.ChatParameters{}
	if maxTokens, ok := intValue(raw["max_output_tokens"]); ok && maxTokens > 0 {
		params.MaxCompletionTokens = &maxTokens
	} else if maxTokens, ok := intValue(raw["max_completion_tokens"]); ok && maxTokens > 0 {
		params.MaxCompletionTokens = &maxTokens
	} else if maxTokens, ok := intValue(raw["max_tokens"]); ok && maxTokens > 0 {
		params.MaxCompletionTokens = &maxTokens
	}

	if temp, ok := float64Value(raw["temperature"]); ok {
		params.Temperature = &temp
	}
	if topP, ok := float64Value(raw["top_p"]); ok {
		params.TopP = &topP
	}
	if isStream := boolValue(raw["stream"]); isStream {
		params.StreamOptions = &schemas.ChatStreamOptions{
			IncludeUsage: bifrost.Ptr(true),
		}
	}
	if reasoningRaw, ok := raw["reasoning"].(map[string]any); ok {
		effort := stringValue(reasoningRaw["effort"])
		var maxTokensPtr *int
		if mt, ok := intValue(reasoningRaw["max_tokens"]); ok && mt > 0 {
			maxTokensPtr = &mt
		}
		if effort != "" || maxTokensPtr != nil {
			var effortPtr *string
			if effort != "" {
				effortPtr = &effort
			}
			params.Reasoning = &schemas.ChatReasoning{
				Effort:    effortPtr,
				MaxTokens: maxTokensPtr,
			}
		}
	}
	if ptc, ok := raw["parallel_tool_calls"].(bool); ok {
		params.ParallelToolCalls = &ptc
	}
	if tcRaw := raw["tool_choice"]; tcRaw != nil {
		if b, err := json.Marshal(tcRaw); err == nil {
			var tc schemas.ChatToolChoice
			if err := json.Unmarshal(b, &tc); err == nil {
				params.ToolChoice = &tc
			}
		}
	}
	if stopRaw := raw["stop"]; stopRaw != nil {
		switch s := stopRaw.(type) {
		case string:
			if s != "" {
				params.Stop = []string{s}
			}
		case []any:
			stops := make([]string, 0, len(s))
			for _, elem := range s {
				if str := stringValue(elem); str != "" {
					stops = append(stops, str)
				}
			}
			if len(stops) > 0 {
				params.Stop = stops
			}
		}
	}
	if pck := stringValue(raw["prompt_cache_key"]); pck != "" {
		params.PromptCacheKey = &pck
	}
	if u := stringValue(raw["user"]); u != "" {
		params.User = &u
	}
	if textRaw, ok := raw["text"].(map[string]any); ok {
		if formatRaw, ok := textRaw["format"].(map[string]any); ok {
			if b, err := json.Marshal(formatRaw); err == nil {
				var tf schemas.ResponsesTextConfigFormat
				if err := json.Unmarshal(b, &tf); err == nil {
					if rf := schemas.ChatResponseFormatFromResponsesFormat(&tf); rf != nil {
						params.ResponseFormat = rf
					}
				}
			}
		}
	} else if rfRaw := raw["response_format"]; rfRaw != nil {
		params.ResponseFormat = &rfRaw
	}

	return params
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
	if summary, ok := m["summary"].([]any); ok {
		var sb strings.Builder
		for _, s := range summary {
			if sm, ok := s.(map[string]any); ok {
				if t, ok := sm["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	if content, ok := m["content"].([]any); ok {
		var sb strings.Builder
		for _, item := range content {
			if cm, ok := item.(map[string]any); ok {
				t := stringValue(cm["type"])
				if t == "reasoning" || t == "thought" || t == "thinking" || t == "reasoning_text" {
					if text, ok := cm["text"].(string); ok {
						sb.WriteString(text)
					}
				}
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
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
