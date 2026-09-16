package gemini

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

const (
	BifrostContextKeyResponseFormat schemas.BifrostContextKey = "bifrost_context_key_response_format"
)

type GeminiProvider struct {
	logger               schemas.Logger                // Logger for provider operations
	client               *fasthttp.Client              // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient      *fasthttp.Client              // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig        schemas.NetworkConfig         // Network configuration including extra headers
	sendBackRawRequest   bool                          // Whether to include raw request in BifrostResponse
	sendBackRawResponse  bool                          // Whether to include raw response in BifrostResponse
	customProviderConfig *schemas.CustomProviderConfig // Custom provider config
}

func setGeminiRequestBody(req *fasthttp.Request, bodyReader io.Reader, bodySize int, jsonData []byte) {
	// Large payload mode streams request bytes directly from the ingress reader.
	// Normal mode sends marshaled JSON as before.
	// Example failure prevented: materializing giant request payloads again here
	// after transport already selected streaming mode.
	if bodyReader != nil {
		req.SetBodyStream(bodyReader, bodySize)
		return
	}
	req.SetBody(jsonData)
}

func normalizeRawGenerateContentBody(ctx *schemas.BifrostContext, body []byte) []byte {
	if rawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && rawBody {
		body = NormalizeRawGenerateContentRequestForCompatibility(body)
	}
	if updated, err := providerUtils.DeleteJSONField(body, "fallbacks"); err == nil {
		body = updated
	}
	return body
}

// NewGeminiProvider creates a new Gemini provider instance.
// It initializes the HTTP client with the provided configuration.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewGeminiProvider(config *schemas.ProviderConfig, logger schemas.Logger) *GeminiProvider {
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
		config.NetworkConfig.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &GeminiProvider{
		logger:               logger,
		client:               client,
		streamingClient:      streamingClient,
		networkConfig:        config.NetworkConfig,
		customProviderConfig: config.CustomProviderConfig,
		sendBackRawRequest:   config.SendBackRawRequest,
		sendBackRawResponse:  config.SendBackRawResponse,
	}
}

// GetProviderKey returns the provider identifier for Gemini.
func (provider *GeminiProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.Gemini, provider.customProviderConfig)
}

// completeRequest handles the common HTTP request pattern for Gemini API calls.
// When large response streaming is activated (BifrostContextKeyLargeResponseMode set in ctx),
// returns (nil, nil, latency, nil) — callers must check the context flag.
func (provider *GeminiProvider) completeRequest(ctx *schemas.BifrostContext, model string, key schemas.Key, jsonBody []byte, endpoint string) (*GenerateContentResponse, interface{}, time.Duration, map[string]string, *schemas.BifrostError) {
	// Create request
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

	// Use Gemini's generateContent endpoint
	req.SetRequestURI(provider.networkConfig.BaseURL + providerUtils.GetPathFromContext(ctx, "/models/"+model+endpoint))
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	if key.Value.GetValue() != "" {
		req.Header.Set("x-goog-api-key", key.Value.GetValue())
	}

	// Large payload mode: stream original request bytes directly from ingress.
	// Normal mode: use converted JSON body.
	usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBody(ctx, req)
	if !usedLargePayloadBody {
		jsonBody = normalizeRawGenerateContentBody(ctx, jsonBody)
		req.SetBody(jsonBody)
	}

	// Send the request with optional large response streaming
	activeClient := providerUtils.PrepareResponseStreaming(ctx, provider.client, resp)
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, nil, latency, nil, bifrostErr
	}
	if usedLargePayloadBody {
		providerUtils.DrainLargePayloadRemainder(ctx)
	}

	// Extract provider response headers before status check so error responses also forward them
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		return nil, nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(parseGeminiError(resp), latency)
	}

	body, isLargeResp, decodeErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
	if decodeErr != nil {
		return nil, nil, latency, providerResponseHeaders, decodeErr
	}
	if isLargeResp {
		respOwned = false
		return nil, nil, latency, providerResponseHeaders, nil
	}

	// Parse Gemini's response
	var geminiResponse GenerateContentResponse
	if err := sonic.Unmarshal(body, &geminiResponse); err != nil {
		return nil, nil, latency, providerResponseHeaders, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, err)
	}

	var rawResponse interface{}
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		if err := sonic.Unmarshal(body, &rawResponse); err != nil {
			return nil, nil, latency, providerResponseHeaders, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, err)
		}
	}

	return &geminiResponse, rawResponse, latency, providerResponseHeaders, nil
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
func (provider *GeminiProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	providerName := provider.GetProviderKey()
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// Build URL using centralized URL construction
	req.SetRequestURI(provider.networkConfig.BaseURL + providerUtils.GetPathFromContext(ctx, fmt.Sprintf("/models?pageSize=%d", schemas.DefaultPageSize)))
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")
	if key.Value.GetValue() != "" {
		req.Header.Set("x-goog-api-key", key.Value.GetValue())
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
		return nil, providerUtils.SetErrorLatency(parseGeminiError(resp), latency)
	}

	// Parse Gemini's response
	var geminiResponse GeminiListModelsResponse
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(resp.Body(), &geminiResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	if len(geminiResponse.Models) == 0 {
		var singleModel GeminiModel
		if err := sonic.Unmarshal(resp.Body(), &singleModel); err == nil && singleModel.Name != "" {
			geminiResponse.Models = []GeminiModel{singleModel}
		}
	}

	response := geminiResponse.ToBifrostListModelsResponse(providerName, key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)

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

// ListModels performs a list models request to Gemini's API.
// Requests are made concurrently for improved performance.
func (provider *GeminiProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Gemini, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
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

// ChatCompletion performs a chat completion request to the Gemini API.
func (provider *GeminiProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	// Check if chat completion is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.Gemini, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	jsonData, err := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToGeminiChatCompletionRequest(ctx, request)
		})
	if err != nil {
		return nil, err
	}

	geminiResponse, rawResponse, latency, providerResponseHeaders, bifrostErr := provider.completeRequest(ctx, request.Model, key, jsonData, ":generateContent")
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
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

	convTracer, convHandle := providerUtils.StartResponseConvertorSpan(ctx)
	bifrostResponse := geminiResponse.ToBifrostChatResponse()
	if convTracer != nil {
		convTracer.EndSpan(convHandle, schemas.SpanStatusOk, "")
	}

	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		providerUtils.ParseAndSetRawRequest(&bifrostResponse.ExtraFields, jsonData)
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}

	return bifrostResponse, nil
}

