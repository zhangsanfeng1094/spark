// Package openai provides the OpenAI provider implementation for the Bifrost framework.
package openai

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// OpenAIProvider implements the Provider interface for OpenAI's GPT API.
type OpenAIProvider struct {
	logger               schemas.Logger                // Logger for provider operations
	client               *fasthttp.Client              // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient      *fasthttp.Client              // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig        schemas.NetworkConfig         // Network configuration including extra headers
	sendBackRawRequest   bool                          // Whether to include raw request in BifrostResponse
	sendBackRawResponse  bool                          // Whether to include raw response in BifrostResponse
	customProviderConfig *schemas.CustomProviderConfig // Custom provider config
	disableStore         bool                          // Whether to force store=false on outgoing requests
}

// NewOpenAIProvider creates a new OpenAI provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewOpenAIProvider(config *schemas.ProviderConfig, logger schemas.Logger) *OpenAIProvider {
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
	// 	openAIResponsePool.Put(&schemas.BifrostResponse{})
	// }

	// Configure proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	// Set default BaseURL if not provided
	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = "https://api.openai.com"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &OpenAIProvider{
		logger:               logger,
		client:               client,
		streamingClient:      streamingClient,
		networkConfig:        config.NetworkConfig,
		sendBackRawRequest:   config.SendBackRawRequest,
		sendBackRawResponse:  config.SendBackRawResponse,
		customProviderConfig: config.CustomProviderConfig,
		disableStore:         config.OpenAIConfig != nil && config.OpenAIConfig.DisableStore,
	}
}

// GetProviderKey returns the provider identifier for OpenAI.
func (provider *OpenAIProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.OpenAI, provider.customProviderConfig)
}

// buildRequestURL constructs the full request URL using the provider's configuration.
func (provider *OpenAIProvider) buildRequestURL(ctx *schemas.BifrostContext, defaultPath string, requestType schemas.RequestType) string {
	path, isCompleteURL := providerUtils.GetRequestPath(ctx, defaultPath, provider.customProviderConfig, requestType)
	if isCompleteURL {
		return path
	}
	return provider.networkConfig.BaseURL + path
}

func (provider *OpenAIProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.OpenAI, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
		return nil, err
	}
	providerName := provider.GetProviderKey()

	if provider.customProviderConfig != nil && provider.customProviderConfig.IsKeyLess {
		return providerUtils.HandleKeylessListModelsRequest(providerName, func() (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
			return ListModelsByKey(
				ctx,
				provider.client,
				provider.buildRequestURL(ctx, "/v1/models", schemas.ListModelsRequest),
				schemas.Key{Models: schemas.WhiteList{"*"}},
				request.Unfiltered,
				provider.networkConfig.ExtraHeaders,
				providerName,
				providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
				providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			)
		})
	}

	return HandleOpenAIListModelsRequest(ctx,
		provider.client,
		request,
		provider.buildRequestURL(ctx, "/v1/models", schemas.ListModelsRequest),
		keys,
		provider.networkConfig.ExtraHeaders,
		providerName,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
	)
}

// ListModelsByKey performs a list models request for a single key.
// Returns the list-models response, or an error if the request fails.
func ListModelsByKey(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	key schemas.Key,
	unfiltered bool,
	extraHeaders map[string]string,
	providerName schemas.ModelProvider,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	req.SetRequestURI(url)
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")

	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value.GetValue())
	}

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	// Extract provider response headers early so they're available on error paths too
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := ParseOpenAIError(resp)
		return nil, providerUtils.SetErrorLatency(bifrostErr, latency)
	}

	// Copy response body before releasing
	responseBody := append([]byte(nil), resp.Body()...)

	openaiResponse := &OpenAIListModelsResponse{}

	// Use enhanced response handler with pre-allocated response
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, openaiResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, providerUtils.SetErrorLatency(bifrostErr, latency)
	}

	response := openaiResponse.ToBifrostListModelsResponse(providerName, key.Models, key.BlacklistedModels, key.Aliases, unfiltered)

	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, sendBackRawRequest) {
		response.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

// BearerAuthHeader builds the auth header map for OpenAI-compatible providers that authenticate
// with a bearer token. It returns an empty (non-nil) map when the key carries no value (e.g.
// SigV4 / header-based auth supplied via extraHeaders), so callers can pass it directly to the
// Handle*Request functions that take an authHeader map.
func BearerAuthHeader(key schemas.Key) map[string]string {
	headers := map[string]string{}
	if key.Value.GetValue() != "" {
		headers["Authorization"] = "Bearer " + key.Value.GetValue()
	}
	return headers
}

// HandleOpenAIListModelsRequest handles a list models request to OpenAI's API.
func HandleOpenAIListModelsRequest(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	request *schemas.BifrostListModelsRequest,
	url string,
	keys []schemas.Key,
	extraHeaders map[string]string,
	providerName schemas.ModelProvider,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if len(keys) == 0 {
		return ListModelsByKey(ctx, client, url, schemas.Key{}, request.Unfiltered, extraHeaders, providerName, sendBackRawRequest, sendBackRawResponse)
	}
	listModelsByKeyWrapper := func(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
		return ListModelsByKey(ctx, client, url, key, request.Unfiltered, extraHeaders, providerName, sendBackRawRequest, sendBackRawResponse)
	}
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		listModelsByKeyWrapper,
	)
}


