package gemini

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/compat/engine"
	"spark/internal/compat/ingress/media"
	"spark/internal/config"
)

func ToBifrostRequest(raw map[string]any, profile *config.Profile) *schemas.BifrostChatRequest {
	provider, _, _ := engine.MapProfileToProvider(profile)
	preferred := ""
	if profile != nil {
		preferred = profile.DefaultModel
	}
	model := engine.ResolveProfileModel(stringValue(raw["model"]), profile, preferred)
	params := geminiParams(raw)
	params.Tools = geminiTools(raw["tools"])
	params.ToolChoice = geminiToolChoice(raw["toolConfig"])
	return &schemas.BifrostChatRequest{
		Provider: provider,
		Model:    model,
		Input:    geminiMessages(raw),
		Params:   params,
	}
}

func geminiMessages(raw map[string]any) []schemas.ChatMessage {
	messages := make([]schemas.ChatMessage, 0, 8)
	if sys := geminiSystemText(raw["systemInstruction"]); sys != "" {
		messages = append(messages, textMessage(schemas.ChatMessageRoleSystem, sys))
	}
	for _, item := range listValue(raw["contents"]) {
		content := mapValue(item)
		if len(content) == 0 {
			continue
		}
		role := schemas.ChatMessageRoleUser
		if r := strings.ToLower(stringValue(content["role"])); r == "model" || r == "assistant" {
			role = schemas.ChatMessageRoleAssistant
		}
		text, blocks, reasoning, calls, results := geminiParts(content["parts"])
		if role == schemas.ChatMessageRoleAssistant {
			var msgContent *schemas.ChatMessageContent
			if len(blocks) > 0 {
				msgContent = &schemas.ChatMessageContent{ContentBlocks: blocks}
			} else if text != "" {
				msgContent = &schemas.ChatMessageContent{ContentStr: &text}
			}
			if msgContent != nil || reasoning != "" || len(calls) > 0 {
				msg := schemas.ChatMessage{Role: role, Content: msgContent}
				if reasoning != "" || len(calls) > 0 {
					var reasoningPtr *string
					if reasoning != "" {
						reasoningPtr = &reasoning
					}
					msg.ChatAssistantMessage = &schemas.ChatAssistantMessage{Reasoning: reasoningPtr, ToolCalls: calls}
				}
				messages = append(messages, msg)
			}
		} else if len(blocks) > 0 {
			messages = append(messages, schemas.ChatMessage{Role: role, Content: &schemas.ChatMessageContent{ContentBlocks: blocks}})
		} else if text != "" {
			messages = append(messages, textMessage(role, text))
		}
		messages = append(messages, results...)
	}
	if len(messages) == 0 {
		messages = append(messages, textMessage(schemas.ChatMessageRoleUser, ""))
	}
	return messages
}

func geminiSystemText(raw any) string {
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return geminiPartsText(mapValue(raw)["parts"])
}

func geminiParts(raw any) (string, []schemas.ChatContentBlock, string, []schemas.ChatAssistantMessageToolCall, []schemas.ChatMessage) {
	var text strings.Builder
	var reasoning strings.Builder
	blocks := make([]schemas.ChatContentBlock, 0, 4)
	calls := make([]schemas.ChatAssistantMessageToolCall, 0, 2)
	results := make([]schemas.ChatMessage, 0, 2)
	for idx, item := range listValue(raw) {
		part := mapValue(item)
		if len(part) == 0 {
			continue
		}
		if partText := stringValue(part["text"]); partText != "" {
			if thought, _ := part["thought"].(bool); thought {
				reasoning.WriteString(partText)
			} else {
				text.WriteString(partText)
				blocks = append(blocks, schemas.ChatContentBlock{Type: schemas.ChatContentBlockTypeText, Text: &partText})
			}
			continue
		}
		if inline := mapValue(part["inlineData"]); len(inline) > 0 {
			mimeType := stringValue(inline["mimeType"])
			data := stringValue(inline["data"])
			if strings.HasPrefix(mimeType, "image/") && data != "" {
				if cleanData, ok := media.CleanAndValidateBase64(data); ok {
					blocks = append(blocks, schemas.ChatContentBlock{
						Type:           schemas.ChatContentBlockTypeImage,
						ImageURLStruct: &schemas.ChatInputImage{URL: "data:" + mimeType + ";base64," + cleanData},
					})
				}
			}
			continue
		}
		if file := mapValue(part["fileData"]); len(file) > 0 {
			uri := stringValue(file["fileUri"])
			mimeType := stringValue(file["mimeType"])
			if strings.HasPrefix(mimeType, "image/") {
				blocks = append(blocks, schemas.ChatContentBlock{Type: schemas.ChatContentBlockTypeImage, ImageURLStruct: &schemas.ChatInputImage{URL: uri}})
			} else if uri != "" {
				blocks = append(blocks, schemas.ChatContentBlock{Type: schemas.ChatContentBlockTypeFile, File: &schemas.ChatInputFile{FileURL: &uri, FileType: &mimeType}})
			}
			continue
		}
		if call := mapValue(part["functionCall"]); len(call) > 0 {
			name := stringValue(call["name"])
			if name == "" {
				continue
			}
			id := stringValue(call["id"])
			if id == "" {
				id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx)
			}
			callType := "function"
			calls = append(calls, schemas.ChatAssistantMessageToolCall{
				Index: uint16(idx), ID: &id, Type: &callType,
				Function: schemas.ChatAssistantMessageToolCallFunction{Name: &name, Arguments: jsonObjectString(call["args"])},
			})
			continue
		}
		if response := mapValue(part["functionResponse"]); len(response) > 0 {
			id := stringValue(response["id"])
			if id == "" {
				id = stringValue(response["name"])
			}
			if id == "" {
				continue
			}
			output := jsonObjectString(response["response"])
			results = append(results, schemas.ChatMessage{
				Role:            schemas.ChatMessageRoleTool,
				Content:         &schemas.ChatMessageContent{ContentStr: &output},
				ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: &id},
			})
		}
	}
	if len(blocks) == 1 && blocks[0].Type == schemas.ChatContentBlockTypeText {
		blocks = nil
	}
	return text.String(), blocks, reasoning.String(), calls, results
}

