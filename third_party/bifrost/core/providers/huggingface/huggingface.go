package huggingface

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// HuggingFaceProvider implements the Provider interface for Hugging Face's inference APIs.
type HuggingFaceProvider struct {
	logger                    schemas.Logger
	client                    *fasthttp.Client // unary API requests (ReadTimeout bounds overall response)
	streamingClient           *fasthttp.Client // streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig             schemas.NetworkConfig
	sendBackRawResponse       bool
	sendBackRawRequest        bool
	customProviderConfig      *schemas.CustomProviderConfig
	modelProviderMappingCache *sync.Map
}

// NewHuggingFaceProvider creates a new Hugging Face provider instance configured with the provided settings.
func NewHuggingFaceProvider(config *schemas.ProviderConfig, logger schemas.Logger) *HuggingFaceProvider {
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
	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = defaultInferenceBaseURL
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &HuggingFaceProvider{
		logger:                    logger,
		client:                    client,
		streamingClient:           streamingClient,
		networkConfig:             config.NetworkConfig,
		sendBackRawResponse:       config.SendBackRawResponse,
		sendBackRawRequest:        config.SendBackRawRequest,
		customProviderConfig:      config.CustomProviderConfig,
		modelProviderMappingCache: &sync.Map{},
	}
}

// GetProviderKey returns the provider key, taking custom providers into account.
func (provider *HuggingFaceProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.HuggingFace, provider.customProviderConfig)
}

// buildRequestURL composes the final request URL based on context overrides.
func (provider *HuggingFaceProvider) buildRequestURL(ctx *schemas.BifrostContext, defaultPath string, requestType schemas.RequestType) string {
	path, isCompleteURL := providerUtils.GetRequestPath(ctx, defaultPath, provider.customProviderConfig, requestType)
	if isCompleteURL {
		return path
	}
	return provider.networkConfig.BaseURL + path
}

func (provider *HuggingFaceProvider) completeRequest(ctx *schemas.BifrostContext, jsonData []byte, url string, key string) ([]byte, time.Duration, map[string]string, *schemas.BifrostError) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	req.SetRequestURI(url)
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	if !providerUtils.ApplyLargePayloadRequestBodyWithModelNormalization(ctx, req, schemas.HuggingFace) {
		req.SetBody(jsonData)
	}

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, latency, nil, bifrostErr
	}

	// Extract provider response headers before status check so error responses also forward them
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(parseHuggingFaceImageError(resp), latency)
	}

	body, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		return nil, latency, providerResponseHeaders, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseDecode, err)
	}

	// Read the response body and copy it before releasing the response
	// to avoid use-after-free since resp.Body() references fasthttp's internal buffer
	bodyCopy := append([]byte(nil), body...)

	return bodyCopy, latency, providerResponseHeaders, nil
}

func (provider *HuggingFaceProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	providerName := provider.GetProviderKey()

	type providerResult struct {
		provider inferenceProvider
		response *HuggingFaceListModelsResponse
		latency  int64
		rawResp  map[string]interface{}
		err      *schemas.BifrostError
	}

	resultsChan := make(chan providerResult, len(INFERENCE_PROVIDERS))
	var wg sync.WaitGroup

	for _, infProvider := range INFERENCE_PROVIDERS {
		wg.Add(1)
		go func(inferProvider inferenceProvider) {
			defer wg.Done()

			req := fasthttp.AcquireRequest()
			resp := fasthttp.AcquireResponse()
			defer fasthttp.ReleaseRequest(req)
			defer fasthttp.ReleaseResponse(resp)

			providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

			modelHubURL := provider.buildModelHubURL(request, inferProvider)
			req.SetRequestURI(modelHubURL)
			req.Header.SetMethod(http.MethodGet)
			req.Header.SetContentType("application/json")
			if key.Value.GetValue() != "" {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
			}

			latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
			defer wait()
			if bifrostErr != nil {
				resultsChan <- providerResult{provider: inferProvider, err: bifrostErr}
				return
			}

			if resp.StatusCode() != fasthttp.StatusOK {
				var errorResp HuggingFaceHubError
				bifrostErr := providerUtils.HandleProviderAPIError(resp, &errorResp)
				if bifrostErr.Error == nil {
					bifrostErr.Error = &schemas.ErrorField{}
				}
				if strings.TrimSpace(errorResp.Message) != "" {
					bifrostErr.Error.Message = errorResp.Message
				}
				resultsChan <- providerResult{provider: inferProvider, err: bifrostErr}
				return
			}

			body, err := providerUtils.CheckAndDecodeBody(resp)
			if err != nil {
				resultsChan <- providerResult{provider: inferProvider, err: providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseDecode, err)}
				return
			}

			var huggingfaceAPIResponse HuggingFaceListModelsResponse
			var rawResponse interface{}
			var rawRequest interface{}
			rawRequest, rawResponse, bifrostErr = providerUtils.HandleProviderResponse(body, &huggingfaceAPIResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
			if bifrostErr != nil {
				resultsChan <- providerResult{provider: inferProvider, err: bifrostErr}
				return
			}
			var rawRespMap map[string]interface{}
			if rawResponse != nil {
				if converted, ok := rawResponse.(map[string]interface{}); ok {
					rawRespMap = converted
				}
			}
			// If raw request was requested, attach it to the raw response map
			if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) && rawRequest != nil {
				if rawRespMap == nil {
					rawRespMap = make(map[string]interface{})
				}
				rawRespMap["raw_request"] = rawRequest
			}

			resultsChan <- providerResult{
				provider: inferProvider,
				response: &huggingfaceAPIResponse,
				latency:  latency.Milliseconds(),
				rawResp:  rawRespMap,
			}
		}(infProvider)
	}

	// Close results channel after all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Aggregate results
	aggregatedResponse := &schemas.BifrostListModelsResponse{
		Data: make([]schemas.Model, 0),
	}
	var totalLatency int64
	var successCount int
	var firstError *schemas.BifrostError
	var rawResponses []map[string]interface{}

	for result := range resultsChan {
		if result.err != nil {
			if firstError == nil {
				firstError = result.err
			}
			continue
		}

		if result.response != nil {
			providerResponse := result.response.ToBifrostListModelsResponse(providerName, result.provider, key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)
			if providerResponse != nil {
				aggregatedResponse.Data = append(aggregatedResponse.Data, providerResponse.Data...)
				totalLatency += result.latency
				successCount++
				if result.rawResp != nil {
					rawResponses = append(rawResponses, result.rawResp)
				}
			}
		}
	}

	// If all requests failed, return the first error
	if successCount == 0 && firstError != nil {
		return nil, firstError
	}

	// Calculate average latency
	if successCount > 0 {
		aggregatedResponse.ExtraFields.Latency = totalLatency / int64(successCount)
	}

	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) && len(rawResponses) > 0 {
		// Combine all raw responses into a single map
		combinedRaw := make(map[string]interface{})
		for i, raw := range rawResponses {
			combinedRaw[fmt.Sprintf("provider_%d", i)] = raw
		}
		aggregatedResponse.ExtraFields.RawResponse = combinedRaw
	}

	return aggregatedResponse, nil
}