// ChatCompletionStream performs a streaming chat completion request to the Gemini API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Returns a channel containing BifrostStreamChunk objects representing the stream or an error if the request fails.
func (provider *GeminiProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Check if chat completion stream is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.Gemini, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}

	jsonData, err := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody, err := ToGeminiChatCompletionRequest(ctx, request)
			if err != nil {
				return nil, err
			}
			if reqBody == nil {
				return nil, fmt.Errorf("chat completion request is not provided or could not be converted to gemini format")
			}
			return reqBody, nil
		})
	if err != nil {
		return nil, err
	}

	// Prepare Gemini headers
	headers := map[string]string{
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	if key.Value.GetValue() != "" {
		headers["x-goog-api-key"] = key.Value.GetValue()
	}

	// Use shared Gemini streaming logic
	return HandleGeminiChatCompletionStream(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/models/"+request.Model+":streamGenerateContent?alt=sse"),
		jsonData,
		headers,
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		request.Model,
		postHookRunner,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// HandleGeminiChatCompletionStream handles streaming for Gemini-compatible APIs.
func HandleGeminiChatCompletionStream(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	jsonBody []byte,
	headers map[string]string,
	extraHeaders map[string]string,
	streamIdleTimeoutInSeconds int,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	model string,
	postHookRunner schemas.PostHookRunner,
	postResponseConverter func(*schemas.BifrostChatResponse) *schemas.BifrostChatResponse,
	logger schemas.Logger,
	postHookSpanFinalizer func(context.Context),
) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, streamIdleTimeoutInSeconds)
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(url)
	req.Header.SetContentType("application/json")
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Large payload mode: stream original request bytes directly from ingress.
	if !providerUtils.ApplyLargePayloadRequestBody(ctx, req) {
		jsonBody = normalizeRawGenerateContentBody(ctx, jsonBody)
		req.SetBody(jsonBody)
	}

	startTime := time.Now()
	// Make the request — caller is responsible for passing a streaming-configured client.
	doErr := providerUtils.DoStreamingRequest(ctx, client, req, resp)
	latency := time.Since(startTime)
	if doErr != nil {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		if errors.Is(doErr, context.Canceled) {
			return nil, providerUtils.EnrichError(ctx, &schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   doErr,
				},
			}, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		if errors.Is(doErr, fasthttp.ErrTimeout) || errors.Is(doErr, context.DeadlineExceeded) {
			return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, doErr), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError(schemas.ErrProviderDoRequest, doErr), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Extract provider response headers before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors — use parseGeminiError to preserve upstream error details
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		respBody := append([]byte(nil), resp.Body()...)
		return nil, providerUtils.EnrichError(ctx, parseGeminiError(resp), jsonBody, respBody, sendBackRawRequest, sendBackRawResponse, latency)
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

		if resp.BodyStream() == nil {
			bifrostErr := providerUtils.NewBifrostOperationError(
				"Provider returned an empty response",
				fmt.Errorf("provider returned an empty response"),
			)
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
			return
		}

		// Decompress gzip-encoded streams transparently (no-op for non-gzip)
		decompressedReader, releaseGzip := providerUtils.DecompressStreamBody(resp)
		defer releaseGzip()

		// Wrap reader with idle timeout to detect stalled streams.
		decompressedReader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(decompressedReader, resp.BodyStream(), providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close the raw network stream on ctx cancellation,
		// which immediately unblocks any in-progress read (including reads blocked inside a gzip decompression layer).
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), logger)
		defer stopCancellation()

		skipInlineData := shouldSkipInlineDataForStreamingContext(ctx)
		var lineReader *bufio.Reader
		var sseReader providerUtils.SSEDataReader
		if skipInlineData {
			lineReader = bufio.NewReaderSize(decompressedReader, 64*1024)
		} else {
			sseReader = providerUtils.GetSSEDataReader(ctx, decompressedReader)
		}

		chunkIndex := 0
		lastChunkTime := startTime

		var responseID string
		var modelName string

		streamState := NewGeminiStreamState()

		// Running usage handle so a mid-stream cancel/timeout can bill for
		// tokens already processed. Gemini's usageMetadata is cumulative, so the
		// latest non-nil copy below is the running total.
		streamUsage := &schemas.BifrostLLMUsage{}
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, streamUsage)

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			var (
				eventData []byte
				readErr   error
			)
			if skipInlineData {
				eventData, readErr = readNextSSEDataLine(lineReader, true)
			} else {
				eventData, readErr = sseReader.ReadDataLine()
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				// If context was cancelled/timed out, let defer handle it
				if ctx.Err() != nil {
					return
				}
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				logger.Warn("Error reading stream: %v", readErr)
				providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, logger, postHookSpanFinalizer)
				return
			}
			// Process chunk using shared function. Per-event decode -> "response-parse" (Serialization) stream phase.
			parseStart := time.Now()
			geminiResponse, err := processGeminiStreamChunk(eventData)
			schemas.AddStreamParse(ctx, time.Since(parseStart))
			if err != nil {
				if strings.Contains(err.Error(), "gemini api error") {
					// Handle API error
					bifrostErr := toGeminiStreamBifrostError(err)
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
					return
				}
				logger.Warn("Failed to process chunk: %v", err)
				continue
			}

			// Track response ID and model
			if geminiResponse.ResponseID != "" && responseID == "" {
				responseID = geminiResponse.ResponseID
			}
			if geminiResponse.ModelVersion != "" && modelName == "" {
				modelName = geminiResponse.ModelVersion
			}

			// Convert to Bifrost stream response. Per-event mapping -> "convertor" (Convertor) stream phase.
			convStart := time.Now()
			response, bifrostErr, isLastChunk := geminiResponse.ToBifrostChatCompletionStream(streamState)
			schemas.AddStreamConvert(ctx, time.Since(convStart))
			if bifrostErr != nil {
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency), responseChan, logger, postHookSpanFinalizer)
				return
			}

			if response != nil {
				response.ID = responseID
				if modelName != "" {
					response.Model = modelName
				}
				// keep the running usage handle current (cumulative).
				if response.Usage != nil {
					*streamUsage = *response.Usage
				}
				response.ExtraFields = schemas.BifrostResponseExtraFields{
					ChunkIndex: chunkIndex,
					Latency:    time.Since(lastChunkTime).Milliseconds(),
				}

				if postResponseConverter != nil {
					response = postResponseConverter(response)
					if response == nil {
						logger.Warn("postResponseConverter returned nil; skipping chunk")
						continue
					}
				}

				if sendBackRawResponse {
					response.ExtraFields.RawResponse = string(eventData)
				}

				lastChunkTime = time.Now()
				chunkIndex++

				if isLastChunk {
					if sendBackRawRequest {
						providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
					}
					response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
					break
				}

				// Process response through post-hooks and send to channel
				providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
			}
		}
	}()

	return responseChan, nil
}

