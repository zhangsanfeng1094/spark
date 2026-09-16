package cohere

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"net/http"
	"net/url"

	"github.com/bytedance/sonic"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/valyala/fasthttp"
)

// cohereResponsePool provides a pool for Cohere v2 response objects.
var cohereResponsePool = sync.Pool{
	New: func() interface{} {
		return &CohereChatResponse{}
	},
}

// acquireCohereResponse gets a Cohere v2 response from the pool and resets it.
func acquireCohereResponse() *CohereChatResponse {
	resp := cohereResponsePool.Get().(*CohereChatResponse)
	*resp = CohereChatResponse{} // Reset the struct
	return resp
}

// releaseCohereResponse returns a Cohere v2 response to the pool.
func releaseCohereResponse(resp *CohereChatResponse) {
	if resp != nil {
		cohereResponsePool.Put(resp)
	}
}

// CohereProvider implements the Provider interface for Cohere.
type CohereProvider struct {
	logger               schemas.Logger                // Logger for provider operations
	client               *fasthttp.Client              // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient      *fasthttp.Client              // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig        schemas.NetworkConfig         // Network configuration including extra headers
	sendBackRawRequest   bool                          // Whether to include raw request in BifrostResponse
	sendBackRawResponse  bool                          // Whether to include raw response in BifrostResponse
	customProviderConfig *schemas.CustomProviderConfig // Custom provider config
}