func geminiPartsText(raw any) string {
	parts := make([]string, 0, len(listValue(raw)))
	for _, item := range listValue(raw) {
		if text := stringValue(mapValue(item)["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func textMessage(role schemas.ChatMessageRole, text string) schemas.ChatMessage {
	return schemas.ChatMessage{Role: role, Content: &schemas.ChatMessageContent{ContentStr: &text}}
}

func geminiTools(raw any) []schemas.ChatTool {
	tools := make([]schemas.ChatTool, 0, 8)
	for _, toolRaw := range listValue(raw) {
		for _, declarationRaw := range listValue(mapValue(toolRaw)["functionDeclarations"]) {
			declaration := mapValue(declarationRaw)
			name := stringValue(declaration["name"])
			if name == "" {
				continue
			}
			description := stringValue(declaration["description"])
			var descriptionPtr *string
			if description != "" {
				descriptionPtr = &description
			}
			var parameters *schemas.ToolFunctionParameters
			if schema := declaration["parameters"]; schema != nil {
				if data, err := json.Marshal(schema); err == nil {
					var decoded schemas.ToolFunctionParameters
					if json.Unmarshal(data, &decoded) == nil {
						parameters = &decoded
					}
				}
			}
			tools = append(tools, schemas.ChatTool{Type: schemas.ChatToolTypeFunction, Function: &schemas.ChatToolFunction{Name: name, Description: descriptionPtr, Parameters: parameters}})
		}
	}
	return tools
}

func geminiToolChoice(raw any) *schemas.ChatToolChoice {
	cfg := mapValue(mapValue(raw)["functionCallingConfig"])
	if len(cfg) == 0 {
		return nil
	}
	mode := strings.ToUpper(stringValue(cfg["mode"]))
	allowed := listValue(cfg["allowedFunctionNames"])
	if mode == "ANY" && len(allowed) == 1 {
		name := stringValue(allowed[0])
		return &schemas.ChatToolChoice{ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{Type: schemas.ChatToolChoiceTypeFunction, Function: &schemas.ChatToolChoiceFunction{Name: name}}}
	}
	choice := "auto"
	switch mode {
	case "ANY":
		choice = "required"
	case "NONE":
		choice = "none"
	}
	return &schemas.ChatToolChoice{ChatToolChoiceStr: &choice}
}

func geminiParams(raw map[string]any) *schemas.ChatParameters {
	params := &schemas.ChatParameters{}
	cfg := mapValue(raw["generationConfig"])
	if value, ok := intValue(cfg["maxOutputTokens"]); ok && value > 0 {
		params.MaxCompletionTokens = &value
	}
	if value, ok := floatValue(cfg["temperature"]); ok {
		params.Temperature = value
	}
	if value, ok := floatValue(cfg["topP"]); ok {
		params.TopP = value
	}
	if value, ok := intValue(cfg["topK"]); ok {
		params.TopK = &value
	}
	for _, stop := range listValue(cfg["stopSequences"]) {
		if value := stringValue(stop); value != "" {
			params.Stop = append(params.Stop, value)
		}
	}
	if thinking := mapValue(cfg["thinkingConfig"]); len(thinking) > 0 {
		enabled := true
		reasoning := &schemas.ChatReasoning{Enabled: &enabled}
		if budget, ok := intValue(thinking["thinkingBudget"]); ok && budget >= 0 {
			reasoning.MaxTokens = &budget
		}
		params.Reasoning = reasoning
	}
	if stream, _ := raw["stream"].(bool); stream {
		params.StreamOptions = &schemas.ChatStreamOptions{IncludeUsage: bifrost.Ptr(true)}
	}
	return params
}