// Responses performs a chat completion request to Gemini's API.
// It formats the request, sends it to Gemini, and processes the response.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *GeminiProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Gemini, provider.customProviderConfig, schemas.ResponsesRequest); err != nil {
		return nil, err
	}

	// Check for large payload streaming mode (enterprise-only feature)
	// In large payload mode, the request body streams directly from the client — skip body conversion
	var bodyReader io.Reader
	bodySize := -1
	var jsonData []byte

	if isLargePayload, ok := ctx.Value(schemas.BifrostContextKeyLargePayloadMode).(bool); ok && isLargePayload {
		if reader, readerOk := ctx.Value(schemas.BifrostContextKeyLargePayloadReader).(io.Reader); readerOk && reader != nil {
			bodyReader = reader
			if contentLength, lenOk := ctx.Value(schemas.BifrostContextKeyLargePayloadContentLength).(int); lenOk {
				bodySize = contentLength
			}
		}
	}

	// For normal path (no large payload body reader), convert request to bytes
	if bodyReader == nil {
		var err *schemas.BifrostError
		jsonData, err = providerUtils.CheckContextAndGetRequestBody(
			ctx,
			request,
			func() (providerUtils.RequestBodyWithExtraParams, error) {
				reqBody, err := ToGeminiResponsesRequest(ctx, request)
				if err != nil {
					return nil, err
				}
				if reqBody == nil {
					return nil, fmt.Errorf("responses input is not provided or could not be converted to gemini format")
				}
				return reqBody, nil
			})
		if err != nil {
			return nil, err
		}
	}

	// Check if enterprise large response detection is enabled
	if responseThreshold, ok := ctx.Value(schemas.BifrostContextKeyLargeResponseThreshold).(int64); ok && responseThreshold > 0 {
		return provider.responsesWithLargeResponseDetection(ctx, key, request, jsonData, responseThreshold, bodyReader, bodySize)
	}

	// Use struct directly for JSON marshaling
	geminiResponse, rawResponse, latency, providerResponseHeaders, bifrostErr := provider.completeRequest(ctx, request.Model, key, jsonData, ":generateContent")
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
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

	// Create final response
	bifrostResponse := geminiResponse.ToResponsesBifrostResponsesResponse()

	// Set ExtraFields
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		providerUtils.ParseAndSetRawRequest(&bifrostResponse.ExtraFields, jsonData)
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}

	return bifrostResponse, nil
}

