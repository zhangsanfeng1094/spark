// Package mistral implements the Mistral provider.
package mistral

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// MistralProvider implements the Provider interface for Mistral's API.
type MistralProvider struct {
	logger               schemas.Logger        // Logger for provider operations
	client               *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient      *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig        schemas.NetworkConfig // Network configuration including extra headers
	customProviderConfig *schemas.CustomProviderConfig
	sendBackRawRequest   bool // Whether to include raw request in BifrostResponse
	sendBackRawResponse  bool // Whether to include raw response in BifrostResponse
}

// NewMistralProvider creates a new Mistral provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewMistralProvider(config *schemas.ProviderConfig, logger schemas.Logger) *MistralProvider {
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
	// 	mistralResponsePool.Put(&schemas.BifrostResponse{})
	// }

	// Configure proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	// Set default BaseURL if not provided
	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = "https://api.mistral.ai"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &MistralProvider{
		logger:               logger,
		client:               client,
		streamingClient:      streamingClient,
		networkConfig:        config.NetworkConfig,
		customProviderConfig: config.CustomProviderConfig,
		sendBackRawRequest:   config.SendBackRawRequest,
		sendBackRawResponse:  config.SendBackRawResponse,
	}
}

// GetProviderKey returns the provider identifier for Mistral.
func (provider *MistralProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.Mistral, provider.customProviderConfig)
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
func (provider *MistralProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	req.SetRequestURI(provider.networkConfig.BaseURL + providerUtils.GetPathFromContext(ctx, "/v1/models"))
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value.GetValue())
	}

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := providerUtils.SetErrorLatency(ParseMistralError(resp), latency)
		return nil, bifrostErr
	}

	// Copy response body before releasing
	responseBody := append([]byte(nil), resp.Body()...)

	// Parse Mistral's response
	var mistralResponse MistralListModelsResponse
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, &mistralResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Create final response
	response := mistralResponse.ToBifrostListModelsResponse(key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)

	response.ExtraFields.Latency = latency.Milliseconds()

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		response.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

// ListModels performs a list models request to Mistral's API.
// Requests are made concurrently for improved performance.
func (provider *MistralProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// normalizeChatRequestForConversion returns the request unchanged for the stock Mistral
// provider. For custom aliases (e.g. a provider registered as "custom-mistral" with
// BaseProviderType=Mistral), it returns a shallow copy with Provider set to schemas.Mistral
// so the shared OpenAI converter applies Mistral-specific compatibility (max_completion_tokens
// → max_tokens, tool_choice struct → "any"). The caller's request is never mutated.
func (provider *MistralProvider) normalizeChatRequestForConversion(request *schemas.BifrostChatRequest) *schemas.BifrostChatRequest {
	if request == nil || provider.customProviderConfig == nil || request.Provider == schemas.Mistral {
		return request
	}
	normalized := *request
	normalized.Provider = schemas.Mistral
	return &normalized
}

// ChatCompletion performs a chat completion request to the Mistral API.
func (provider *MistralProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
		provider.normalizeChatRequestForConversion(request),
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		nil,
		ParseMistralError,
		nil,
		provider.logger,
	)
}

// ChatCompletionStream performs a streaming chat completion request to the Mistral API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses Mistral's OpenAI-compatible streaming format.
// Returns a channel containing BifrostStreamChunk objects representing the stream or an error if the request fails.
func (provider *MistralProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+"/v1/chat/completions",
		provider.normalizeChatRequestForConversion(request),
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		postHookRunner,
		nil,
		nil,
		ParseMistralError,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// Responses performs a responses request to the Mistral API.
func (provider *MistralProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	chatResponse, err := provider.ChatCompletion(ctx, key, request.ToChatRequest())
	if err != nil {
		return nil, err
	}

	response := chatResponse.ToBifrostResponsesResponse()

	return response, nil
}

// ResponsesStream performs a streaming responses request to the Mistral API.
func (provider *MistralProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	ctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
	return provider.ChatCompletionStream(
		ctx,
		postHookRunner,
		postHookSpanFinalizer,
		key,
		request.ToChatRequest(),
	)
}
