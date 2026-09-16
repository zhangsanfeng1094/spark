package gemini

import (
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

func geminiNonStreamResponse(resp *schemas.BifrostChatResponse) map[string]any {
	parts := make([]map[string]any, 0, 4)
	finishReason := "STOP"
	if resp != nil && len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.FinishReason != nil {
			finishReason = geminiFinishReason(*choice.FinishReason)
		}
		var msg *schemas.ChatMessage
		if choice.ChatNonStreamResponseChoice != nil {
			msg = choice.ChatNonStreamResponseChoice.Message
		}
		if msg != nil {
			if msg.ChatAssistantMessage != nil && msg.ChatAssistantMessage.Reasoning != nil {
				if text := strings.TrimSpace(*msg.ChatAssistantMessage.Reasoning); text != "" {
					parts = append(parts, map[string]any{"text": text, "thought": true})
				}
			}
			parts = append(parts, geminiMessageTextParts(msg)...)
			if msg.ChatAssistantMessage != nil {
				for _, tc := range msg.ChatAssistantMessage.ToolCalls {
					if part := geminiFunctionCallPart(tc); part != nil {
						parts = append(parts, part)
					}
				}
			}
		}
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]any{"text": ""})
	}

	out := map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"role":  "model",
					"parts": parts,
				},
				"finishReason": finishReason,
				"index":        0,
			},
		},
	}
	if resp != nil {
		if resp.ID != "" {
			out["responseId"] = resp.ID
		}
		if resp.Model != "" {
			out["modelVersion"] = resp.Model
		}
		if usage := geminiUsageMetadata(resp.Usage); usage != nil {
			out["usageMetadata"] = usage
		}
	}
	return out
}

func geminiMessageTextParts(msg *schemas.ChatMessage) []map[string]any {
	if msg == nil || msg.Content == nil {
		return nil
	}
	if msg.Content.ContentStr != nil {
		if text := *msg.Content.ContentStr; text != "" {
			return []map[string]any{{"text": text}}
		}
		return nil
	}
	parts := make([]map[string]any, 0, len(msg.Content.ContentBlocks))
	for _, block := range msg.Content.ContentBlocks {
		if block.Type != schemas.ChatContentBlockTypeText || block.Text == nil {
			continue
		}
		if text := *block.Text; text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
	}
	return parts
}

func geminiFunctionCallPart(tc schemas.ChatAssistantMessageToolCall) map[string]any {
	name := ""
	if tc.Function.Name != nil {
		name = strings.TrimSpace(*tc.Function.Name)
	}
	if name == "" {
		return nil
	}
	args := objectFromJSONString(tc.Function.Arguments)
	call := map[string]any{
		"name": name,
		"args": args,
	}
	if tc.ID != nil && strings.TrimSpace(*tc.ID) != "" {
		call["id"] = *tc.ID
	}
	return map[string]any{"functionCall": call}
}

func geminiFinishReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens", "max_token":
		return "MAX_TOKENS"
	case "content_filter", "safety":
		return "SAFETY"
	default:
		return "STOP"
	}
}

func geminiUsageMetadata(u *schemas.BifrostLLMUsage) map[string]any {
	if u == nil {
		return nil
	}
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return nil
	}
	out := map[string]any{
		"promptTokenCount":     u.PromptTokens,
		"candidatesTokenCount": u.CompletionTokens,
		"totalTokenCount":      u.TotalTokens,
	}
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens > 0 {
		out["thoughtsTokenCount"] = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedReadTokens > 0 {
		out["cachedContentTokenCount"] = u.PromptTokensDetails.CachedReadTokens
	}
	return out
}

func geminiErrorBody(bErr *schemas.BifrostError) map[string]any {
	status := "INTERNAL"
	code := 500
	if bErr != nil && bErr.StatusCode != nil && *bErr.StatusCode > 0 {
		code = *bErr.StatusCode
	}
	switch code {
	case 400:
		status = "INVALID_ARGUMENT"
	case 401:
		status = "UNAUTHENTICATED"
	case 403:
		status = "PERMISSION_DENIED"
	case 404:
		status = "NOT_FOUND"
	case 429:
		status = "RESOURCE_EXHAUSTED"
	}
	message := "internal error"
	if bErr != nil {
		if msg := strings.TrimSpace(bErr.GetErrorString()); msg != "" {
			message = msg
		}
	}
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"status":  status,
		},
	}
}
