package gemini

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"spark/internal/compatir"
)

func GenerateContentInbound(req map[string]any) compatir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := compatir.Request{
		Model:    model,
		Messages: geminiMessages(req),
		Tools:    geminiTools(req["tools"]),
		Stream:   boolValue(req["stream"]),
		Source:   compatir.ProtocolGeminiGenerateContent,
		Raw:      req,
	}
	config := mapValue(req["generationConfig"])
	if max, ok := intValue(config["maxOutputTokens"]); ok && max > 0 {
		out.Generation.MaxTokens = &max
	}
	if temp, ok := float64Value(config["temperature"]); ok {
		out.Generation.Temperature = temp
	}
	if topP, ok := float64Value(config["topP"]); ok {
		out.Generation.TopP = topP
	}
	for _, raw := range listValue(config["stopSequences"]) {
		if stop := stringValue(raw); stop != "" {
			out.Generation.Stop = append(out.Generation.Stop, stop)
		}
	}
	if len(config) > 0 {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["generationConfig"] = config
	}
	if choice, ok := geminiToolChoice(req["toolConfig"]); ok {
		out.ToolChoice = choice
	}
	if len(out.Messages) == 0 {
		out.Messages = []compatir.Message{{Role: compatir.RoleUser, Content: []compatir.ContentBlock{compatir.Text("")}}}
	}
	return out
}

func geminiMessages(req map[string]any) []compatir.Message {
	out := make([]compatir.Message, 0, 8)
	if sys := geminiSystemText(req["systemInstruction"]); sys != "" {
		out = append(out, compatir.Message{Role: compatir.RoleSystem, Content: []compatir.ContentBlock{compatir.Text(sys)}})
	}
	for _, item := range listValue(req["contents"]) {
		content := mapValue(item)
		if len(content) == 0 {
			continue
		}
		role := geminiRole(content["role"])
		blocks := geminiPartBlocks(content["parts"])
		if role == compatir.RoleAssistant {
			out = append(out, compatir.Message{Role: role, Content: blocks, Raw: content})
			continue
		}
		contentBlocks := geminiUserContentBlocks(blocks)
		if len(contentBlocks) > 0 {
			out = append(out, compatir.Message{Role: role, Content: contentBlocks, Raw: content})
		}
		for _, block := range filterBlocks(blocks, compatir.BlockToolResult) {
			out = append(out, compatir.Message{Role: compatir.RoleTool, Content: []compatir.ContentBlock{block}, Raw: content})
		}
	}
	return out
}

func geminiSystemText(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		return geminiPartsText(v["parts"])
	default:
		return normalizeContent(v)
	}
}

func geminiRole(raw any) compatir.Role {
	switch strings.ToLower(stringValue(raw)) {
	case "model", "assistant":
		return compatir.RoleAssistant
	case "system":
		return compatir.RoleSystem
	case "function", "tool":
		return compatir.RoleTool
	case "user", "":
		return compatir.RoleUser
	default:
		return compatir.RoleUser
	}
}

func geminiPartBlocks(raw any) []compatir.ContentBlock {
	parts := listValue(raw)
	if len(parts) == 0 {
		if m := mapValue(raw); len(m) > 0 {
			parts = []any{m}
		}
	}
	blocks := make([]compatir.ContentBlock, 0, len(parts))
	for idx, partRaw := range parts {
		part := mapValue(partRaw)
		if len(part) == 0 {
			continue
		}
		if reasoning := geminiThought(part); reasoning != nil {
			blocks = append(blocks, compatir.ContentBlock{
				Type:      compatir.BlockReasoning,
				Reasoning: reasoning,
				Raw:       part,
			})
			continue
		}
		if text := stringValue(part["text"]); text != "" {
			blocks = append(blocks, compatir.Text(text))
			continue
		}
		if image := geminiInlineImage(part["inlineData"]); image != nil {
			blocks = append(blocks, compatir.ContentBlock{Type: compatir.BlockImage, Image: image, Raw: part})
			continue
		}
		if block := geminiFileBlock(part["fileData"]); block != nil {
			blocks = append(blocks, *block)
			continue
		}
		if call := geminiFunctionCall(part["functionCall"], idx); call != nil {
			blocks = append(blocks, compatir.ContentBlock{
				Type:     compatir.BlockToolCall,
				ToolCall: call,
				Raw:      part,
			})
			continue
		}
		if result := geminiFunctionResponse(part["functionResponse"]); result != nil {
			blocks = append(blocks, compatir.ContentBlock{
				Type:       compatir.BlockToolResult,
				ToolResult: result,
				Raw:        part,
			})
		}
	}
	return blocks
}

func geminiThought(part map[string]any) *compatir.ReasoningBlock {
	if !boolValue(part["thought"]) && stringValue(part["thoughtSignature"]) == "" {
		return nil
	}
	return &compatir.ReasoningBlock{
		Text:       stringValue(part["text"]),
		Signature:  stringValue(part["thoughtSignature"]),
		Visibility: compatir.ReasoningVisibilityInternal,
	}
}