// ChatCompletion performs a chat completion request to the OpenAI API.
// It supports both text and image content in messages.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *OpenAIProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	// Check if chat completion is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.OpenAI, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	if provider.disableStore {
		if request.Params == nil {
			request.Params = &schemas.ChatParameters{}
		}
		request.Params.Store = schemas.Ptr(false)
	}

	return HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		provider.buildRequestURL(ctx, "/v1/chat/completions", schemas.ChatCompletionRequest),
		request,
		BearerAuthHeader(key),
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

// HandleOpenAIChatCompletionRequest handles a chat completion request to OpenAI's API.
func HandleOpenAIChatCompletionRequest(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	request *schemas.BifrostChatRequest,
	authHeader map[string]string,
	extraHeaders map[string]string,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	customResponseHandler responseHandler[schemas.BifrostChatResponse],
	customErrorConverter ErrorConverter,
	signer providerUtils.BodySigner,
	logger schemas.Logger,
) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	// resp lifecycle: managed by finalizeOpenAIResponse or released on error paths
	respOwned := true
	defer func() {
		if respOwned {
			fasthttp.ReleaseResponse(resp)
		}
	}()
	activeClient := providerUtils.PrepareResponseStreaming(ctx, client, resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	req.SetRequestURI(url)
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")

	for k, v := range authHeader {
		req.Header.Set(k, v)
	}

	// Large payload passthrough: stream body directly without JSON marshaling
	if lpResult, lpErr, handled := handleOpenAILargePayloadPassthrough(ctx, client, url, authHeader, extraHeaders, providerName, logger); handled {
		if lpErr != nil {
			return nil, lpErr
		}
		if len(lpResult.ResponseBody) > 0 {
			response := &schemas.BifrostChatResponse{}
			if err := sonic.Unmarshal(lpResult.ResponseBody, response); err != nil {
				return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, err)
			}
			response.ExtraFields = schemas.BifrostResponseExtraFields{Latency: lpResult.Latency}
			return response, nil
		}
		return &schemas.BifrostChatResponse{
			Model:       request.Model,
			Usage:       lpResult.Usage,
			ExtraFields: schemas.BifrostResponseExtraFields{Latency: lpResult.Latency},
		}, nil
	}

	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody := ToOpenAIChatRequest(ctx, request)
			// Resolved here rather than inside the converter, so a failed document fetch surfaces
			// as itself instead of as a provider 400 about a missing file_id.
			if err := ResolveChatFileURLs(ctx, request.Provider, reqBody); err != nil {
				return nil, err
			}
			return reqBody, nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if signer != nil {
		sigHeaders, bErr := signer(jsonData)
		if bErr != nil {
			return nil, bErr
		}
		for k, v := range sigHeaders {
			req.Header.Set(k, v)
		}
	}

	req.SetBody(jsonData)

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}
	// Extract provider response headers early so they're available on error paths too
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		logger.Debug("error from %s provider: status %d", providerName, resp.StatusCode())
		if customErrorConverter != nil {
			return nil, providerUtils.EnrichError(ctx, customErrorConverter(resp), jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, ParseOpenAIError(resp), jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	body, lpResult, finalErr := finalizeOpenAIResponse(ctx, resp, latency, providerName, logger)
	respOwned = false // ownership transferred
	if finalErr != nil {
		return nil, providerUtils.EnrichError(ctx, finalErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}
	if lpResult != nil {
		return &schemas.BifrostChatResponse{
			Model:       request.Model,
			Usage:       lpResult.Usage,
			ExtraFields: schemas.BifrostResponseExtraFields{Latency: lpResult.Latency},
		}, nil
	}
	response := &schemas.BifrostChatResponse{}
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	var rawRequest, rawResponse interface{}

	if customResponseHandler != nil {
		rawRequest, rawResponse, bifrostErr = customResponseHandler(body, response, jsonData, sendBackRawRequest, sendBackRawResponse)
	} else {
		rawRequest, rawResponse, bifrostErr = providerUtils.HandleProviderResponseCtx(ctx, body, response, jsonData, sendBackRawRequest, sendBackRawResponse)
	}

	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, body, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// A 200 is not proof of success on every OpenAI-compatible provider: some report
	// failures in-band once the status line is already committed. Left unchecked those
	// surface to the caller as a 200 with null choices and null usage.
	// The non-custom path already validated body via HandleProviderResponse's
	// sonic.Unmarshal, so it skips the redundant gjson.ValidBytes rescan. A custom
	// handler makes no such guarantee, so validate before scanning to avoid reading an
	// "error" object out of malformed JSON.
	var inBandErr *schemas.BifrostError
	if customResponseHandler != nil {
		inBandErr = ErrorInSuccessfulChatBody(body)
	} else {
		inBandErr = errorInValidatedChatBody(body)
	}
	if inBandErr != nil {
		logger.Debug("in-band error on a 200 from %s provider: %s", providerName, inBandErr.Error.Message)
		return nil, providerUtils.EnrichError(ctx, inBandErr, jsonData, body, sendBackRawRequest, sendBackRawResponse, latency)
	}

	response.ExtraFields.Latency = latency.Milliseconds()

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, sendBackRawRequest) {
		response.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, sendBackRawResponse) {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

// ChatCompletionStream handles streaming for OpenAI chat completions.
// It formats messages, prepares request body, and uses shared streaming logic.
// Returns a channel for streaming responses and any error that occurred.
func (provider *OpenAIProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Check if chat completion stream is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.OpenAI, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}
	if provider.disableStore {
		if request.Params == nil {
			request.Params = &schemas.ChatParameters{}
		}
		request.Params.Store = schemas.Ptr(false)
	}

	// Use shared streaming logic
	return HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		provider.buildRequestURL(ctx, "/v1/chat/completions", schemas.ChatCompletionStreamRequest),
		request,
		BearerAuthHeader(key),
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
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// HandleOpenAIChatCompletionStreaming handles streaming for OpenAI-compatible APIs.
// This shared function reduces code duplication between providers that use the same SSE format.
func HandleOpenAIChatCompletionStreaming(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	request *schemas.BifrostChatRequest,
	authHeader map[string]string,
	extraHeaders map[string]string,
	streamIdleTimeoutInSeconds int,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	postHookRunner schemas.PostHookRunner,
	customRequestConverter func(*schemas.BifrostChatRequest) (providerUtils.RequestBodyWithExtraParams, error),
	customResponseHandler responseHandler[schemas.BifrostChatResponse],
	customErrorConverter ErrorConverter,
	postRequestConverter func(*OpenAIChatRequest) *OpenAIChatRequest,
	postResponseConverter func(*schemas.BifrostChatResponse) *schemas.BifrostChatResponse,
	signer providerUtils.BodySigner,
	logger schemas.Logger,
	postHookSpanFinalizer func(context.Context),
) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, streamIdleTimeoutInSeconds)
	// Check if the request is a redirect from ResponsesStream to ChatCompletionStream
	isResponsesToChatCompletionsFallback := false
	var responsesStreamState *schemas.ChatToResponsesStreamState
	if ctx.Value(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback) != nil {
		isResponsesToChatCompletionsFallbackValue, ok := ctx.Value(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback).(bool)
		if ok && isResponsesToChatCompletionsFallbackValue {
			isResponsesToChatCompletionsFallback = true
			responsesStreamState = schemas.AcquireChatToResponsesStreamState()
		}
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}

	if authHeader != nil {
		// Copy auth header to headers
		maps.Copy(headers, authHeader)
	}

	jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			if customRequestConverter != nil {
				return customRequestConverter(request)
			}
			reqBody := ToOpenAIChatRequest(ctx, request)
			// Same resolution the non-streaming path does: streaming rejects file_url just as
			// hard, so a URL-sourced document has to be inlined here too.
			if err := ResolveChatFileURLs(ctx, request.Provider, reqBody); err != nil {
				return nil, err
			}
			if reqBody != nil {
				reqBody.Stream = new(true)
				reqBody.StreamOptions = &schemas.ChatStreamOptions{
					IncludeUsage: new(true),
				}
				if postRequestConverter != nil {
					reqBody = postRequestConverter(reqBody)
				}
			}
			return reqBody, nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Create HTTP request for streaming
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	// Updating request
	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(url)
	req.Header.SetContentType("application/json")

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if signer != nil {
		sigHeaders, bErr := signer(jsonBody)
		if bErr != nil {
			defer providerUtils.ReleaseStreamingResponse(ctx, resp)
			return nil, bErr
		}
		for k, v := range sigHeaders {
			req.Header.Set(k, v)
		}
	}

	setStreamingRequestBody(ctx, req, jsonBody, providerName)

	// Use streaming-aware client when large payload optimization is active — ensures
	// MaxResponseBodySize > 0 so ErrBodyTooLarge triggers StreamBody for Content-Length responses.
	activeClient := providerUtils.PrepareResponseStreaming(ctx, client, resp)

	startTime := time.Now()
	// Make the request
	err := providerUtils.DoStreamingRequest(ctx, activeClient, req, resp)
	latency := time.Since(startTime)
	if err != nil {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		if errors.Is(err, context.Canceled) {
			return nil, providerUtils.EnrichError(ctx, &schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   err,
				},
			}, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		if errors.Is(err, fasthttp.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		// The request failed before the first response byte (connection refused, server
		// closed an idle/pooled connection, broken pipe, DNS failure, etc.). Mirror the
		// non-streaming path (makeRequestWithDoFunc) and surface this as a retriable upstream
		// connection error (502, IsBifrostError=false) rather than NewBifrostOperationError
		// (500, IsBifrostError=true). The latter caused the retry loop in executeRequestWithRetries
		// to break early on IsBifrostError, so max_retries never applied to streaming connection
		// failures - see https://github.com/maximhq/bifrost/issues/4496.
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostUpstreamConnectionError(schemas.ErrProviderDoRequest, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Store provider response headers in context before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		if customErrorConverter != nil {
			return nil, providerUtils.EnrichError(ctx, customErrorConverter(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, ParseOpenAIError(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Large payload streaming passthrough — pipe raw upstream SSE to client
	if providerUtils.SetupStreamingPassthrough(ctx, resp) {
		responseChan := make(chan *schemas.BifrostStreamChunk)
		providerUtils.CloseStream(ctx, responseChan)
		return responseChan, nil
	}

	// Create response channel
	responseChan := make(chan *schemas.BifrostStreamChunk, schemas.DefaultStreamBufferSize)

	// Start streaming in a goroutine
	go func() {
		defer providerUtils.EnsureStreamFinalizerCalled(ctx, postHookSpanFinalizer)
		defer func() {
			if ctx.Err() == context.Canceled {
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
			}
			// Release the responses stream state if it was acquired (for ResponsesToChatCompletions fallback)
			schemas.ReleaseChatToResponsesStreamState(responsesStreamState)
			providerUtils.CloseStream(ctx, responseChan)
		}()
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		// Decompress gzip-encoded streams transparently (no-op for non-gzip)
		reader, releaseGzip := providerUtils.DecompressStreamBody(resp)
		defer releaseGzip()

		// Wrap reader with idle timeout to detect stalled streams.
		reader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(reader, resp.BodyStream(), providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close the raw network stream on ctx cancellation,
		// which immediately unblocks any in-progress read (including reads blocked inside a gzip decompression layer).
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), logger)
		defer stopCancellation()

		// Skip scanner for non-SSE responses — avoids bufio.Scanner buffer bloat
		// on non-line-delimited data (e.g. provider returned JSON instead of SSE).
		reader, drained := providerUtils.DrainNonSSEStreamReader(resp, reader)
		if drained {
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			providerUtils.ProcessAndSendError(ctx, postHookRunner, errors.New("provider returned non-SSE response for streaming request"), responseChan, logger, postHookSpanFinalizer)
			return
		}

		sseReader := providerUtils.GetSSEDataReader(ctx, reader)

		chunkIndex := -1
		usage := &schemas.BifrostLLMUsage{}
		// Register the accumulating usage handle so a mid-stream
		// cancel/timeout can bill for tokens the provider already processed.
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, usage)

		lastChunkTime := startTime

		var finishReason *string
		var messageID string
		var modelName string
		var created int
		// service_tier is echoed on chunks; propagate to the final chunk for priority/flex billing
		var serviceTier *schemas.BifrostServiceTier
		forwardedTerminalFinishReason := false
		// Defer final completed/incomplete event until usage chunk arrives (fallback path only).
		var pendingFinalEvent *schemas.BifrostResponsesStreamResponse
		usageSeen := false
		// Fallback path only: tracks whether the upstream ever sent a finish_reason,
		// so a finish_reason that fails to produce a terminal event (e.g. a future
		// regression in ToBifrostResponsesStreamResponse) is treated as truncation
		// instead of a silent stream close.
		fallbackFinishReasonSeen := false

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			data, readErr := sseReader.ReadDataLine()
			if readErr != nil {
				if ctx.Err() != nil {
					return
				}
				if readErr != io.EOF {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					logger.Warn("Error reading stream: %v", readErr)
					providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, logger, postHookSpanFinalizer)
					return
				}
				break
			}
			jsonData := string(data)

			// Quick check for error field (allocation-free using sonic.GetFromString)
			if errorNode, _ := sonic.GetFromString(jsonData, "error"); errorNode.Exists() {
				// Only unmarshal when we know there's an error
				var bifrostErr schemas.BifrostError
				if err := sonic.UnmarshalString(jsonData, &bifrostErr); err == nil {
					if bifrostErr.Error != nil && bifrostErr.Error.Message != "" {
						ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
						providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, &bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
						return
					}
				}
			}

			// Parse into bifrost response
			var response schemas.BifrostChatResponse
			if customResponseHandler != nil {
				// Custom handler decodes the raw event itself -> time as "response-parse" stream phase.
				parseStart := time.Now()
				rawRequest, rawResponse, handlerErr := customResponseHandler([]byte(jsonData), &response, nil, sendBackRawRequest, sendBackRawResponse)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if handlerErr != nil {
					if sendBackRawRequest {
						handlerErr.ExtraFields.RawRequest = rawRequest
					}
					if sendBackRawResponse {
						handlerErr.ExtraFields.RawResponse = rawResponse
					}
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, handlerErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
					return
				}
			} else {
				// Per-event decode -> "response-parse" (Serialization) stream phase.
				parseStart := time.Now()
				umErr := sonic.UnmarshalString(jsonData, &response)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if umErr != nil {
					logger.Warn("Failed to parse stream response: %v", umErr)
					continue
				}
			}

			// choices be array if nil
			if response.Choices == nil {
				response.Choices = []schemas.BifrostResponseChoice{}
			}

			if isResponsesToChatCompletionsFallback {
				if len(response.Choices) > 0 && response.Choices[0].FinishReason != nil && *response.Choices[0].FinishReason != "" {
					fallbackFinishReasonSeen = true
				}

				// Accumulate usage across chunks; attached to final event below.
				if response.Usage != nil {
					usageSeen = true
					if response.Usage.PromptTokens > usage.PromptTokens {
						usage.PromptTokens = response.Usage.PromptTokens
					}
					if response.Usage.CompletionTokens > usage.CompletionTokens {
						usage.CompletionTokens = response.Usage.CompletionTokens
					}
					if response.Usage.TotalTokens > usage.TotalTokens {
						usage.TotalTokens = response.Usage.TotalTokens
					}
					if calculatedTotal := usage.PromptTokens + usage.CompletionTokens; calculatedTotal > usage.TotalTokens {
						usage.TotalTokens = calculatedTotal
					}
					if response.Usage.PromptTokensDetails != nil {
						usage.PromptTokensDetails = response.Usage.PromptTokensDetails
					}
					if response.Usage.CompletionTokensDetails != nil {
						usage.CompletionTokensDetails = response.Usage.CompletionTokensDetails
					}
					if response.Usage.Cost != nil {
						usage.Cost = response.Usage.Cost
					}
				}

				// Per-event mapping (chat->responses) -> "convertor" (Convertor) stream phase.
				convStart := time.Now()
				spreadResponses := response.ToBifrostResponsesStreamResponse(responsesStreamState)
				schemas.AddStreamConvert(ctx, time.Since(convStart))
				for _, response := range spreadResponses {
					if response.Type == schemas.ResponsesStreamResponseTypeError {
						bifrostErr := &schemas.BifrostError{
							Type:           schemas.Ptr(string(schemas.ResponsesStreamResponseTypeError)),
							IsBifrostError: false,
							Error:          &schemas.ErrorField{},
						}

						if response.Message != nil {
							bifrostErr.Error.Message = *response.Message
						}
						if response.Param != nil {
							bifrostErr.Error.Param = *response.Param
						}
						if response.Code != nil {
							bifrostErr.Error.Code = response.Code
						}

						ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
						providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
						return
					}

					response.ExtraFields.ChunkIndex = response.SequenceNumber

					if sendBackRawResponse {
						response.ExtraFields.RawResponse = jsonData
					}

					if response.Type == schemas.ResponsesStreamResponseTypeCompleted || response.Type == schemas.ResponsesStreamResponseTypeIncomplete {
						// Defer sending until stream end so usage can be attached.
						pendingFinalEvent = response
						continue
					}

					response.ExtraFields.Latency = time.Since(lastChunkTime).Milliseconds()
					lastChunkTime = time.Now()

					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
				}
			} else {
				if postResponseConverter != nil {
					// Per-event mapping -> "convertor" (Convertor) stream phase.
					convStart := time.Now()
					converted := postResponseConverter(&response)
					schemas.AddStreamConvert(ctx, time.Since(convStart))
					if converted != nil {
						response = *converted
					} else {
						logger.Warn("postResponseConverter returned nil; leaving chunk unmodified")
					}
				}

				if response.ServiceTier != nil {
					serviceTier = response.ServiceTier
				}

				// Handle usage-only chunks (when stream_options include_usage is true)
				if response.Usage != nil {
					// Collect usage information and send at the end of the stream
					// Here in some cases usage comes before final message
					// So we need to check if the response.Usage is nil and then if usage != nil
					// then add up all tokens
					if response.Usage.PromptTokens > usage.PromptTokens {
						usage.PromptTokens = response.Usage.PromptTokens
					}
					if response.Usage.CompletionTokens > usage.CompletionTokens {
						usage.CompletionTokens = response.Usage.CompletionTokens
					}
					if response.Usage.TotalTokens > usage.TotalTokens {
						usage.TotalTokens = response.Usage.TotalTokens
					}
					calculatedTotal := usage.PromptTokens + usage.CompletionTokens
					if calculatedTotal > usage.TotalTokens {
						usage.TotalTokens = calculatedTotal
					}
					if response.Usage.PromptTokensDetails != nil {
						usage.PromptTokensDetails = response.Usage.PromptTokensDetails
					}
					if response.Usage.CompletionTokensDetails != nil {
						usage.CompletionTokensDetails = response.Usage.CompletionTokensDetails
					}
					if response.Usage.Cost != nil {
						usage.Cost = response.Usage.Cost
					}
					response.Usage = nil
				}

				if response.Model != "" {
					modelName = response.Model
				}

				// Skip empty responses or responses without choices
				if len(response.Choices) == 0 {
					continue
				}

				// Handle finish reason, usually in the final chunk
				choice := response.Choices[0]
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					// Collect finish reason and send at the end of the stream
					finishReason = choice.FinishReason
				}

				if response.ID != "" && messageID == "" {
					messageID = response.ID
				}
				if response.Created != 0 && created == 0 {
					created = response.Created
				}

				// Handle regular content chunks, including reasoning
				if choice.ChatStreamResponseChoice != nil &&
					choice.ChatStreamResponseChoice.Delta != nil &&
					((choice.ChatStreamResponseChoice.Delta.Content != nil && *choice.ChatStreamResponseChoice.Delta.Content != "") ||
						choice.ChatStreamResponseChoice.Delta.Reasoning != nil ||
						len(choice.ChatStreamResponseChoice.Delta.ReasoningDetails) > 0 ||
						choice.ChatStreamResponseChoice.Delta.Audio != nil ||
						len(choice.ChatStreamResponseChoice.Delta.ToolCalls) > 0) {
					if choice.FinishReason != nil && *choice.FinishReason != "" {
						forwardedTerminalFinishReason = true
					}
					chunkIndex++

					response.ExtraFields.ChunkIndex = chunkIndex
					response.ExtraFields.Latency = time.Since(lastChunkTime).Milliseconds()
					lastChunkTime = time.Now()

					if sendBackRawResponse {
						response.ExtraFields.RawResponse = jsonData
					}

					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, &response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
				}

				// For providers that don't send [DONE] marker break on finish_reason
				if !providerUtils.ProviderSendsDoneMarker(providerName) && finishReason != nil {
					break
				}
			}
		}

		// A dying upstream closes the connection on a chunk boundary, which fasthttp
		// reports as a plain io.EOF — the same read result as a properly terminated
		// body. Truncation is therefore only detectable semantically: require an
		// explicit [DONE], a finish_reason, or (on the fallback path) a terminal
		// Responses event before synthesizing a final chunk. Without one, emitting
		// the synthetic chunk would hand the client a clean, content-free completion
		// that is indistinguishable from a provider that generated zero tokens.
		terminalSignalSeen := providerUtils.SSEStreamEndedOnMarker(sseReader)
		if isResponsesToChatCompletionsFallback {
			// A finish_reason without a resulting terminal event means the conversion
			// path failed to synthesize Completed/Incomplete — treat that as truncation
			// rather than a silent stream close, even though [DONE] may have arrived.
			terminalSignalSeen = pendingFinalEvent != nil || (terminalSignalSeen && !fallbackFinishReasonSeen)
		} else {
			terminalSignalSeen = terminalSignalSeen || finishReason != nil
		}
		if !terminalSignalSeen {
			providerUtils.SendStreamTruncatedError(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
			return
		}

		if isResponsesToChatCompletionsFallback {
			if pendingFinalEvent != nil {
				if usageSeen && pendingFinalEvent.Response != nil {
					pendingFinalEvent.Response.Usage = usage.ToResponsesResponseUsage()
				}
				if sendBackRawRequest {
					providerUtils.ParseAndSetRawRequest(&pendingFinalEvent.ExtraFields, jsonBody)
				}
				pendingFinalEvent.ExtraFields.Latency = time.Since(startTime).Milliseconds()
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, pendingFinalEvent, nil, nil, nil), responseChan, postHookSpanFinalizer)
			}
		} else {
			finalFinishReason := finishReason
			if forwardedTerminalFinishReason {
				finalFinishReason = nil
			}
			response := providerUtils.CreateBifrostChatCompletionChunkResponse(messageID, usage, finalFinishReason, chunkIndex, modelName, created)
			if postResponseConverter != nil {
				response = postResponseConverter(response)
			}
			// Preserve captured tier so priority/flex billing applies to the streamed response
			if serviceTier != nil {
				response.ServiceTier = serviceTier
			}
			// Set raw request if enabled
			if sendBackRawRequest {
				providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
			}
			response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
		}
	}()

	return responseChan, nil
}

// Responses performs a responses request to the OpenAI API.
func (provider *OpenAIProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if provider.shouldFallbackResponsesToChat(schemas.ResponsesRequest, schemas.ChatCompletionRequest) {
		chatResponse, err := provider.ChatCompletion(ctx, key, request.ToChatRequest())
		if err != nil {
			return nil, err
		}
		return chatResponse.ToBifrostResponsesResponse(), nil
	}

	// Check if chat completion is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.OpenAI, provider.customProviderConfig, schemas.ResponsesRequest); err != nil {
		return nil, err
	}

	if provider.disableStore {
		if request.Params == nil {
			request.Params = &schemas.ResponsesParameters{}
		}
		request.Params.Store = schemas.Ptr(false)
	}

	return HandleOpenAIResponsesRequest(
		ctx,
		provider.client,
		provider.buildRequestURL(ctx, "/v1/responses", schemas.ResponsesRequest),
		request,
		BearerAuthHeader(key),
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

// HandleOpenAIResponsesRequest handles a responses request to OpenAI's API.
func HandleOpenAIResponsesRequest(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	request *schemas.BifrostResponsesRequest,
	authHeader map[string]string,
	extraHeaders map[string]string,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	customResponseHandler responseHandler[schemas.BifrostResponsesResponse],
	customErrorConverter ErrorConverter,
	signer providerUtils.BodySigner,
	logger schemas.Logger,
) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	// resp lifecycle: managed by finalizeOpenAIResponse or released on error paths
	respOwned := true
	defer func() {
		if respOwned {
			fasthttp.ReleaseResponse(resp)
		}
	}()
	activeClient := providerUtils.PrepareResponseStreaming(ctx, client, resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	req.SetRequestURI(url)
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")

	for k, v := range authHeader {
		req.Header.Set(k, v)
	}

	// Large payload passthrough: stream body directly without JSON marshaling
	if lpResult, lpErr, handled := handleOpenAILargePayloadPassthrough(ctx, client, url, authHeader, extraHeaders, providerName, logger); handled {
		if lpErr != nil {
			return nil, lpErr
		}
		if len(lpResult.ResponseBody) > 0 {
			response := &schemas.BifrostResponsesResponse{}
			if err := sonic.Unmarshal(lpResult.ResponseBody, response); err != nil {
				return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, err)
			}
			response.ExtraFields = schemas.BifrostResponseExtraFields{Latency: lpResult.Latency}
			return response, nil
		}
		return &schemas.BifrostResponsesResponse{
			Model:       request.Model,
			ExtraFields: schemas.BifrostResponseExtraFields{Latency: lpResult.Latency},
		}, nil
	}

	// Use centralized converter
	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToOpenAIResponsesRequest(ctx, request), nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	if signer != nil {
		sigHeaders, bErr := signer(jsonData)
		if bErr != nil {
			return nil, bErr
		}
		for k, v := range sigHeaders {
			req.Header.Set(k, v)
		}
	}

	req.SetBody(jsonData)

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}
	// Extract provider response headers early so they're available on error paths too
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		logger.Debug("error from %s provider: status %d", providerName, resp.StatusCode())
		if customErrorConverter != nil {
			return nil, providerUtils.EnrichError(ctx, customErrorConverter(resp), jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, ParseOpenAIError(resp), jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	body, lpResult, finalErr := finalizeOpenAIResponse(ctx, resp, latency, providerName, logger)
	respOwned = false // ownership transferred
	if finalErr != nil {
		return nil, providerUtils.EnrichError(ctx, finalErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}
	if lpResult != nil {
		return &schemas.BifrostResponsesResponse{
			Model:       request.Model,
			ExtraFields: schemas.BifrostResponseExtraFields{Latency: lpResult.Latency},
		}, nil
	}

	response := &schemas.BifrostResponsesResponse{}

	var rawRequest, rawResponse interface{}

	if customResponseHandler != nil {
		rawRequest, rawResponse, bifrostErr = customResponseHandler(body, response, jsonData, sendBackRawRequest, sendBackRawResponse)
	} else {
		rawRequest, rawResponse, bifrostErr = providerUtils.HandleProviderResponse(body, response, jsonData, sendBackRawRequest, sendBackRawResponse)
	}

	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, body, sendBackRawRequest, sendBackRawResponse, latency)
	}

	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

// ResponsesStream performs a streaming responses request to the OpenAI API.
func (provider *OpenAIProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if provider.shouldFallbackResponsesToChat(schemas.ResponsesStreamRequest, schemas.ChatCompletionStreamRequest) {
		ctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
		return provider.ChatCompletionStream(ctx, postHookRunner, postHookSpanFinalizer, key, request.ToChatRequest())
	}

	// Check if chat completion stream is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.OpenAI, provider.customProviderConfig, schemas.ResponsesStreamRequest); err != nil {
		return nil, err
	}
	if provider.disableStore {
		if request.Params == nil {
			request.Params = &schemas.ResponsesParameters{}
		}
		request.Params.Store = schemas.Ptr(false)
	}

	// Use shared streaming logic
	return HandleOpenAIResponsesStreaming(
		ctx,
		provider.streamingClient,
		provider.buildRequestURL(ctx, "/v1/responses", schemas.ResponsesStreamRequest),
		request,
		BearerAuthHeader(key),
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

// HandleOpenAIResponsesStreaming handles streaming for OpenAI-compatible APIs.
// This shared function reduces code duplication between providers that use the same SSE format.
func HandleOpenAIResponsesStreaming(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	request *schemas.BifrostResponsesRequest,
	authHeader map[string]string,
	extraHeaders map[string]string,
	streamIdleTimeoutInSeconds int,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	postHookRunner schemas.PostHookRunner,
	customResponseHandler responseHandler[schemas.BifrostResponsesStreamResponse],
	customErrorConverter ErrorConverter,
	postRequestConverter func(*OpenAIResponsesRequest) *OpenAIResponsesRequest,
	postResponseConverter func(*schemas.BifrostResponsesStreamResponse) *schemas.BifrostResponsesStreamResponse,
	signer providerUtils.BodySigner,
	logger schemas.Logger,
	postHookSpanFinalizer func(context.Context),
) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, streamIdleTimeoutInSeconds)
	// Prepare SGL headers (SGL typically doesn't require authorization, but we include it if provided)
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}

	if authHeader != nil {
		// Copy auth header to headers
		maps.Copy(headers, authHeader)
	}

	jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody := ToOpenAIResponsesRequest(ctx, request)
			if reqBody != nil {
				reqBody.Stream = schemas.Ptr(true)
				if postRequestConverter != nil {
					reqBody = postRequestConverter(reqBody)
				}
			}
			return reqBody, nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Create HTTP request for streaming
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(url)
	req.Header.SetContentType("application/json")

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if signer != nil {
		sigHeaders, bErr := signer(jsonBody)
		if bErr != nil {
			defer providerUtils.ReleaseStreamingResponse(ctx, resp)
			return nil, bErr
		}
		for k, v := range sigHeaders {
			req.Header.Set(k, v)
		}
	}

	setStreamingRequestBody(ctx, req, jsonBody, providerName)

	// Use streaming-aware client when large payload optimization is active — ensures
	// MaxResponseBodySize > 0 so ErrBodyTooLarge triggers StreamBody for Content-Length responses.
	activeClient := providerUtils.PrepareResponseStreaming(ctx, client, resp)

	startTime := time.Now()
	// Make the request
	err := providerUtils.DoStreamingRequest(ctx, activeClient, req, resp)
	latency := time.Since(startTime)
	if err != nil {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		if errors.Is(err, context.Canceled) {
			return nil, providerUtils.EnrichError(ctx, &schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   err,
				},
			}, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		if errors.Is(err, fasthttp.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		// The request failed before the first response byte (connection refused, server
		// closed an idle/pooled connection, broken pipe, DNS failure, etc.). Mirror the
		// non-streaming path (makeRequestWithDoFunc) and surface this as a retriable upstream
		// connection error (502, IsBifrostError=false) rather than NewBifrostOperationError
		// (500, IsBifrostError=true). The latter caused the retry loop in executeRequestWithRetries
		// to break early on IsBifrostError, so max_retries never applied to streaming connection
		// failures - see https://github.com/maximhq/bifrost/issues/4496.
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostUpstreamConnectionError(schemas.ErrProviderDoRequest, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Store provider response headers in context before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		if customErrorConverter != nil {
			return nil, providerUtils.EnrichError(ctx, customErrorConverter(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, ParseOpenAIError(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Large payload streaming passthrough — pipe raw upstream SSE to client
	if providerUtils.SetupStreamingPassthrough(ctx, resp) {
		responseChan := make(chan *schemas.BifrostStreamChunk)
		providerUtils.CloseStream(ctx, responseChan)
		return responseChan, nil
	}

	// Create response channel
	responseChan := make(chan *schemas.BifrostStreamChunk, schemas.DefaultStreamBufferSize)

	// Start streaming in a goroutine
	go func() {
		defer providerUtils.EnsureStreamFinalizerCalled(ctx, postHookSpanFinalizer)
		defer func() {
			if ctx.Err() == context.Canceled {
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
			}
			providerUtils.CloseStream(ctx, responseChan)
		}()
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		// Decompress gzip-encoded streams transparently (no-op for non-gzip)
		reader, releaseGzip := providerUtils.DecompressStreamBody(resp)
		defer releaseGzip()

		// Wrap reader with idle timeout to detect stalled streams.
		reader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(reader, resp.BodyStream(), providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close the raw network stream on ctx cancellation,
		// which immediately unblocks any in-progress read (including reads blocked inside a gzip decompression layer).
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), logger)
		defer stopCancellation()

		// Skip scanner for non-SSE responses — avoids bufio.Scanner buffer bloat
		// on non-line-delimited data (e.g. provider returned JSON instead of SSE).
		reader, drained := providerUtils.DrainNonSSEStreamReader(resp, reader)
		if drained {
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			providerUtils.ProcessAndSendError(ctx, postHookRunner, errors.New("provider returned non-SSE response for streaming request"), responseChan, logger, postHookSpanFinalizer)
			return
		}

		sseReader := providerUtils.GetSSEDataReader(ctx, reader)

		lastChunkTime := startTime

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			data, readErr := sseReader.ReadDataLine()
			if readErr != nil {
				if ctx.Err() != nil {
					return
				}
				if readErr != io.EOF {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					logger.Warn("Error reading stream: %v", readErr)
					providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, logger, postHookSpanFinalizer)
					// The read error is already reported; returning (rather than
					// breaking) keeps the post-loop truncation check from reporting
					// the same dead stream a second time.
					return
				}
				break
			}
			jsonData := string(data)

			// Parse into bifrost response
			var response schemas.BifrostResponsesStreamResponse
			if customResponseHandler != nil {
				// Custom handler decodes the raw event itself -> time as "response-parse" stream phase.
				parseStart := time.Now()
				rawRequest, rawResponse, bifrostErr := customResponseHandler([]byte(jsonData), &response, nil, sendBackRawRequest, sendBackRawResponse)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if bifrostErr != nil {
					if sendBackRawRequest {
						bifrostErr.ExtraFields.RawRequest = rawRequest
					}
					if sendBackRawResponse {
						bifrostErr.ExtraFields.RawResponse = rawResponse
					}
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
					return
				}
				if sendBackRawResponse {
					response.ExtraFields.RawResponse = jsonData
				}
			} else {
				// Per-event decode -> "response-parse" (Serialization) stream phase.
				parseStart := time.Now()
				umErr := sonic.UnmarshalString(jsonData, &response)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if umErr != nil {
					logger.Warn("Failed to parse stream response: %v", umErr)
					continue
				}

				if postResponseConverter != nil {
					// Per-event mapping -> "convertor" (Convertor) stream phase.
					convStart := time.Now()
					converted := postResponseConverter(&response)
					schemas.AddStreamConvert(ctx, time.Since(convStart))
					if converted != nil {
						response = *converted
					} else {
						logger.Warn("postResponseConverter returned nil; leaving chunk unmodified")
					}
				}

				if sendBackRawResponse {
					response.ExtraFields.RawResponse = jsonData
				}
			}

			if response.Type == schemas.ResponsesStreamResponseTypeError {
				bifrostErr := responsesStreamError(&response)

				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, []byte(jsonData), sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
				return
			}

			// Some providers (e.g. Fireworks) send response.failed on HTTP 200 streams
			// instead of a pre-stream 4xx. Convert to BifrostError for consistent handling.
			if response.Type == schemas.ResponsesStreamResponseTypeFailed {
				bifrostErr := responsesStreamError(&response)
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, []byte(jsonData), sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
				return
			}

			response.ExtraFields.ChunkIndex = response.SequenceNumber
			if response.Type == schemas.ResponsesStreamResponseTypeCompleted || response.Type == schemas.ResponsesStreamResponseTypeIncomplete {
				// Set raw request if enabled
				if sendBackRawRequest {
					providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
				}
				response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, &response, nil, nil, nil), responseChan, postHookSpanFinalizer)
				return
			}

			response.ExtraFields.Latency = time.Since(lastChunkTime).Milliseconds()
			lastChunkTime = time.Now()

			providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, &response, nil, nil, nil), responseChan, postHookSpanFinalizer)
		}

		// The loop returns as soon as a terminal event (completed / incomplete /
		// failed / error) arrives, so falling out of it means the body ended without
		// one. A plain io.EOF cannot distinguish that from a healthy close, so
		// surface it rather than closing the channel silently — a silent close is
		// indistinguishable to the client from a stream that just stopped emitting.
		if !providerUtils.SSEStreamEndedOnMarker(sseReader) {
			providerUtils.SendStreamTruncatedError(ctx, postHookRunner, responseChan, logger, postHookSpanFinalizer, jsonBody)
		}
	}()

	return responseChan, nil
}

// shouldFallbackResponsesToChat reports whether a Responses call should be
// transparently translated into Chat Completions. This applies when a custom
// provider disables the Responses operation but still allows Chat Completions.
func (provider *OpenAIProvider) shouldFallbackResponsesToChat(responsesOp, chatOp schemas.RequestType) bool {
	cfg := provider.customProviderConfig
	if cfg == nil || cfg.AllowedRequests == nil {
		return false
	}
	return !cfg.IsOperationAllowed(responsesOp) && cfg.IsOperationAllowed(chatOp)
}
