// Package openrouter implements the OpenRouter LLM provider.
package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// OpenRouterProvider implements the Provider interface for OpenRouter's API.
type OpenRouterProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewOpenRouterProvider creates a new OpenRouter provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewOpenRouterProvider(config *schemas.ProviderConfig, logger schemas.Logger) *OpenRouterProvider {
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
		config.NetworkConfig.BaseURL = "https://openrouter.ai/api"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &OpenRouterProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}
}

// GetProviderKey returns the provider identifier for OpenRouter.
func (provider *OpenRouterProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.OpenRouter
}

// validateKey verifies the API key is valid using OpenRouter's /v1/auth/key endpoint.
// OpenRouter's /v1/models endpoint doesn't require authentication, so list models
// will succeed even with an invalid key. This method catches invalid keys.
func (provider *OpenRouterProvider) validateKey(ctx *schemas.BifrostContext, key schemas.Key) *schemas.BifrostError {
	keyValue := key.Value.GetValue()
	if keyValue == "" {
		return nil // Skip validation for empty keys
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(provider.networkConfig.BaseURL + "/v1/auth/key")
	req.Header.SetMethod(http.MethodGet)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", keyValue))

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return bifrostErr
	}

	// Check for auth errors (401, 403)
	statusCode := resp.StatusCode()
	if statusCode == fasthttp.StatusUnauthorized || statusCode == fasthttp.StatusForbidden {
		return providerUtils.SetErrorLatency(openai.ParseOpenAIError(resp), latency)
	}

	// Any 4xx/5xx error indicates the key might be invalid
	if statusCode >= 400 {
		return providerUtils.SetErrorLatency(openai.ParseOpenAIError(resp), latency)
	}

	return nil
}

