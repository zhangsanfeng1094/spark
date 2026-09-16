// Package providers implements various LLM providers and their utility functions.
// This file contains the SGL provider implementation.
package sgl

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

// sglAnthropicVersion is the Anthropic API version sent as the "anthropic-version" header on
// SGLang's Anthropic-compatible endpoint.
const sglAnthropicVersion = "2023-06-01"

// SGLProvider implements the Provider interface for SGL's API.
type SGLProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewSGLProvider creates a new SGL provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewSGLProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*SGLProvider, error) {
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

	// Pre-warm response pools
	// for range config.ConcurrencyAndBufferSize.Concurrency {
	// 	sglResponsePool.Put(&schemas.BifrostResponse{})
	// }

	// Configure proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	// BaseURL is optional when keys have sgl_key_config with per-key URLs
	return &SGLProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for SGL.
func (provider *SGLProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.SGL
}

// getBaseURL resolves the base URL for a request from the per-key sgl_key_config.
// Each SGL key must have its own URL configured and falls back to provider-level base_url if not configured.
func (provider *SGLProvider) getBaseURL(key schemas.Key) string {
	if key.SGLKeyConfig != nil && key.SGLKeyConfig.URL.GetValue() != "" {
		return strings.TrimRight(key.SGLKeyConfig.URL.GetValue(), "/")
	}
	if provider.networkConfig.BaseURL != "" {
		return strings.TrimRight(provider.networkConfig.BaseURL, "/")
	}
	return ""
}

// anthropicHeaders builds the auth and version headers for SGL's Anthropic-compatible endpoint.
func anthropicHeaders(key schemas.Key) map[string]string {
	headers := openai.BearerAuthHeader(key)
	headers["anthropic-version"] = sglAnthropicVersion
	return headers
}

// baseURLOrError returns the resolved base URL or a BifrostError when none is configured.
func (provider *SGLProvider) baseURLOrError(key schemas.Key) (string, *schemas.BifrostError) {
	u := provider.getBaseURL(key)
	if u == "" {
		return "", providerUtils.NewBifrostOperationError(
			"no base URL configured: either set sgl_key_config.url on the key or set network_config.base_url",
			nil)
	}
	return u, nil
}

// listModelsByKey performs a list models request for a single SGL key,
// resolving the per-key URL so each backend is queried individually.
func (provider *SGLProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	url := baseURL + providerUtils.GetPathFromContext(ctx, "/v1/models")
	return openai.ListModelsByKey(
		ctx,
		provider.client,
		url,
		key,
		request.Unfiltered,
		provider.networkConfig.ExtraHeaders,
		provider.GetProviderKey(),
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
	)
}

// ListModels performs a list models request to SGL's API.
// Requests are made concurrently per key so that each backend is queried
// with its own URL (from sgl_key_config).
func (provider *SGLProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to the SGL API. When the key (or the
// resolved alias) has UseAnthropicEndpoints set, the request is routed through SGLang's
// Anthropic-compatible Messages endpoint instead of its OpenAI-compatible one.
func (provider *SGLProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicChatCompletionRequest(
			ctx,
			provider.client,
			baseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  provider.GetProviderKey(),
				BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		baseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
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

// ChatCompletionStream performs a streaming chat completion request to the SGL API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses SGL's OpenAI-compatible streaming format.
// Returns a channel containing BifrostResponse objects representing the stream or an error if the request fails.
func (provider *SGLProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		url := baseURL + providerUtils.GetPathFromContext(ctx, "/v1/messages")
		jsonData, bifrostErr := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  provider.GetProviderKey(),
			IsStreaming:               true,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		return anthropic.HandleAnthropicChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			url,
			jsonData,
			anthropicHeaders(key),
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

	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		baseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		schemas.SGL,
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

// Responses performs a responses request to the SGL API. When the key (or the resolved alias)
// has UseAnthropicEndpoints set, the request is routed through SGLang's Anthropic-compatible
// Messages endpoint directly (rather than falling back through ChatCompletion), since the
// Anthropic responses handler operates on the Responses-shaped request natively.
func (provider *SGLProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
		baseURL, bifrostErr := provider.baseURLOrError(key)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		return anthropic.HandleAnthropicResponsesRequest(
			ctx,
			provider.client,
			baseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  provider.GetProviderKey(),
				ValidateTools:             true,
				BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			anthropicHeaders(key),
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

// ResponsesStream performs a streaming responses request to the SGL API.
func (provider *SGLProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
		baseURL, bifrostErr := provider.baseURLOrError(key)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		url := baseURL + providerUtils.GetPathFromContext(ctx, "/v1/messages")
		jsonData, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  provider.GetProviderKey(),
			IsStreaming:               true,
			ValidateTools:             true,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		return anthropic.HandleAnthropicResponsesStream(
			ctx,
			provider.streamingClient,
			url,
			jsonData,
			anthropicHeaders(key),
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
