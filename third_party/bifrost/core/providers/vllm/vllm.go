// Package vllm implements the vLLM LLM provider (OpenAI-compatible).
package vllm

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

// VLLMProvider implements the Provider interface for vLLM's OpenAI-compatible API.
type VLLMProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewVLLMProvider creates a new vLLM provider instance.
func NewVLLMProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*VLLMProvider, error) {
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

	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	// BaseURL is optional when keys have vllm_key_config with per-key URLs
	return &VLLMProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for vLLM.
func (provider *VLLMProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.VLLM
}

// getBaseURL resolves the base URL for a request from the per-key vllm_key_config.
// Each vLLM key must have its own URL configured — there is no provider-level fallback.
func (provider *VLLMProvider) getBaseURL(key schemas.Key) string {
	if key.VLLMKeyConfig != nil && key.VLLMKeyConfig.URL.GetValue() != "" {
		return strings.TrimRight(key.VLLMKeyConfig.URL.GetValue(), "/")
	}
	return ""
}

// anthropicHeaders builds the auth headers for vLLM's Anthropic-compatible endpoint.
func (provider *VLLMProvider) anthropicHeaders(key schemas.Key) map[string]string {
	return openai.BearerAuthHeader(key)
}

// baseURLOrError returns the resolved base URL or a BifrostError when none is configured.
func (provider *VLLMProvider) baseURLOrError(key schemas.Key) (string, *schemas.BifrostError) {
	u := provider.getBaseURL(key)
	if u == "" {
		return "", providerUtils.NewBifrostOperationError(
			"no base URL configured: set vllm_key_config.url on the key",
			nil)
	}
	return u, nil
}

// listModelsByKey performs a list models request for a single vLLM key,
// resolving the per-key URL so each backend is queried individually.
func (provider *VLLMProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
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

// ListModels performs a list models request to vLLM's API.
// Requests are made concurrently per key so that each backend is queried
// with its own URL (from vllm_key_config).
func (provider *VLLMProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to vLLM's API.
func (provider *VLLMProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
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
				Provider:                  schemas.VLLM,
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
		HandleVLLMResponse,
		nil,
		nil,
		provider.logger,
	)
}

// ChatCompletionStream performs a streaming chat completion request to vLLM's API.
func (provider *VLLMProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.VLLM,
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
			baseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			jsonData,
			provider.anthropicHeaders(key),
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			provider.networkConfig.BetaHeaderOverrides,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			schemas.VLLM,
			postHookRunner,
			nil,
			nil,
			provider.logger,
			postHookSpanFinalizer,
		)
	}

	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
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
		provider.GetProviderKey(),
		postHookRunner,
		nil,
		HandleVLLMResponse,
		nil,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// Responses performs a responses request to vLLM's API.
func (provider *VLLMProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		return anthropic.HandleAnthropicResponsesRequest(
			ctx,
			provider.client,
			baseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.VLLM,
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
	return openai.HandleOpenAIResponsesRequest(
		ctx,
		provider.client,
		baseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		HandleVLLMResponse,
		nil,
		nil,
		provider.logger,
	)
}

// ResponsesStream performs a streaming responses request to vLLM's API.
func (provider *VLLMProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if anthropic.ResolveUseAnthropicEndpoints(ctx, key) {
		jsonData, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.VLLM,
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
			baseURL+providerUtils.GetPathFromContext(ctx, "/v1/messages"),
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

	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
	return openai.HandleOpenAIResponsesStreaming(
		ctx,
		provider.streamingClient,
		baseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		postHookRunner,
		HandleVLLMResponse,
		nil,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}