// ListModels queries the Hugging Face model hub API to list models served by the inference provider.
func (provider *HuggingFaceProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {

	if err := providerUtils.CheckOperationAllowed(schemas.HuggingFace, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
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

func (provider *HuggingFaceProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.HuggingFace, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	inferenceProvider, modelName, nameErr := splitIntoModelProvider(request.Model)
	if nameErr != nil {
		return nil, &schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: nameErr.Error(),
				Error:   nameErr,
			},
		}
	}
	if inferenceProvider != "" {
		request.Model = fmt.Sprintf("%s:%s", modelName, inferenceProvider)
	} else {
		request.Model = modelName
	}

	jsonBody, err := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			reqBody, err := ToHuggingFaceChatCompletionRequest(request)
			if err != nil {
				return nil, err
			}
			if reqBody != nil {
				reqBody.Stream = schemas.Ptr(false)
			}
			return reqBody, nil
		})
	if err != nil {
		return nil, err
	}

	requestURL := provider.buildRequestURL(ctx, "/v1/chat/completions", schemas.ChatCompletionRequest)

	responseBody, latency, providerResponseHeaders, err := provider.completeRequest(ctx, jsonBody, requestURL, key.Value.GetValue())
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if err != nil {
		return nil, providerUtils.EnrichError(ctx, err, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	bifrostResponse := &schemas.BifrostChatResponse{}

	var rawResponse interface{}
	var rawRequest interface{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, bifrostResponse, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Ensure model is set correctly
	if bifrostResponse.Model == "" {
		bifrostResponse.Model = request.Model
	}

	// Set object if not already set
	if bifrostResponse.Object == "" {
		bifrostResponse.Object = "chat.completion"
	}

	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		bifrostResponse.ExtraFields.RawResponse = rawResponse
	}

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		bifrostResponse.ExtraFields.RawRequest = rawRequest
	}

	return bifrostResponse, nil
}

func (provider *HuggingFaceProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.HuggingFace, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}

	inferenceProvider, modelName, nameErr := splitIntoModelProvider(request.Model)
	if nameErr != nil {
		return nil, &schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: nameErr.Error(),
				Error:   nameErr,
			},
		}
	}
	if inferenceProvider != "" {
		request.Model = fmt.Sprintf("%s:%s", modelName, inferenceProvider)
	} else {
		request.Model = modelName
	}

	customRequestConverter := func(request *schemas.BifrostChatRequest) (providerUtils.RequestBodyWithExtraParams, error) {
		reqBody, err := ToHuggingFaceChatCompletionStreamRequest(request)
		if err != nil {
			return nil, err
		}
		return reqBody, nil
	}

	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		provider.streamingClient,
		provider.buildRequestURL(ctx, "/v1/chat/completions", schemas.ChatCompletionStreamRequest),
		request,
		openai.BearerAuthHeader(key),
		provider.networkConfig.ExtraHeaders,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
		provider.GetProviderKey(),
		postHookRunner,
		customRequestConverter,
		nil,
		nil,
		nil,
		nil,
		nil,
		provider.logger,
		postHookSpanFinalizer,
	)
}
