package ir

import (
	"strings"
)

type Protocol string

const (
	ProtocolUnknown               Protocol = ""
	ProtocolOpenAIChat            Protocol = "openai_chat"
	ProtocolOpenAIResponses       Protocol = "openai_responses"
	ProtocolAnthropicMessages     Protocol = "anthropic_messages"
	ProtocolGeminiGenerateContent Protocol = "gemini_generate_content"
)

type Request struct {
	Model      string
	Messages   []Message
	Tools      []Tool
	ToolChoice ToolChoice
	Generation GenerationConfig
	Stream     bool
	Metadata   map[string]any
	Source     Protocol
	Raw        map[string]any
}

type Message struct {
	Role    Role
	Content []ContentBlock
	Raw     map[string]any
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentBlock struct {
	Type         BlockType
	Text         string
	Reasoning    *ReasoningBlock
	ToolCall     *ToolCall
	ToolResult   *ToolResult
	Image        *ImageBlock
	Document     *DocumentBlock
	CacheControl map[string]any
	Raw          map[string]any
}

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockReasoning  BlockType = "reasoning"
	BlockToolCall   BlockType = "tool_call"
	BlockToolResult BlockType = "tool_result"
	BlockImage      BlockType = "image"
	BlockDocument   BlockType = "document"
)

type ReasoningBlock struct {
	Text           string
	Signature      string
	Visibility     ReasoningVisibility
	// Redacted marks an Anthropic `redacted_thinking` block: the server
	// returned only opaque `data`, with no plaintext `thinking` content.
	// These blocks must be round-tripped unchanged on subsequent turns so
	// the model can decrypt them server-side.
	Redacted        bool
	Display         ReasoningDisplay
	ProviderFields  map[string]any
}

// ReasoningDisplay mirrors Anthropic's thinking `display` field.
type ReasoningDisplay string

const (
	ReasoningDisplayUnspecified ReasoningDisplay = ""
	ReasoningDisplaySummarized  ReasoningDisplay = "summarized"
	ReasoningDisplayOmitted     ReasoningDisplay = "omitted"
)

type ReasoningVisibility string

const (
	ReasoningVisibilityInternal ReasoningVisibility = "internal"
	ReasoningVisibilitySummary  ReasoningVisibility = "summary"
	ReasoningVisibilityVisible  ReasoningVisibility = "visible"
)

type ToolCall struct {
	ID        string
	Type      ToolType
	Name      string
	Arguments string
	Raw       map[string]any
}

type ToolResult struct {
	ToolCallID string
	Output     string
	IsError    bool
	Raw        map[string]any
}

type Tool struct {
	Type         ToolType
	Function     FunctionTool
	CacheControl map[string]any
	Raw          map[string]any
}

type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

type FunctionTool struct {
	Name        string
	Description string
	Parameters  any
}

type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
	Raw  any
}

type ToolChoiceMode string

const (
	ToolChoiceUnspecified ToolChoiceMode = ""
	ToolChoiceAuto        ToolChoiceMode = "auto"
	ToolChoiceNone        ToolChoiceMode = "none"
	ToolChoiceRequired    ToolChoiceMode = "required"
	ToolChoiceFunction    ToolChoiceMode = "function"
)

type GenerationConfig struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	Stop        []string
	Reasoning   ReasoningConfig
	Raw         map[string]any
}

type ImageBlock struct {
	URL      string
	MimeType string
	Data     []byte
	Raw      map[string]any
}

type DocumentBlock struct {
	Name     string
	MimeType string
	Text     string
	Data     []byte
	Raw      map[string]any
}

type Response struct {
	ID         string
	Model      string
	Output     []ContentBlock
	StopReason StopReason
	Usage      Usage
	Raw        map[string]any
}

type StopReason string

const (
	StopReasonUnknown       StopReason = ""
	StopReasonEndTurn       StopReason = "end_turn"
	StopReasonMaxTokens     StopReason = "max_tokens"
	StopReasonToolUse       StopReason = "tool_use"
	StopReasonContentFilter StopReason = "content_filter"
	StopReasonError         StopReason = "error"
)

func Text(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

func Reasoning(text string) ContentBlock {
	return ContentBlock{
		Type: BlockReasoning,
		Reasoning: &ReasoningBlock{
			Text:       text,
			Visibility: ReasoningVisibilityInternal,
		},
	}
}

func (m Message) Text() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type == BlockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (m Message) ReasoningText() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type != BlockReasoning || block.Reasoning == nil || block.Reasoning.Text == "" {
			continue
		}
		parts = append(parts, block.Reasoning.Text)
	}
	return strings.Join(parts, "\n")
}

func (m Message) HasToolCalls() bool {
	for _, block := range m.Content {
		if block.Type == BlockToolCall && block.ToolCall != nil {
			return true
		}
	}
	return false
}

func (m Message) ToolCallIDs() []string {
	ids := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type != BlockToolCall || block.ToolCall == nil || block.ToolCall.ID == "" {
			continue
		}
		ids = append(ids, block.ToolCall.ID)
	}
	return ids
}

func (r Request) ToolCallIDs() []string {
	var ids []string
	for _, msg := range r.Messages {
		ids = append(ids, msg.ToolCallIDs()...)
	}
	return ids
}
