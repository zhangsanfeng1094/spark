// Package deepseek implements the DeepSeek LLM provider.
package deepseek

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// DeepSeekProvider implements the Provider interface for DeepSeek's API.
type DeepSeekProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewDeepSeekProvider creates a new DeepSeek provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewDeepSeekProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*DeepSeekProvider, error) {
	config.CheckAndSetDefaults()

	requestTimeout := time.Second * time.Duration(config.NetworkConfig.DefaultRequestTimeoutInSeconds)
	client := &fasthttp.Client{
		ReadTimeout:         requestTimeout,
		WriteTimeout:        requestTimeout,
		MaxConnsPerHost:     config.NetworkConfig.MaxConnsPerHost,
		MaxIdleConnDuration: time.Second * time.Duration(config.NetworkConfig.KeepAliveTimeoutInSeconds),
		MaxConnWaitTimeout:  requestTimeout,
		MaxConnDuration:     time.Second * time.Duration(schemas.DefaultMaxConnDurationInSeconds),
		ConnPoolStrategy:    fasthttp.FIFO,
	}

	// Configure proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	// Set default BaseURL if not provided
	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = "https://api.deepseek.com"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &DeepSeekProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

func (provider *DeepSeekProvider) anthropicHeaders(key schemas.Key) map[string]string {
	headers := map[string]string{}
	if key.Value.GetValue() != "" {
		headers["x-api-key"] = key.Value.GetValue()
	}
	return headers
}

// applyDeepSeekThinkingCompatibility returns a request with thinking forced off when
// DeepSeek's OpenAI-compatible endpoint would otherwise reject it. Thinking is enabled by
// default upstream (effort "high"), so this shim stays as narrow as possible: anything it
// catches silently loses reasoning, with no error surfaced to the caller.
//
// The original request is never mutated. Params and ExtraParams are shared with the caller
// and with any fallback attempt against a different provider, so writing through would leak
// thinking:disabled well beyond this call.
func applyDeepSeekThinkingCompatibility(request *schemas.BifrostChatRequest) *schemas.BifrostChatRequest {
	if request == nil || request.Params == nil {
		return request
	}
	if !requiresDeepSeekThinkingDisabled(request) {
		return request
	}

	paramsCopy := *request.Params
	extraParams := make(map[string]any, len(paramsCopy.ExtraParams)+1)
	maps.Copy(extraParams, paramsCopy.ExtraParams)
	extraParams["thinking"] = map[string]any{"type": "disabled"}
	paramsCopy.ExtraParams = extraParams

	requestCopy := *request
	requestCopy.Params = &paramsCopy
	return &requestCopy
}

// requiresDeepSeekThinkingDisabled reports whether DeepSeek's OpenAI-compatible endpoint
// would reject this request with thinking on. Two cases qualify:
//
//  1. A forced tool_choice ("required"/"any", or the struct form pinning a specific
//     function/custom/allowed_tools call) — DeepSeek returns 400 "Thinking mode does not
//     support this tool_choice" while thinking is enabled.
//  2. A history containing an assistant tool_call turn that carries no replayable
//     reasoning. DeepSeek requires reasoning_content to be replayed on tool_call turns and
//     400s without it; when the caller cannot supply it, thinking has to stay off. Only
//     ChatAssistantMessage.Reasoning counts here - reasoning_details is inbound-only and
//     never reaches the wire - and this is judged from the messages alone, since a caller
//     may replay tool_call turns without re-declaring Params.Tools.
//
// Everything else keeps thinking on. In particular, a plain multi-turn conversation needs no
// reasoning replay whatsoever — per DeepSeek's thinking-mode guide, "the intermediate
// assistant's reasoning_content does not need to participate in the context concatenation"
// when there is no tool call. Assistant turns without reasoning are therefore expected and
// must not disable thinking (issue #5887).
func requiresDeepSeekThinkingDisabled(request *schemas.BifrostChatRequest) bool {
	if tc := request.Params.ToolChoice; tc != nil {
		switch {
		case tc.ChatToolChoiceStr != nil:
			switch schemas.ChatToolChoiceType(*tc.ChatToolChoiceStr) {
			case schemas.ChatToolChoiceTypeRequired, schemas.ChatToolChoiceTypeAny:
				return true
			}
		case tc.ChatToolChoiceStruct != nil:
			switch tc.ChatToolChoiceStruct.Type {
			case schemas.ChatToolChoiceTypeRequired, schemas.ChatToolChoiceTypeAny,
				schemas.ChatToolChoiceTypeFunction, schemas.ChatToolChoiceTypeCustom,
				schemas.ChatToolChoiceTypeAllowedTools:
				return true
			}
		}
	}

	// The replay requirement follows the messages, not the tool declarations: a caller that
	// drops Params.Tools on a final summarizing turn still replays the assistant tool_call
	// turns, and DeepSeek still demands reasoning_content on them.
	for _, msg := range request.Input {
		if msg.Role != schemas.ChatMessageRoleAssistant || msg.ChatAssistantMessage == nil {
			continue
		}
		if len(msg.ChatAssistantMessage.ToolCalls) == 0 {
			continue
		}
		// Only Reasoning can satisfy the replay requirement: it is the sole field
		// ConvertBifrostMessagesToOpenAIMessages copies onto the outbound assistant
		// message, as reasoning_content. ReasoningDetails is inbound-only and never
		// reaches the wire (see core/providers/openai/types.go), so a details-only turn
		// would keep thinking on and then 400 for the missing reasoning_content.
		if msg.ChatAssistantMessage.Reasoning == nil {
			return true
		}
	}

	return false
}

// GetProviderKey returns the provider identifier for DeepSeek.
func (provider *DeepSeekProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.DeepSeek
}

// ListModels performs a list models request to DeepSeek's API.
func (provider *DeepSeekProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return openai.HandleOpenAIListModelsRequest(
		ctx,
		provider.client,
		request,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/models"),
		keys,
		provider.networkConfig.ExtraHeaders,
		provider.GetProviderKey(),
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
	)
}

// ChatCompletion performs a chat completion request to DeepSeek's Anthropic-compatible API.
func (provider *DeepSeekProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicChatCompletionRequest(
			ctx,
			provider.client,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/anthropic/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.DeepSeek,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	request = applyDeepSeekThinkingCompatibility(request)
	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		nil,
		nil,
		nil,
		provider.logger,
	)
}

// ChatCompletionStream performs a streaming chat completion request to DeepSeek's Anthropic-compatible API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Returns a channel containing BifrostStreamChunk objects representing the stream or an error if the request fails.
func (provider *DeepSeekProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.DeepSeek,
			IsStreaming:               true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		return anthropic.HandleAnthropicChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/anthropic/v1/messages"),
			jsonData,
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			provider.networkConfig.BetaHeaderOverrides,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			schemas.DeepSeek,
			postHookRunner,
			nil,
			nil,
			provider.logger,
			postHookSpanFinalizer,
		)
	}

	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	request = applyDeepSeekThinkingCompatibility(request)
	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		schemas.DeepSeek,
		postHookRunner,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// Responses performs a Responses API request against DeepSeek's Anthropic-compatible endpoint.
func (provider *DeepSeekProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicResponsesRequest(
			ctx,
			provider.client,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/anthropic/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.DeepSeek,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	chatResponse, err := provider.ChatCompletion(ctx, key, request.ToChatRequest())
	if err != nil {
		return nil, err
	}

	response := chatResponse.ToBifrostResponsesResponse()

	return response, nil
}

// ResponsesStream performs a streaming Responses API request to DeepSeek's Anthropic-compatible endpoint.
func (provider *DeepSeekProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.DeepSeek,
			IsStreaming:               true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		return anthropic.HandleAnthropicResponsesStream(
			ctx,
			provider.streamingClient,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/anthropic/v1/messages"),
			jsonData,
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			provider.networkConfig.BetaHeaderOverrides,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			provider.GetProviderKey(),
			postHookRunner,
			nil,
			nil,
			provider.logger,
			postHookSpanFinalizer,
		)
	}

	ctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
	return provider.ChatCompletionStream(
		ctx,
		postHookRunner,
		postHookSpanFinalizer,
		key,
		request.ToChatRequest(),
	)
}
