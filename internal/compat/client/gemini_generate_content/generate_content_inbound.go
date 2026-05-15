package gemini_generate_content

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"spark/internal/compat/ir"
)

func GenerateContentInbound(req map[string]any) ir.Request {
	model := stringValue(req["model"])
	if model == "" {
		model = "unknown"
	}
	out := ir.Request{
		Model:    model,
		Messages: geminiMessages(req),
		Tools:    geminiTools(req["tools"]),
		Stream:   boolValue(req["stream"]),
		Source:   ir.ProtocolGeminiGenerateContent,
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
	if reasoning := geminiReasoningConfig(config["thinkingConfig"]); reasoning.HasControls() {
		out.Generation.Reasoning = reasoning
	}
	if len(config) > 0 {
		out.Generation.Raw = ensureRaw(out.Generation.Raw)
		out.Generation.Raw["generationConfig"] = config
	}
	if choice, ok := geminiToolChoice(req["toolConfig"]); ok {
		out.ToolChoice = choice
	}
	if len(out.Messages) == 0 {
		out.Messages = []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.Text("")}}}
	}
	return out
}

func geminiReasoningConfig(raw any) ir.ReasoningConfig {
	config := mapValue(raw)
	if len(config) == 0 {
		return ir.ReasoningConfig{}
	}
	out := ir.ReasoningConfig{
		Raw: map[string]any{"thinkingConfig": raw},
	}
	if include, ok := config["includeThoughts"].(bool); ok {
		out.IncludeThoughts = &include
	}
	if budget, ok := intValue(config["thinkingBudget"]); ok {
		out.BudgetTokens = &budget
	}
	return out
}