func geminiInlineImage(raw any) *compatir.ImageBlock {
	data := mapValue(raw)
	if len(data) == 0 {
		return nil
	}
	mimeType := stringValue(data["mimeType"])
	if !strings.HasPrefix(mimeType, "image/") {
		return nil
	}
	var decoded []byte
	if encoded := stringValue(data["data"]); encoded != "" {
		decoded, _ = base64.StdEncoding.DecodeString(encoded)
	}
	return &compatir.ImageBlock{
		MimeType: mimeType,
		Data:     decoded,
		Raw:      data,
	}
}

func geminiFileBlock(raw any) *compatir.ContentBlock {
	data := mapValue(raw)
	if len(data) == 0 {
		return nil
	}
	mimeType := stringValue(data["mimeType"])
	uri := stringValue(data["fileUri"])
	if strings.HasPrefix(mimeType, "image/") {
		return &compatir.ContentBlock{
			Type: compatir.BlockImage,
			Image: &compatir.ImageBlock{
				URL:      uri,
				MimeType: mimeType,
				Raw:      data,
			},
			Raw: data,
		}
	}
	return &compatir.ContentBlock{
		Type: compatir.BlockDocument,
		Document: &compatir.DocumentBlock{
			Name:     uri,
			MimeType: mimeType,
			Raw:      data,
		},
		Raw: data,
	}
}

func geminiFunctionCall(raw any, idx int) *compatir.ToolCall {
	call := mapValue(raw)
	if len(call) == 0 {
		return nil
	}
	name := stringValue(call["name"])
	if name == "" {
		return nil
	}
	id := stringValue(call["id"])
	if id == "" {
		id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx)
	}
	return &compatir.ToolCall{
		ID:        id,
		Type:      compatir.ToolTypeFunction,
		Name:      name,
		Arguments: jsonObjectString(call["args"]),
		Raw:       call,
	}
}

func geminiFunctionResponse(raw any) *compatir.ToolResult {
	resp := mapValue(raw)
	if len(resp) == 0 {
		return nil
	}
	id := stringValue(resp["id"])
	name := stringValue(resp["name"])
	if id == "" {
		id = name
	}
	if id == "" {
		return nil
	}
	return &compatir.ToolResult{
		ToolCallID: id,
		Output:     jsonObjectString(resp["response"]),
		Raw:        resp,
	}
}

func geminiTools(raw any) []compatir.Tool {
	out := make([]compatir.Tool, 0, 4)
	for _, toolRaw := range listValue(raw) {
		tool := mapValue(toolRaw)
		for _, declRaw := range listValue(tool["functionDeclarations"]) {
			decl := mapValue(declRaw)
			name := stringValue(decl["name"])
			if name == "" {
				continue
			}
			out = append(out, compatir.Tool{
				Type: compatir.ToolTypeFunction,
				Function: compatir.FunctionTool{
					Name:        name,
					Description: stringValue(decl["description"]),
					Parameters:  decl["parameters"],
				},
				Raw: decl,
			})
		}
	}
	return out
}

func geminiToolChoice(raw any) (compatir.ToolChoice, bool) {
	cfg := mapValue(mapValue(raw)["functionCallingConfig"])
	if len(cfg) == 0 {
		return compatir.ToolChoice{}, false
	}
	mode := strings.ToUpper(stringValue(cfg["mode"]))
	allowed := listValue(cfg["allowedFunctionNames"])
	if mode == "ANY" && len(allowed) == 1 {
		if name := stringValue(allowed[0]); name != "" {
			return compatir.ToolChoice{Mode: compatir.ToolChoiceFunction, Name: name, Raw: raw}, true
		}
	}
	switch mode {
	case "ANY":
		return compatir.ToolChoice{Mode: compatir.ToolChoiceRequired, Raw: raw}, true
	case "NONE":
		return compatir.ToolChoice{Mode: compatir.ToolChoiceNone, Raw: raw}, true
	case "AUTO":
		return compatir.ToolChoice{Mode: compatir.ToolChoiceAuto, Raw: raw}, true
	default:
		return compatir.ToolChoice{}, false
	}
}

func geminiPartsText(raw any) string {
	parts := make([]string, 0, len(listValue(raw)))
	for _, partRaw := range listValue(raw) {
		part := mapValue(partRaw)
		if text := stringValue(part["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func filterBlocks(blocks []compatir.ContentBlock, typ compatir.BlockType) []compatir.ContentBlock {
	out := make([]compatir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == typ {
			out = append(out, block)
		}
	}
	return out
}

func geminiUserContentBlocks(blocks []compatir.ContentBlock) []compatir.ContentBlock {
	out := make([]compatir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case compatir.BlockText, compatir.BlockImage, compatir.BlockDocument, compatir.BlockReasoning:
			out = append(out, block)
		}
	}
	return out
}