// responsesWithLargeResponseDetection makes the upstream request with response body streaming
// enabled. If the response Content-Length exceeds the threshold, it sets context flags for
// the router to stream the body directly to the client without full materialization.
// If the response is small, it falls through to the normal parse-and-convert path.
func (provider *GeminiProvider) responsesWithLargeResponseDetection(
	ctx *schemas.BifrostContext,
	key schemas.Key,
	request *schemas.BifrostResponsesRequest,
	jsonData []byte,
	responseThreshold int64,
	bodyReader io.Reader, // Optional: for large payload request streaming (pass nil for normal path)
	bodySize int, // Required if bodyReader is non-nil
) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	// Create request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	// Note: resp is NOT deferred — lifecycle managed manually for large responses

	// Enable response body streaming so fasthttp doesn't buffer the entire body
	resp.StreamBody = true

	// Set up request (same as completeRequest)
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)
	req.SetRequestURI(provider.networkConfig.BaseURL + providerUtils.GetPathFromContext(ctx, "/models/"+request.Model+":generateContent"))
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	if key.Value.GetValue() != "" {
		req.Header.Set("x-goog-api-key", key.Value.GetValue())
	}

	// Large payload mode streams request bytes directly to upstream; normal mode sends marshaled bytes.
	if bodyReader == nil {
		jsonData = normalizeRawGenerateContentBody(ctx, jsonData)
	}
	setGeminiRequestBody(req, bodyReader, bodySize, jsonData)

	// Make request
	streamingClient := providerUtils.BuildLargeResponseClient(provider.client, responseThreshold)
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, streamingClient, req, resp)
	if bifrostErr != nil {
		wait()
		fasthttp.ReleaseResponse(resp)
		return nil, bifrostErr
	}

	// Handle error response — materialize stream body for error parsing
	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		bifrostErr := parseGeminiError(resp)
		wait()
		fasthttp.ReleaseResponse(resp)
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Delegate large response detection + normal buffered path to shared utility
	responseBody, isLarge, respErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
	if respErr != nil {
		wait()
		fasthttp.ReleaseResponse(resp)
		return nil, providerUtils.EnrichError(ctx, respErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if isLarge {
		// Build lightweight response with usage from preview for plugin pipeline
		preview, _ := ctx.Value(schemas.BifrostContextKeyLargePayloadResponsePreview).(string)
		usage := extractUsageFromResponsePrefetch([]byte(preview))
		bifrostResponse := &schemas.BifrostResponsesResponse{
			ID:        schemas.Ptr("resp_" + schemas.GetRandomString(50)),
			CreatedAt: int(time.Now().Unix()),
			Model:     request.Model,
			Usage:     usage,
		}
		bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
		// resp owned by reader in context — don't release
		wait()
		return bifrostResponse, nil
	}
	wait()
	fasthttp.ReleaseResponse(resp)

	// Normal parse-and-convert path
	var geminiResponse GenerateContentResponse
	if unmarshalErr := sonic.Unmarshal(responseBody, &geminiResponse); unmarshalErr != nil {
		return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, unmarshalErr)
	}
	bifrostResponse := geminiResponse.ToResponsesBifrostResponsesResponse()
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		providerUtils.ParseAndSetRawRequest(&bifrostResponse.ExtraFields, jsonData)
	}
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		var rawResponse interface{}
		sonic.Unmarshal(responseBody, &rawResponse) //nolint:errcheck
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}
	return bifrostResponse, nil
}

