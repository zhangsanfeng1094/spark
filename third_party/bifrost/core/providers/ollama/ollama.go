// Package providers implements various LLM providers and their utility functions.
// This file contains the Ollama provider implementation.
package ollama

import (
	"context"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// OllamaProvider implements the Provider interface for Ollama's API.
type OllamaProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewOllamaProvider creates a new Ollama provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewOllamaProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*OllamaProvider, error) {
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

	// // Pre-warm response pools
	// for range config.ConcurrencyAndBufferSize.Concurrency {
	// 	ollamaResponsePool.Put(&schemas.BifrostResponse{})
	// }

	// Configure proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	// BaseURL is optional when keys have ollama_key_config with per-key URLs
	return &OllamaProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for Ollama.
func (provider *OllamaProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.Ollama
}

// getBaseURL resolves the base URL for a request from the per-key ollama_key_config.
// Each Ollama key must have its own URL configured and falls back to provider-level base_url if not configured.
func (provider *OllamaProvider) getBaseURL(key schemas.Key) string {
	if key.OllamaKeyConfig != nil && key.OllamaKeyConfig.URL.GetValue() != "" {
		return strings.TrimRight(key.OllamaKeyConfig.URL.GetValue(), "/")
	}
	if provider.networkConfig.BaseURL != "" {
		return strings.TrimRight(provider.networkConfig.BaseURL, "/")
	}
	return ""
}

// baseURLOrError returns the resolved base URL or a BifrostError when none is configured.
func (provider *OllamaProvider) baseURLOrError(key schemas.Key) (string, *schemas.BifrostError) {
	u := provider.getBaseURL(key)
	if u == "" {
		return "", providerUtils.NewBifrostOperationError(
			"no base URL configured: either set ollama_key_config.url on the key or set network_config.base_url",
			nil)
	}
	return u, nil
}

// listModelsByKey performs a list models request for a single Ollama key.
func (provider *OllamaProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
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

// ListModels performs a list models request to Ollama's API.
// Requests are made concurrently per key so that each backend is queried
// with its own URL (from ollama_key_config).
func (provider *OllamaProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to the Ollama API.
func (provider *OllamaProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
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

// ChatCompletionStream performs a streaming chat completion request to the Ollama API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses Ollama's OpenAI-compatible streaming format.
// Returns a channel containing BifrostResponse objects representing the stream or an error if the request fails.
func (provider *OllamaProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	baseURL, bifrostErr := provider.baseURLOrError(key)
	if bifrostErr != nil {
		return nil, bifrostErr
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
		schemas.Ollama,
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

// Responses performs a responses request to the Ollama API.
func (provider *OllamaProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	chatResponse, err := provider.ChatCompletion(ctx, key, request.ToChatRequest())
	if err != nil {
		return nil, err
	}

	response := chatResponse.ToBifrostResponsesResponse()

	return response, nil
}

// ResponsesStream performs a streaming responses request to the Ollama API.
func (provider *OllamaProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	ctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
	return provider.ChatCompletionStream(
		ctx,
		postHookRunner,
		postHookSpanFinalizer,
		key,
		request.ToChatRequest(),
	)
}