// NewCohereProvider creates a new Cohere provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts and connection limits.
func NewCohereProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*CohereProvider, error) {
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

	// Setting proxy and retry policy
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	// Pre-warm response pools
	for i := 0; i < config.ConcurrencyAndBufferSize.Concurrency; i++ {
		cohereResponsePool.Put(&CohereChatResponse{})
	}

	streamingClient := providerUtils.BuildStreamingClient(client)

	// Set default BaseURL if not provided
	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = "https://api.cohere.ai"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &CohereProvider{
		logger:               logger,
		client:               client,
		streamingClient:      streamingClient,
		networkConfig:        config.NetworkConfig,
		customProviderConfig: config.CustomProviderConfig,
		sendBackRawRequest:   config.SendBackRawRequest,
		sendBackRawResponse:  config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for Cohere.
func (provider *CohereProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.Cohere, provider.customProviderConfig)
}

// buildRequestURL constructs the full request URL using the provider's configuration.
func (provider *CohereProvider) buildRequestURL(ctx *schemas.BifrostContext, defaultPath string, requestType schemas.RequestType) string {
	path, isCompleteURL := providerUtils.GetRequestPath(ctx, defaultPath, provider.customProviderConfig, requestType)
	if isCompleteURL {
		return path
	}
	return provider.networkConfig.BaseURL + path
}

// completeRequest sends a request to Cohere's API and handles the response.
// It constructs the API URL, sets up authentication, and processes the response.
// Returns the response body or an error if the request fails.
func (provider *CohereProvider) completeRequest(ctx *schemas.BifrostContext, jsonData []byte, url string, key string) ([]byte, time.Duration, map[string]string, *schemas.BifrostError) {
	// Create the request with the JSON body
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	respOwned := true
	defer func() {
		if respOwned {
			fasthttp.ReleaseResponse(resp)
		}
	}()

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	req.SetRequestURI(url)
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBodyWithModelNormalization(ctx, req, schemas.Cohere)
	if !usedLargePayloadBody {
		req.SetBody(jsonData)
	}

	// Send the request with optional large response streaming
	activeClient := providerUtils.PrepareResponseStreaming(ctx, provider.client, resp)
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
	defer wait()
	if usedLargePayloadBody {
		providerUtils.DrainLargePayloadRemainder(ctx)
	}
	if bifrostErr != nil {
		return nil, latency, nil, bifrostErr
	}

	// Extract provider response headers before status check so error responses also forward them
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(parseCohereError(resp), latency)
	}

	body, isLargeResp, decodeErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
	if decodeErr != nil {
		return nil, latency, providerResponseHeaders, decodeErr
	}
	if isLargeResp {
		respOwned = false
		return nil, latency, providerResponseHeaders, nil
	}

	return body, latency, providerResponseHeaders, nil
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
func (provider *CohereProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// Build base URL first
	baseURL := provider.buildRequestURL(ctx, "/v1/models", schemas.ListModelsRequest)

	// Parse and add query parameters
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, providerUtils.NewBifrostOperationError("failed to parse request url", err)
	}

	q := u.Query()
	q.Set("page_size", strconv.Itoa(schemas.DefaultPageSize))
	if request.ExtraParams != nil {
		if endpoint, ok := request.ExtraParams["endpoint"].(string); ok && endpoint != "" {
			q.Set("endpoint", endpoint)
		}
		if defaultOnly, ok := request.ExtraParams["default_only"].(bool); ok && defaultOnly {
			q.Set("default_only", "true")
		}
	}
	u.RawQuery = q.Encode()

	// Set the final URL
	req.SetRequestURI(u.String())
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	}

	// Make request
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Store provider response headers in context before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, providerUtils.SetErrorLatency(parseCohereError(resp), latency)
	}

	body, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseDecode, err)
	}

	// Parse Cohere list models response
	var cohereResponse CohereListModelsResponse
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(body, &cohereResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Convert Cohere v2 response to Bifrost response
	response := cohereResponse.ToBifrostListModelsResponse(provider.GetProviderKey(), key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)

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

// ListModels performs a list models request to Cohere's API.
// Requests are made concurrently for improved performance.
func (provider *CohereProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Cohere, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
		return nil, err
	}
	if provider.customProviderConfig != nil && provider.customProviderConfig.IsKeyLess {
		return providerUtils.HandleKeylessListModelsRequest(provider.GetProviderKey(), func() (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
			return provider.listModelsByKey(ctx, schemas.Key{Models: schemas.WhiteList{"*"}}, request)
		})
	}
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to the Cohere API using v2 converter.
// It formats the request, sends it to Cohere, and processes the response.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *CohereProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	// Check if chat completion is allowed
	if err := providerUtils.CheckOperationAllowed(schemas.Cohere, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	// Convert to Cohere v2 request
	jsonBody, err := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToCohereChatCompletionRequest(request)
		})
	if err != nil {
		return nil, err
	}

	responseBody, latency, providerResponseHeaders, err := provider.completeRequest(ctx, jsonBody, provider.buildRequestURL(ctx, "/v2/chat", schemas.ChatCompletionRequest), key.Value.GetValue())
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if err != nil {
		return nil, providerUtils.EnrichError(ctx, err, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Large response mode: return lightweight response with metadata only
	if isLargeResp, _ := ctx.Value(schemas.BifrostContextKeyLargeResponseMode).(bool); isLargeResp {
		return &schemas.BifrostChatResponse{
			Model: request.Model,
			ExtraFields: schemas.BifrostResponseExtraFields{
				Latency:                 latency.Milliseconds(),
				ProviderResponseHeaders: providerResponseHeaders,
			},
		}, nil
	}

	// Create response object from pool
	response := acquireCohereResponse()
	defer releaseCohereResponse(response)

	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponseCtx(ctx, responseBody, response, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	convTracer, convHandle := providerUtils.StartResponseConvertorSpan(ctx)
	bifrostResponse := response.ToBifrostChatResponse(request.Model)
	if convTracer != nil {
		convTracer.EndSpan(convHandle, schemas.SpanStatusOk, "")
	}

	// Set ExtraFields
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		bifrostResponse.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}

	return bifrostResponse, nil
}

// ChatCompletionStream performs a streaming chat completion request to the Cohere API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Returns a channel containing BifrostResponse objects representing the stream or an error if the request fails.
func (provider *CohereProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Check if chat completion stream is allowed
	if err := providerUtils.CheckOperationAllowed(schemas.Cohere, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}

	jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody, err := ToCohereChatCompletionRequest(request)
			if err != nil {
				return nil, err
			}
			reqBody.Stream = schemas.Ptr(true)
			return reqBody, nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := provider.sendBackRawRequest
	sendBackRawResponse := provider.sendBackRawResponse

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(provider.buildRequestURL(ctx, "/v2/chat", schemas.ChatCompletionStreamRequest))
	req.Header.SetContentType("application/json")

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// Set headers
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value.GetValue())
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBodyWithModelNormalization(ctx, req, schemas.Cohere)
	if !usedLargePayloadBody {
		req.SetBody(jsonBody)
	}

	startTime := time.Now()
	// Make the request
	err := providerUtils.DoStreamingRequest(ctx, provider.streamingClient, req, resp)
	latency := time.Since(startTime)
	if usedLargePayloadBody {
		providerUtils.DrainLargePayloadRemainder(ctx)
	}
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
		// Request failed before the first response byte (server closed an idle/pooled connection,
		// broken pipe, connection refused, DNS failure, etc.). Surface as a retriable upstream
		// connection error (502) so executeRequestWithRetries honors max_retries, matching the
		// non-streaming path - see https://github.com/maximhq/bifrost/issues/4496.
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostUpstreamConnectionError(schemas.ErrProviderDoRequest, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Extract provider response headers before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		return nil, providerUtils.EnrichError(ctx, parseCohereError(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, provider.networkConfig.StreamIdleTimeoutInSeconds)

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
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
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
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), provider.logger)
		defer stopCancellation()

		sseReader := providerUtils.GetSSEDataReader(ctx, reader)
		chunkIndex := 0
		lastChunkTime := startTime

		var responseID string

		// Running usage handle so a cancel/timeout can bill for tokens
		// already processed. Cohere only reports usage at message-end, so a true
		// mid-stream cancel captures nothing (the API emits no incremental usage);
		// this still bills correctly when usage has arrived before teardown.
		streamUsage := &schemas.BifrostLLMUsage{}
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, streamUsage)

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			data, readErr := sseReader.ReadDataLine()
			if readErr != nil {
				// Recheck context cancellation
				if ctx.Err() != nil {
					return
				}
				if readErr != io.EOF {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					provider.logger.Warn("Error reading stream: %v", readErr)
					providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}
				break
			}

			eventData := string(data)

			// Parse the unified streaming event. Per-event decode -> "response-parse" (Serialization) stream phase.
			var event CohereStreamEvent
			parseStart := time.Now()
			umErr := sonic.Unmarshal(data, &event)
			schemas.AddStreamParse(ctx, time.Since(parseStart))
			if umErr != nil {
				provider.logger.Warn("Failed to parse stream event: %v", umErr)
				continue
			}

			// Extract response ID from message-start events
			if event.Type == StreamEventMessageStart && event.ID != nil {
				responseID = *event.ID
			}

			// Per-event mapping -> "convertor" (Convertor) stream phase.
			convStart := time.Now()
			response, bifrostErr, isLastChunk := event.ToBifrostChatCompletionStream()
			schemas.AddStreamConvert(ctx, time.Since(convStart))
			if bifrostErr != nil {
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, bifrostErr, responseChan, provider.logger, postHookSpanFinalizer)
				// Already reported; returning (rather than breaking) keeps the
				// post-loop truncation check from reporting the same stream twice.
				return
			}
			if response != nil {
				response.ID = responseID
				// keep the running usage handle current.
				if response.Usage != nil {
					*streamUsage = *response.Usage
				}
				response.ExtraFields = schemas.BifrostResponseExtraFields{
					ChunkIndex: chunkIndex,
					Latency:    time.Since(lastChunkTime).Milliseconds(),
				}

				lastChunkTime = time.Now()
				chunkIndex++

				if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
					response.ExtraFields.RawResponse = eventData
				}

				if isLastChunk {
					// Set raw request if enabled
					if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
						providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
					}
					response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
					return
				}
				providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
			}
		}

		// Cohere terminates the stream with a message-end event, which the loop
		// returns on. Falling out of the loop means the body ended before it — a
		// plain io.EOF, indistinguishable from a healthy close — so surface it
		// rather than closing the channel silently.
		providerUtils.SendStreamTruncatedError(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
	}()

	return responseChan, nil
}

