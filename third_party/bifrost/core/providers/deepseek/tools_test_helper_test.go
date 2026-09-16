package deepseek_test

import (
	"github.com/maximhq/bifrost/core/schemas"
)

const sampleToolTypeTime = "get_current_time"

func getSampleChatTool() *schemas.ChatTool {
	desc := "Get the current time in UTC"
	return &schemas.ChatTool{
		Type: "function",
		Function: &schemas.ChatToolFunction{
			Name:        sampleToolTypeTime,
			Description: &desc,
			Parameters: &schemas.ToolFunctionParameters{
				Type:       "object",
				Properties: schemas.NewOrderedMap(),
			},
		},
	}
}

func getSampleResponsesTool() *schemas.ResponsesTool {
	desc := "Get the current time in UTC"
	name := sampleToolTypeTime
	return &schemas.ResponsesTool{
		Type:        "function",
		Name:        &name,
		Description: &desc,
		ResponsesToolFunction: &schemas.ResponsesToolFunction{
			Parameters: &schemas.ToolFunctionParameters{
				Type:       "object",
				Properties: schemas.NewOrderedMap(),
			},
		},
	}
}