// extractUsageFromResponsePrefetch extracts usage metadata from the response prefetch buffer.
// Uses sonic.Get for O(1) extraction without parsing the full response.
func extractUsageFromResponsePrefetch(data []byte) *schemas.ResponsesResponseUsage {
	node, err := sonic.Get(data, "usageMetadata")
	if err != nil {
		return nil
	}
	raw, _ := node.Raw()
	if raw == "" {
		return nil
	}

	var usageMeta GenerateContentResponseUsageMetadata
	if err := sonic.UnmarshalString(raw, &usageMeta); err != nil {
		return nil
	}

	return ConvertGeminiUsageMetadataToResponsesUsage(&usageMeta)
}

// ResponsesStream performs a streaming responses request to the Gemini API.
func (provider *GeminiProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Check if responses stream is allowed for this provider
	if err := providerUtils.CheckOperationAllowed(schemas.Gemini, provider.customProviderConfig, schemas.ResponsesStreamRequest); err != nil {
		return nil, err
	}

	jsonData, err := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody, err := ToGeminiResponsesRequest(ctx, request)
			if err != nil {
				return nil, err
			}
			if reqBody == nil {
				return nil, fmt.Errorf("responses input is not provided or could not be converted to gemini format")
			}
			return reqBody, nil
		})
	if err != nil {
		return nil, err
	}

	// Prepare Gemini headers
	headers := map[string]string{
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	if key.Value.GetValue() != "" {
		headers["x-goog-api-key"] = key.Value.GetValue()
	}

	return HandleGeminiResponsesStream(
		ctx,
		provider.streamingClient,
		provider.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/models/"+request.Model+":streamGenerateContent?alt=sse"),
		jsonData,
		headers,
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		request.Model,
		postHookRunner,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}

// HandleGeminiResponsesStream handles streaming for Gemini-compatible APIs.
func HandleGeminiResponsesStream(
	ctx *schemas.BifrostContext,
	client *fasthttp.Client,
	url string,
	jsonBody []byte,
	headers map[string]string,
	extraHeaders map[string]string,
	streamIdleTimeoutInSeconds int,
	sendBackRawRequest bool,
	sendBackRawResponse bool,
	providerName schemas.ModelProvider,
	model string,
	postHookRunner schemas.PostHookRunner,
	postResponseConverter func(*schemas.BifrostResponsesStreamResponse) *schemas.BifrostResponsesStreamResponse,
	logger schemas.Logger,
	postHookSpanFinalizer func(context.Context),
) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, streamIdleTimeoutInSeconds)
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	resp.StreamBody = true
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(http.MethodPost)
	req.SetRequestURI(url)
	req.Header.SetContentType("application/json")
	providerUtils.SetExtraHeaders(ctx, req, extraHeaders, nil)

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Large payload mode: stream original request body to upstream.
	if !providerUtils.ApplyLargePayloadRequestBody(ctx, req) {
		jsonBody = normalizeRawGenerateContentBody(ctx, jsonBody)
		req.SetBody(jsonBody)
	}

	startTime := time.Now()
	// Make the request — caller is responsible for passing a streaming-configured client.
	doErr := providerUtils.DoStreamingRequest(ctx, client, req, resp)
	latency := time.Since(startTime)
	if doErr != nil {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		if errors.Is(doErr, context.Canceled) {
			return nil, providerUtils.EnrichError(ctx, &schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   doErr,
				},
			}, jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		if errors.Is(doErr, fasthttp.ErrTimeout) || errors.Is(doErr, context.DeadlineExceeded) {
			return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, doErr), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
		}
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError(schemas.ErrProviderDoRequest, doErr), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
	}

	// Extract provider response headers before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Check for HTTP errors — use parseGeminiError to preserve upstream error details
	if resp.StatusCode() != fasthttp.StatusOK {
		defer providerUtils.ReleaseStreamingResponse(ctx, resp)
		return nil, providerUtils.EnrichError(ctx, parseGeminiError(resp), jsonBody, nil, sendBackRawRequest, sendBackRawResponse, latency)
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

		if resp.BodyStream() == nil {
			bifrostErr := providerUtils.NewBifrostOperationError(
				"Provider returned an empty response",
				fmt.Errorf("provider returned an empty response"),
			)
			ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
			providerUtils.ProcessAndSendBifrostError(
				ctx,
				postHookRunner,
				providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse),
				responseChan,
				logger,
				postHookSpanFinalizer,
			)
			return
		}

		// Decompress gzip-encoded streams transparently (no-op for non-gzip)
		decompressedReader, releaseGzip := providerUtils.DecompressStreamBody(resp)
		defer releaseGzip()

		// Wrap reader with idle timeout to detect stalled streams.
		decompressedReader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(decompressedReader, resp.BodyStream(), providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close the raw network stream on ctx cancellation,
		// which immediately unblocks any in-progress read (including reads blocked inside a gzip decompression layer).
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.BodyStream(), logger)
		defer stopCancellation()

		skipInlineData := shouldSkipInlineDataForStreamingContext(ctx)
		var lineReader *bufio.Reader
		var sseReader providerUtils.SSEDataReader
		if skipInlineData {
			lineReader = bufio.NewReaderSize(decompressedReader, 64*1024)
		} else {
			sseReader = providerUtils.GetSSEDataReader(ctx, decompressedReader)
		}

		chunkIndex := 0
		sequenceNumber := 0 // Track sequence across all events
		lastChunkTime := startTime

		// Initialize stream state for responses lifecycle management
		streamState := acquireGeminiResponsesStreamState()
		defer releaseGeminiResponsesStreamState(streamState)

		var lastUsageMetadata *GenerateContentResponseUsageMetadata

		// Running usage handle so a mid-stream cancel/timeout can bill for
		// tokens already processed. Gemini's usageMetadata is cumulative.
		streamUsage := &schemas.BifrostLLMUsage{}
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, streamUsage)

		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			var (
				eventData []byte
				readErr   error
			)
			if skipInlineData {
				eventData, readErr = readNextSSEDataLine(lineReader, true)
			} else {
				eventData, readErr = sseReader.ReadDataLine()
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				if ctx.Err() != nil {
					return
				}
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				logger.Warn("Error reading stream: %v", readErr)
				providerUtils.ProcessAndSendError(ctx, postHookRunner, readErr, responseChan, logger, postHookSpanFinalizer)
				return
			}

			// Process chunk using shared function. Per-event decode -> "response-parse" (Serialization) stream phase.
			parseStart := time.Now()
			geminiResponse, err := processGeminiStreamChunk(eventData)
			schemas.AddStreamParse(ctx, time.Since(parseStart))
			if err != nil {
				if strings.Contains(err.Error(), "gemini api error") {
					// Handle API error
					bifrostErr := toGeminiStreamBifrostError(err)
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse), responseChan, logger, postHookSpanFinalizer)
					return
				}
				logger.Warn("Failed to process chunk: %v", err)
				continue
			}

			// Track usage metadata from the latest chunk
			if geminiResponse.UsageMetadata != nil {
				lastUsageMetadata = geminiResponse.UsageMetadata
				// keep the running usage handle current (cumulative).
				if converted := ConvertGeminiUsageMetadataToChatUsage(lastUsageMetadata); converted != nil {
					*streamUsage = *converted
				}
			}

			// Convert to Bifrost responses stream response. Per-event mapping -> "convertor" (Convertor) stream phase.
			convStart := time.Now()
			responses, bifrostErr := geminiResponse.ToBifrostResponsesStream(sequenceNumber, streamState)
			schemas.AddStreamConvert(ctx, time.Since(convStart))
			if bifrostErr != nil {
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, sendBackRawRequest, sendBackRawResponse), responseChan, logger, postHookSpanFinalizer)
				return
			}

			for i, response := range responses {
				if response != nil {
					response.ExtraFields = schemas.BifrostResponseExtraFields{
						ChunkIndex: chunkIndex,
						Latency:    time.Since(lastChunkTime).Milliseconds(),
					}

					if postResponseConverter != nil {
						response = postResponseConverter(response)
						if response == nil {
							logger.Warn("postResponseConverter returned nil; skipping chunk")
							continue
						}
					}

					// Only add raw response to the LAST response in the array
					if sendBackRawResponse && i == len(responses)-1 {
						response.ExtraFields.RawResponse = string(eventData)
					}

					chunkIndex++
					sequenceNumber++ // Increment sequence number for each response

					// Check if this is the last chunk
					isLastChunk := false
					if response.Type == schemas.ResponsesStreamResponseTypeCompleted {
						isLastChunk = true
					}

					if isLastChunk {
						if sendBackRawRequest {
							providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonBody)
						}
						response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
						ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
						return
					}

					// For multiple responses in one event, only update timing on the last one
					if i == len(responses)-1 {
						lastChunkTime = time.Now()
					}

					// Process response through post-hooks and send to channel
					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
				}
			}
		}
		// Finalize the stream by closing any open items
		finalResponses := FinalizeGeminiResponsesStream(streamState, lastUsageMetadata, sequenceNumber)
		for i, finalResponse := range finalResponses {
			if finalResponse == nil {
				logger.Warn("FinalizeGeminiResponsesStream returned nil; skipping final response")
				continue
			}
			finalResponse.ExtraFields = schemas.BifrostResponseExtraFields{
				ChunkIndex: chunkIndex,
				Latency:    time.Since(lastChunkTime).Milliseconds(),
			}

			if postResponseConverter != nil {
				finalResponse = postResponseConverter(finalResponse)
				if finalResponse == nil {
					logger.Warn("postResponseConverter returned nil; skipping final response")
					continue
				}
			}

			chunkIndex++
			sequenceNumber++

			if sendBackRawResponse {
				finalResponse.ExtraFields.RawResponse = "{}" // Final event has no payload
			}
			isLast := i == len(finalResponses)-1
			// Set final latency on the last response (completed event)
			if isLast {
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				finalResponse.ExtraFields.Latency = time.Since(startTime).Milliseconds()
			}
			providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, finalResponse, nil, nil, nil), responseChan, postHookSpanFinalizer)
		}
	}()

	return responseChan, nil
}

func shouldSkipInlineDataForStreamingContext(ctx *schemas.BifrostContext) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback).(bool)
	return ok && v
}

func extractSSEJSONData(line []byte) ([]byte, bool) {
	if !bytes.HasPrefix(line, []byte("data: ")) {
		return nil, false
	}
	return bytes.TrimSpace(line[6:]), true
}

func readNextSSEDataLine(reader *bufio.Reader, skipInlineData bool) ([]byte, error) {
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if data, ok := extractSSEJSONData(line); ok {
			if skipInlineData && bytes.Contains(data, []byte(`"inlineData"`)) {
				continue
			}
			return data, nil
		}
	}
}

func processGeminiStreamChunk(jsonData []byte) (*GenerateContentResponse, error) {
	var geminiErr GeminiGenerationError
	if err := sonic.Unmarshal(jsonData, &geminiErr); err == nil && geminiErr.Error != nil && geminiErr.Error.Code != 0 {
		return nil, &GeminiStreamAPIError{Err: geminiErr.Error}
	}
	var resp GenerateContentResponse
	if err := sonic.Unmarshal(jsonData, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

