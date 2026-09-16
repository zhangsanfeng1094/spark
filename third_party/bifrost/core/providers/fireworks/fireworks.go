// Package fireworks implements the Fireworks AI provider and its utility functions.
package fireworks

import (
	"context"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// FireworksProvider implements the Provider interface for Fireworks AI's API.
type FireworksProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewFireworksProvider creates a new Fireworks AI provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewFireworksProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*FireworksProvider, error) {
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
		config.NetworkConfig.BaseURL = "https://api.fireworks.ai/inference"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &FireworksProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for Fireworks AI.
func (provider *FireworksProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.Fireworks
}

// anthropicHeaders builds the auth headers for Fireworks' Anthropic-compatible endpoint.
func (provider *FireworksProvider) anthropicHeaders(key schemas.Key) map[string]string {
	return openai.BearerAuthHeader(key)
}

// ListModels lists models for Fireworks AI from each key's configured models and aliases.
// Fireworks serverless has no usable OpenAI-style /v1/models endpoint (it returns
// "Error listing deployed models"), so models are sourced from config rather than a live
// API call, mirroring the Replicate non-deployment path.
func (provider *FireworksProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(ctx, keys, request, provider.listModelsByKey)
}

// listModelsByKey builds the model list for a single key from its configured models and
// aliases, without calling the upstream API. The configured set is the full catalog we
// can produce (Fireworks has no enumerable live endpoint), so the filtered pipeline path
// is forced regardless of request.Unfiltered — the pipeline skips config backfill when
// unfiltered (it assumes a live response supplies the catalog), which would otherwise
// yield an empty list and drop configured models/aliases from the unfiltered catalog view.
func (provider *FireworksProvider) listModelsByKey(_ *schemas.BifrostContext, key schemas.Key, _ *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return (&openai.OpenAIListModelsResponse{}).ToBifrostListModelsResponse(
		schemas.Fireworks,
		key.Models,
		key.BlacklistedModels,
		key.Aliases,
		false,
	), nil
}

// ChatCompletion performs a chat completion request to the Fireworks AI API.
func (provider *FireworksProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicChatCompletionRequest(
			ctx,
			provider.client,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.Fireworks,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
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

// ChatCompletionStream performs a streaming chat completion request to the Fireworks AI API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses Fireworks AI's OpenAI-compatible streaming format.
// Returns a channel containing BifrostStreamChunk objects representing the stream or an error if the request fails.
func (provider *FireworksProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Fireworks,
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
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			jsonData,
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			provider.networkConfig.BetaHeaderOverrides,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			schemas.Fireworks,
			postHookRunner,
			nil,
			nil,
			provider.logger,
			postHookSpanFinalizer,
		)
	}

	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		schemas.Fireworks,
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

// Responses performs a responses request to the Fireworks AI API.
func (provider *FireworksProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicResponsesRequest(
			ctx,
			provider.client,
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.Fireworks,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	return openai.HandleOpenAIResponsesRequest(
		ctx,
		provider.client,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
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

// ResponsesStream performs a streaming responses request to the Fireworks AI API.
func (provider *FireworksProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Fireworks,
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
			provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
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

	return openai.HandleOpenAIResponsesStreaming(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		postHookRunner,
		nil,
		nil,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}