func geminiMessages(req map[string]any) []ir.Message {
	out := make([]ir.Message, 0, 8)
	if sys := geminiSystemText(req["systemInstruction"]); sys != "" {
		out = append(out, ir.Message{Role: ir.RoleSystem, Content: []ir.ContentBlock{ir.Text(sys)}})
	}
	for _, item := range listValue(req["contents"]) {
		content := mapValue(item)
		if len(content) == 0 {
			continue
		}
		role := geminiRole(content["role"])
		blocks := geminiPartBlocks(content["parts"])
		if role == ir.RoleAssistant {
			out = append(out, ir.Message{Role: role, Content: blocks, Raw: content})
			continue
		}
		contentBlocks := geminiUserContentBlocks(blocks)
		if len(contentBlocks) > 0 {
			out = append(out, ir.Message{Role: role, Content: contentBlocks, Raw: content})
		}
		for _, block := range filterBlocks(blocks, ir.BlockToolResult) {
			out = append(out, ir.Message{Role: ir.RoleTool, Content: []ir.ContentBlock{block}, Raw: content})
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

func geminiRole(raw any) ir.Role {
	switch strings.ToLower(stringValue(raw)) {
	case "model", "assistant":
		return ir.RoleAssistant
	case "system":
		return ir.RoleSystem
	case "function", "tool":
		return ir.RoleTool
	case "user", "":
		return ir.RoleUser
	default:
		return ir.RoleUser
	}
}

func geminiPartBlocks(raw any) []ir.ContentBlock {
	parts := listValue(raw)
	if len(parts) == 0 {
		if m := mapValue(raw); len(m) > 0 {
			parts = []any{m}
		}
	}
	blocks := make([]ir.ContentBlock, 0, len(parts))
	for idx, partRaw := range parts {
		part := mapValue(partRaw)
		if len(part) == 0 {
			continue
		}
		if reasoning := geminiThought(part); reasoning != nil {
			blocks = append(blocks, ir.ContentBlock{
				Type:      ir.BlockReasoning,
				Reasoning: reasoning,
				Raw:       part,
			})
			continue
		}
		if text := stringValue(part["text"]); text != "" {
			blocks = append(blocks, ir.Text(text))
			continue
		}
		if image := geminiInlineImage(part["inlineData"]); image != nil {
			blocks = append(blocks, ir.ContentBlock{Type: ir.BlockImage, Image: image, Raw: part})
			continue
		}
		if block := geminiFileBlock(part["fileData"]); block != nil {
			blocks = append(blocks, *block)
			continue
		}
		if call := geminiFunctionCall(part["functionCall"], idx); call != nil {
			blocks = append(blocks, ir.ContentBlock{
				Type:     ir.BlockToolCall,
				ToolCall: call,
				Raw:      part,
			})
			continue
		}
		if result := geminiFunctionResponse(part["functionResponse"]); result != nil {
			blocks = append(blocks, ir.ContentBlock{
				Type:       ir.BlockToolResult,
				ToolResult: result,
				Raw:        part,
			})
		}
	}
	return blocks
}

func geminiThought(part map[string]any) *ir.ReasoningBlock {
	if !boolValue(part["thought"]) && stringValue(part["thoughtSignature"]) == "" {
		return nil
	}
	return &ir.ReasoningBlock{
		Text:       stringValue(part["text"]),
		Signature:  stringValue(part["thoughtSignature"]),
		Visibility: ir.ReasoningVisibilityInternal,
	}
}

func geminiInlineImage(raw any) *ir.ImageBlock {
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
	return &ir.ImageBlock{
		MimeType: mimeType,
		Data:     decoded,
		Raw:      data,
	}
}

func geminiFileBlock(raw any) *ir.ContentBlock {
	data := mapValue(raw)
	if len(data) == 0 {
		return nil
	}
	mimeType := stringValue(data["mimeType"])
	uri := stringValue(data["fileUri"])
	if strings.HasPrefix(mimeType, "image/") {
		return &ir.ContentBlock{
			Type: ir.BlockImage,
			Image: &ir.ImageBlock{
				URL:      uri,
				MimeType: mimeType,
				Raw:      data,
			},
			Raw: data,
		}
	}
	return &ir.ContentBlock{
		Type: ir.BlockDocument,
		Document: &ir.DocumentBlock{
			Name:     uri,
			MimeType: mimeType,
			Raw:      data,
		},
		Raw: data,
	}
}

func geminiFunctionCall(raw any, idx int) *ir.ToolCall {
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
	return &ir.ToolCall{
		ID:        id,
		Type:      ir.ToolTypeFunction,
		Name:      name,
		Arguments: jsonObjectString(call["args"]),
		Raw:       call,
	}
}

func geminiFunctionResponse(raw any) *ir.ToolResult {
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
	return &ir.ToolResult{
		ToolCallID: id,
		Output:     jsonObjectString(resp["response"]),
		Raw:        resp,
	}
}

func geminiTools(raw any) []ir.Tool {
	out := make([]ir.Tool, 0, 4)
	for _, toolRaw := range listValue(raw) {
		tool := mapValue(toolRaw)
		for _, declRaw := range listValue(tool["functionDeclarations"]) {
			decl := mapValue(declRaw)
			name := stringValue(decl["name"])
			if name == "" {
				continue
			}
			out = append(out, ir.Tool{
				Type: ir.ToolTypeFunction,
				Function: ir.FunctionTool{
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

func geminiToolChoice(raw any) (ir.ToolChoice, bool) {
	cfg := mapValue(mapValue(raw)["functionCallingConfig"])
	if len(cfg) == 0 {
		return ir.ToolChoice{}, false
	}
	mode := strings.ToUpper(stringValue(cfg["mode"]))
	allowed := listValue(cfg["allowedFunctionNames"])
	if mode == "ANY" && len(allowed) == 1 {
		if name := stringValue(allowed[0]); name != "" {
			return ir.ToolChoice{Mode: ir.ToolChoiceFunction, Name: name, Raw: raw}, true
		}
	}
	switch mode {
	case "ANY":
		return ir.ToolChoice{Mode: ir.ToolChoiceRequired, Raw: raw}, true
	case "NONE":
		return ir.ToolChoice{Mode: ir.ToolChoiceNone, Raw: raw}, true
	case "AUTO":
		return ir.ToolChoice{Mode: ir.ToolChoiceAuto, Raw: raw}, true
	default:
		return ir.ToolChoice{}, false
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

func filterBlocks(blocks []ir.ContentBlock, typ ir.BlockType) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == typ {
			out = append(out, block)
		}
	}
	return out
}

func geminiUserContentBlocks(blocks []ir.ContentBlock) []ir.ContentBlock {
	out := make([]ir.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ir.BlockText, ir.BlockImage, ir.BlockDocument, ir.BlockReasoning:
			out = append(out, block)
		}
	}
	return out
}
