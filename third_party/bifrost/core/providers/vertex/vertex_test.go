package vertex_test

import (
	"testing"

	"github.com/maximhq/bifrost/core/providers/gemini"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexGeminiChatCompletionPreservesGCSImageURL(t *testing.T) {
	result, err := gemini.ToGeminiChatCompletionRequestWithImageURLSchemes(nil, &schemas.BifrostChatRequest{
		Model: "gemini-3-flash-preview",
		Input: []schemas.ChatMessage{
			{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentBlocks: []schemas.ChatContentBlock{
						{
							Type: schemas.ChatContentBlockTypeText,
							Text: schemas.Ptr("Describe this image."),
						},
						{
							Type: schemas.ChatContentBlockTypeImage,
							ImageURLStruct: &schemas.ChatInputImage{
								URL: "gs://my-bucket/xxx.png",
							},
						},
					},
				},
			},
		},
	}, "http", "https", "gs")

	require.NoError(t, err)
	require.Len(t, result.Contents, 1)
	require.Len(t, result.Contents[0].Parts, 2)
	require.NotNil(t, result.Contents[0].Parts[1].FileData)
	assert.Equal(t, "gs://my-bucket/xxx.png", result.Contents[0].Parts[1].FileData.FileURI)
	assert.Equal(t, "image/png", result.Contents[0].Parts[1].FileData.MIMEType)
}

func TestVertexGeminiResponsesPreservesGCSImageURL(t *testing.T) {
	result, err := gemini.ToGeminiResponsesRequestWithImageURLSchemes(nil, &schemas.BifrostResponsesRequest{
		Provider: schemas.Vertex,
		Model:    "gemini-3-flash-preview",
		Input: []schemas.ResponsesMessage{
			{
				Role: schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
				Content: &schemas.ResponsesMessageContent{
					ContentBlocks: []schemas.ResponsesMessageContentBlock{
						{
							Type: schemas.ResponsesInputMessageContentBlockTypeText,
							Text: schemas.Ptr("Describe this image."),
						},
						{
							Type: schemas.ResponsesInputMessageContentBlockTypeImage,
							ResponsesInputMessageContentBlockImage: &schemas.ResponsesInputMessageContentBlockImage{
								ImageURL: schemas.Ptr("gs://my-bucket/xxx.png"),
							},
						},
					},
				},
			},
		},
	}, "http", "https", "gs")

	require.NoError(t, err)
	require.Len(t, result.Contents, 1)
	require.Len(t, result.Contents[0].Parts, 2)
	require.NotNil(t, result.Contents[0].Parts[1].FileData)
	assert.Equal(t, "gs://my-bucket/xxx.png", result.Contents[0].Parts[1].FileData.FileURI)
	assert.Equal(t, "image/png", result.Contents[0].Parts[1].FileData.MIMEType)
}

// TestVertexGeminiRejectsUnsupportedScheme guards the upper bound of the Vertex
// scheme allowlist: even though Vertex extends Gemini's defaults with "gs", schemes
// outside that set (file://, ftp://, ...) must still be rejected.
func TestVertexGeminiRejectsUnsupportedScheme(t *testing.T) {
	_, err := gemini.ToGeminiChatCompletionRequestWithImageURLSchemes(nil, &schemas.BifrostChatRequest{
		Model: "gemini-3-flash-preview",
		Input: []schemas.ChatMessage{
			{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentBlocks: []schemas.ChatContentBlock{
						{
							Type: schemas.ChatContentBlockTypeImage,
							ImageURLStruct: &schemas.ChatInputImage{
								URL: "file:///etc/passwd",
							},
						},
					},
				},
			},
		},
	}, "http", "https", "gs")

	require.Error(t, err)
	assert.Contains(t, err.Error(), `URL scheme "file" is not allowed`)
}