// fetchEmbeddingModels fetches OpenRouter's embedding-model catalog. Best-effort: any
// failure is logged and swallowed so it never fails the primary ListModels call.
func (provider *OpenRouterProvider) fetchEmbeddingModels(ctx *schemas.BifrostContext, key schemas.Key) []schemas.Model {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	req.SetRequestURI(provider.networkConfig.BaseURL + "/v1/embeddings/models")
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")
	if keyValue := key.Value.GetValue(); keyValue != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", keyValue))
	}

	_, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil || resp.StatusCode() != fasthttp.StatusOK {
		provider.logger.Debug("openrouter: failed to fetch embedding models, skipping")
		return nil
	}

	var embeddingResponse schemas.BifrostListModelsResponse
	if _, _, bifrostErr := providerUtils.HandleProviderResponse(resp.Body(), &embeddingResponse, nil, false, false); bifrostErr != nil {
		provider.logger.Debug("openrouter: failed to parse embedding models response, skipping")
		return nil
	}
	return embeddingResponse.Data
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
func (provider *OpenRouterProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	// Validate the key first using /v1/auth/key (only during provider add/update).
	// OpenRouter's /v1/models doesn't require auth, so we need this extra check.
	shouldValidate := false
	if v, ok := ctx.Value(schemas.BifrostContextKeyValidateKeys).(bool); ok && v {
		shouldValidate = true
		if validateErr := provider.validateKey(ctx, key); validateErr != nil {
			return nil, validateErr
		}
	}

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
	keyValue := key.Value.GetValue()
	if keyValue != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", keyValue))
	}

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		if shouldValidate {
			// Key is valid (validated above) but transport error on models endpoint.
			// Return empty response; allowed models will be backfilled below.
			return &schemas.BifrostListModelsResponse{}, nil
		}
		return nil, bifrostErr
	}

	// Handle error response
	modelsFetched := true
	if resp.StatusCode() != fasthttp.StatusOK {
		if shouldValidate {
			// Key is valid (validated above) but /v1/models returned error (e.g., privacy/guardrail settings).
			// Continue with empty response; allowed models will be backfilled below.
			modelsFetched = false
		} else {
			bifrostErr := providerUtils.SetErrorLatency(openai.ParseOpenAIError(resp), latency)
			return nil, bifrostErr
		}
	}

	var openrouterResponse schemas.BifrostListModelsResponse
	if modelsFetched {
		// Copy response body before releasing
		responseBody := append([]byte(nil), resp.Body()...)

		// Pass nil requestBody for GET requests - HandleProviderResponse will skip raw request capture
		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, &openrouterResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		// Set raw request if enabled
		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			openrouterResponse.ExtraFields.RawRequest = rawRequest
		}

		// Set raw response if enabled
		if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
			openrouterResponse.ExtraFields.RawResponse = rawResponse
		}
	}

	// Merge in the embedding-model catalog, which OpenRouter's default /v1/models
	// response omits entirely.
	if modelsFetched {
		if embeddingModels := provider.fetchEmbeddingModels(ctx, key); len(embeddingModels) > 0 {
			existing := make(map[string]bool, len(openrouterResponse.Data))
			for _, m := range openrouterResponse.Data {
				existing[strings.ToLower(m.ID)] = true
			}
			for _, m := range embeddingModels {
				if !existing[strings.ToLower(m.ID)] {
					openrouterResponse.Data = append(openrouterResponse.Data, m)
					existing[strings.ToLower(m.ID)] = true
				}
			}
		}
	}

	// OpenRouter model IDs in the API response do NOT include the "openrouter/" prefix
	// (e.g. the API returns "openai/gpt-4", not "openrouter/openai/gpt-4").
	// Users may supply allowedModels / aliases with or without the prefix, so we
	// normalize both by stripping it before feeding into the shared pipeline.
	providerPrefix := string(schemas.OpenRouter) + "/"
	stripPrefix := func(s string) string {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(providerPrefix)) {
			return s[len(providerPrefix):]
		}
		return s
	}

	normalizedAllowed := make(schemas.WhiteList, 0, len(key.Models))
	for _, m := range key.Models {
		normalizedAllowed = append(normalizedAllowed, stripPrefix(m))
	}
	normalizedBlacklist := make(schemas.BlackList, 0, len(key.BlacklistedModels))
	for _, m := range key.BlacklistedModels {
		normalizedBlacklist = append(normalizedBlacklist, stripPrefix(m))
	}
	normalizedAliases := make(schemas.KeyAliases, len(key.Aliases))
	for k, v := range key.Aliases {
		cfg := v
		cfg.ModelID = stripPrefix(v.ModelID)
		normalizedAliases[stripPrefix(k)] = cfg
	}

	pipeline := &providerUtils.ListModelsPipeline{
		AllowedModels:     normalizedAllowed,
		BlacklistedModels: normalizedBlacklist,
		Aliases:           normalizedAliases,
		Unfiltered:        request.Unfiltered,
		ProviderKey:       schemas.OpenRouter,
		MatchFns:          providerUtils.DefaultMatchFns(),
	}

	if pipeline.ShouldEarlyExit() {
		openrouterResponse.Data = make([]schemas.Model, 0)
	} else {
		included := make(map[string]bool)
		filteredData := make([]schemas.Model, 0, len(openrouterResponse.Data))
		for i := range openrouterResponse.Data {
			// rawID has no "openrouter/" prefix — e.g. "openai/gpt-4"
			rawID := openrouterResponse.Data[i].ID
			for _, result := range pipeline.FilterModel(rawID) {
				entry := openrouterResponse.Data[i]
				entry.ID = providerPrefix + result.ResolvedID
				if result.AliasValue != "" {
					entry.Alias = schemas.Ptr(result.AliasValue)
				} else {
					entry.Alias = nil
				}
				filteredData = append(filteredData, entry)
				included[strings.ToLower(result.ResolvedID)] = true
			}
		}
		filteredData = append(filteredData, pipeline.BackfillModels(included)...)
		openrouterResponse.Data = filteredData
	}

	openrouterResponse.ExtraFields.Latency = latency.Milliseconds()

	return &openrouterResponse, nil
}

// ListModels performs a list models request to OpenRouter's API.
// Requests are made concurrently for improved performance.
func (provider *OpenRouterProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to the OpenRouter API.
func (provider *OpenRouterProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
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

// ChatCompletionStream performs a streaming chat completion request to the OpenRouter API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses OpenRouter's OpenAI-compatible streaming format.
// Returns a channel containing BifrostStreamChunk objects representing the stream or an error if the request fails.
func (provider *OpenRouterProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
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
		schemas.OpenRouter,
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

// Responses performs a responses request to the OpenRouter API.
func (provider *OpenRouterProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
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

// ResponsesStream performs a streaming responses request to the OpenRouter API.
func (provider *OpenRouterProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
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
