package policy

import (
	"fmt"

	"spark/internal/compatir"
)

type ToolPolicy struct{}

func (ToolPolicy) ValidateRequest(req compatir.Request) error {
	seenCalls := map[string]struct{}{}
	for msgIdx, msg := range req.Messages {
		for blockIdx, block := range msg.Content {
			switch block.Type {
			case compatir.BlockToolCall:
				if block.ToolCall == nil || block.ToolCall.ID == "" {
					return fmt.Errorf("message %d block %d: tool_call missing id", msgIdx, blockIdx)
				}
				seenCalls[block.ToolCall.ID] = struct{}{}
			case compatir.BlockToolResult:
				if block.ToolResult == nil || block.ToolResult.ToolCallID == "" {
					return fmt.Errorf("message %d block %d: tool_result missing tool_call_id", msgIdx, blockIdx)
				}
				if _, ok := seenCalls[block.ToolResult.ToolCallID]; !ok {
					return fmt.Errorf("message %d block %d: tool_result references unknown tool_call_id %q", msgIdx, blockIdx, block.ToolResult.ToolCallID)
				}
			}
		}
	}
	return nil
}