// Responses performs a responses request to the Cohere API using v2 converter.
func (provider *CohereProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	// Check if chat completion is allowed
	if err := providerUtils.CheckOperationAllowed(schemas.Cohere, provider.customProviderConfig, schemas.ResponsesRequest); err != nil {
		return nil, err
	}

	jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToCohereResponsesRequest(request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Convert to Cohere v2 request
	responseBody, latency, providerResponseHeaders, err := provider.completeRequest(ctx, jsonBody, provider.buildRequestURL(ctx, "/v2/chat", schemas.ResponsesRequest), key.Value.GetValue())
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if err != nil {
		return nil, providerUtils.EnrichError(ctx, err, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Large response mode: return lightweight response with metadata only
	if isLargeResp, _ := ctx.Value(schemas.BifrostContextKeyLargeResponseMode).(bool); isLargeResp {
		return &schemas.BifrostResponsesResponse{
			Model: request.Model,
			ExtraFields: schemas.BifrostResponseExtraFields{
				Latency:                 latency.Milliseconds(),
				ProviderResponseHeaders: providerResponseHeaders,
			},
		}, nil
	}

	// Create response object from pool
	response := acquireCohereResponse()
	defer releaseCohereResponse(response)

	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, response, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	bifrostResponse := response.ToBifrostResponsesResponse()

	bifrostResponse.Model = request.Model

	// Set ExtraFields
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		bifrostResponse.ExtraFields.RawRequest = rawRequest
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}

	return bifrostResponse, nil
}

// ResponsesStream performs a streaming responses request to the Cohere API.
func (provider *CohereProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Check if responses stream is allowed
	if err := providerUtils.CheckOperationAllowed(schemas.Cohere, provider.customProviderConfig, schemas.ResponsesStreamRequest); err != nil {
		return nil, err
	}

	// Convert to Cohere v2 request and add streaming
	jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody, err := ToCohereResponsesRequest(request)
			if err != nil {
				return nil, err
			}
			if reqBody != nil {
				reqBody.Stream = schemas.Ptr(true)
			}
			return reqBody, nil
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := provider.sendBackRawRequest
	sendBackRawResponse := provider.sendBackRawResponse

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(provider.buildRequestURL(ctx, "/v2/chat", schemas.ResponsesStreamRequest))
	req.Header.SetContentType("application/json")
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// Set headers
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value.GetValue())
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBodyWithModelNormalization(ctx, req, schemas.Cohere)
	if !usedLargePayloadBody {
		req.SetBody(jsonBody)
	}

	startTime := time.Now()
	// Make the request
	err := providerUtils.DoStreamingRequest(ctx, provider.streamingClient, req, resp)
	latency := time.Since(startTime)
	if usedLargePayloadBody {
		providerUtils.DrainLargePayloadRemainder(ctx)
	}
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
		// Request failed before the first response byte (server closed an idle/pooled connection,
		// broken pipe, connection refused, DNS failure, etc.). Surface as a retriable upstream
		// connection error (502) so executeRequestWithRetries honors max_retries, matching the
		// non-streaming path - see https://github.com/maximhq/bifrost/issues/4496.
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostUpstreamConnectionError(schemas.ErrProviderDoRequest, err), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Extract provider response headers before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		return nil, providerUtils.EnrichError(ctx, parseCohereError(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, provider.networkConfig.StreamIdleTimeoutInSeconds)

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
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
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
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), provider.logger)
		defer stopCancellation()

		sseReader := providerUtils.GetSSEDataReader(ctx, reader)

		chunkIndex := 0

		lastChunkTime := startTime

		// Create stream state for stateful conversions (outside loop to persist across events)
		streamState := acquireCohereResponsesStreamState()
		streamState.Model = &request.Model
		defer releaseCohereResponsesStreamState(streamState)

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			data, readErr := sseReader.ReadDataLine()
			if readErr != nil {
				// Recheck context cancellation
				if ctx.Err() != nil {
					return
				}
				if readErr != io.EOF {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					provider.logger.Warn("Error reading stream: %v", readErr)
					providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}
				break
			}

			eventData := string(data)

			// Parse the unified streaming event. Per-event decode -> "response-parse" (Serialization) stream phase.
			var event CohereStreamEvent
			parseStart := time.Now()
			umErr := sonic.Unmarshal(data, &event)
			schemas.AddStreamParse(ctx, time.Since(parseStart))
			if umErr != nil {
				provider.logger.Warn("Failed to parse stream event: %v", umErr)
				continue
			}

			// Note: response.created and response.in_progress are now emitted by ToBifrostResponsesStream
			// from the message_start event, so we don't need to call them manually here

			// Per-event mapping -> "convertor" (Convertor) stream phase.
			convStart := time.Now()
			responses, bifrostErr, isLastChunk := event.ToBifrostResponsesStream(chunkIndex, streamState)
			schemas.AddStreamConvert(ctx, time.Since(convStart))
			if bifrostErr != nil {
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, bifrostErr, responseChan, provider.logger, postHookSpanFinalizer)
				// Already reported; returning (rather than breaking) keeps the
				// post-loop truncation check from reporting the same stream twice.
				return
			}
			// Handle each response in the slice
			for i, response := range responses {
				if response != nil {
					response.ExtraFields = schemas.BifrostResponseExtraFields{
						ChunkIndex: chunkIndex,
						Latency:    time.Since(lastChunkTime).Milliseconds(),
					}
					lastChunkTime = time.Now()
					chunkIndex++

					if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
						response.ExtraFields.RawResponse = eventData
					}

					if isLastChunk && i == len(responses)-1 {
						if response.Response == nil {
							response.Response = &schemas.BifrostResponsesResponse{}
						}
						// Set raw request if enabled
						if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
							providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
						}
						response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
						ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
						return
					}
					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
				}
			}
		}

		// See the chat stream above: falling out of the loop means the body ended
		// before the terminal event, which a plain io.EOF cannot distinguish from a
		// healthy close.
		providerUtils.SendStreamTruncatedError(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonBody)
	}()

	return responseChan, nil
}

