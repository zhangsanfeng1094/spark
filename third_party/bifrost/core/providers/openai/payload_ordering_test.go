package openai

import (
	"testing"

	"github.com/bytedance/sonic"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadOrdering_OpenAIChatRequest(t *testing.T) {
	req := &OpenAIChatRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("hello")},
			},
		},
		ChatParameters: schemas.ChatParameters{
			Temperature: schemas.Ptr(0.7),
			Tools: []schemas.ChatTool{
				{
					Type: "function",
					Function: &schemas.ChatToolFunction{
						Name:        "get_weather",
						Description: schemas.Ptr("Get weather"),
						Parameters: &schemas.ToolFunctionParameters{
							Type: "object",
							Properties: schemas.NewOrderedMapFromPairs(
								schemas.KV("location", map[string]interface{}{"type": "string"}),
							),
							Required: []string{"location"},
						},
					},
				},
			},
			Reasoning: &schemas.ChatReasoning{
				Effort: schemas.Ptr("high"),
			},
		},
		Stream: schemas.Ptr(true),
	}

	result, err := providerUtils.MarshalSorted(req)
	require.NoError(t, err)

	golden := `{"model":"gpt-4o","temperature":0.7,"stream":true,"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}}}],"reasoning_effort":"high"}`

	assert.Equal(t, golden, string(result), "payload field ordering changed — if intentional, update the golden string")

	// Determinism: 100 iterations must produce identical bytes
	for i := 0; i < 100; i++ {
		iter, err := providerUtils.MarshalSorted(req)
		require.NoError(t, err)
		assert.Equal(t, string(result), string(iter), "non-deterministic marshal output on iteration %d", i)
	}
}

// TestPayloadOrdering_ResponsesTextFormatJSONSchema guards against JSON-schema
// key reordering on the Responses passthrough path. Structured-output generation
// is sensitive to the literal property order of `text.format.schema`: OpenAI
// models fill fields / pick union branches following schema key order, so
// decoding schema objects into plain Go maps and re-marshaling them sorted
// (alphabetized) measurably degrades output quality (e.g. union-of-parts outputs
// collapsing to citation-only responses).
//
// The whole schema — its own top-level keys as well as everything nested
// (properties, $defs, items, anyOf bodies) — must round-trip in the client's
// original key order: top-level keys via the decoded key order recorded on
// ResponsesTextConfigFormatJSONSchema, nested objects via OrderedMap.
func TestPayloadOrdering_ResponsesTextFormatJSONSchema(t *testing.T) {
	// Deliberately non-alphabetical key order everywhere: "type" precedes
	// "text"/"url"/"items" etc. — alphabetical sorting would reorder all of them.
	schemaJSON := `{"type":"object","properties":{"parts":{"type":"array","items":{"anyOf":[{"$ref":"#/$defs/TextPart"},{"$ref":"#/$defs/WebCitation"}]}}},"required":["parts"],"additionalProperties":false,"$defs":{"TextPart":{"type":"object","properties":{"type":{"const":"text"},"text":{"type":"string"}},"required":["type","text"],"additionalProperties":false},"WebCitation":{"type":"object","properties":{"type":{"const":"cite:web"},"url":{"type":"string"}},"required":["type","url"],"additionalProperties":false}}}`
	rawBody := `{"model":"gpt-4o","input":"hi","text":{"format":{"type":"json_schema","name":"final_output","schema":` + schemaJSON + `,"strict":true}}}`

	var req OpenAIResponsesRequest
	require.NoError(t, sonic.Unmarshal([]byte(rawBody), &req), "decode request")
	require.NotNil(t, req.Text)
	require.NotNil(t, req.Text.Format)
	require.NotNil(t, req.Text.Format.JSONSchema)

	marshaled, err := providerUtils.MarshalSorted(&req)
	require.NoError(t, err)

	// The schema re-serializes byte-identical to what the client sent, at every
	// level, including its own (non-alphabetical) top-level key order.
	goldenSchema := `{"type":"object","properties":{"parts":{"type":"array","items":{"anyOf":[{"$ref":"#/$defs/TextPart"},{"$ref":"#/$defs/WebCitation"}]}}},"required":["parts"],"additionalProperties":false,"$defs":{"TextPart":{"type":"object","properties":{"type":{"const":"text"},"text":{"type":"string"}},"required":["type","text"],"additionalProperties":false},"WebCitation":{"type":"object","properties":{"type":{"const":"cite:web"},"url":{"type":"string"}},"required":["type","url"],"additionalProperties":false}}}`
	assert.Contains(t, string(marshaled), `"schema":`+goldenSchema,
		"nested schema key order changed — if intentional, update the golden string")

	// Determinism: repeated marshals must produce identical bytes
	for i := 0; i < 100; i++ {
		iter, err := providerUtils.MarshalSorted(&req)
		require.NoError(t, err)
		assert.Equal(t, string(marshaled), string(iter), "non-deterministic marshal on iteration %d", i)
	}
}
