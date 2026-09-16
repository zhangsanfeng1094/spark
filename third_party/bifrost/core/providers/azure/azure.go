// Package azure implements the Azure provider.
package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/valyala/fasthttp"
)

// AzureAuthorizationTokenKey is the context key for the Azure authentication token.
const AzureAuthorizationTokenKey schemas.BifrostContextKey = "azure-authorization-token"

// DefaultAzureScope is the default scope for Azure authentication.
const DefaultAzureScope = "https://cognitiveservices.azure.com/.default"

// DefaultAzureSorageScope is the default scope for Azure storage.
const DefaultAzureStorageScope = "https://storage.azure.com/.default"

// AzureProvider implements the Provider interface for Azure's API.
type AzureProvider struct {
	logger          schemas.Logger        // Logger for provider operations
	client          *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig   schemas.NetworkConfig // Network configuration including extra headers

	credentials         sync.Map // map of tenant ID:client ID to azcore.TokenCredential
	sendBackRawRequest  bool     // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool     // Whether to include raw response in BifrostResponse
}

func (p *AzureProvider) getOrCreateAuth(
	tenantID, clientID, clientSecret string,
) (azcore.TokenCredential, error) {
	key := tenantID + ":" + clientID

	// Fast path
	if val, ok := p.credentials.Load(key); ok {
		return val.(azcore.TokenCredential), nil
	}

	// Slow path - create new credential
	cred, err := azidentity.NewClientSecretCredential(
		tenantID,
		clientID,
		clientSecret,
		nil,
	)
	if err != nil {
		return nil, err
	}

	actual, _ := p.credentials.LoadOrStore(key, cred)
	return actual.(azcore.TokenCredential), nil
}

// getOrCreateDefaultAzureCredential returns a DefaultAzureCredential, creating and caching it if needed.
// It automatically detects the auth environment: managed identity on Azure VMs/containers,
// workload identity in AKS, environment variables, Azure CLI, and more.
func (p *AzureProvider) getOrCreateDefaultAzureCredential() (azcore.TokenCredential, error) {
	const cacheKey = "default_azure_credential"

	if val, ok := p.credentials.Load(cacheKey); ok {
		return val.(azcore.TokenCredential), nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	actual, _ := p.credentials.LoadOrStore(cacheKey, cred)
	return actual.(azcore.TokenCredential), nil
}

// getAzureAuthHeaders returns authentication headers based on priority:
// 1. Service Principal (client ID/secret/tenant ID) - Bearer token
// 2. Context token - Bearer token
// 3. API key - api-key or x-api-key header
func (provider *AzureProvider) getAzureAuthHeaders(ctx *schemas.BifrostContext, key schemas.Key, isAnthropicModel bool) (map[string]string, *schemas.BifrostError) {
	authHeader := make(map[string]string)

	// Service Principal authentication
	if key.AzureKeyConfig != nil && key.AzureKeyConfig.ClientID != nil &&
		key.AzureKeyConfig.ClientSecret != nil && key.AzureKeyConfig.TenantID != nil && key.AzureKeyConfig.ClientID.GetValue() != "" && key.AzureKeyConfig.ClientSecret.GetValue() != "" && key.AzureKeyConfig.TenantID.GetValue() != "" {
		cred, err := provider.getOrCreateAuth(key.AzureKeyConfig.TenantID.GetValue(), key.AzureKeyConfig.ClientID.GetValue(), key.AzureKeyConfig.ClientSecret.GetValue())
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("failed to get or create Azure authentication", err)
		}

		scopes := getAzureScopes(key.AzureKeyConfig.Scopes)

		token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
			Scopes: scopes,
		})
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("failed to get Azure access token", err)
		}

		if token.Token == "" {
			return nil, providerUtils.NewBifrostOperationError("Azure access token is empty", errors.New("token is empty"))
		}

		authHeader["Authorization"] = fmt.Sprintf("Bearer %s", token.Token)
		return authHeader, nil
	}

	// Context token authentication
	if authToken, ok := ctx.Value(AzureAuthorizationTokenKey).(string); ok && authToken != "" {
		authHeader["Authorization"] = fmt.Sprintf("Bearer %s", authToken)
		return authHeader, nil
	}

	value := key.Value.GetValue()
	if value == "" {
		// No explicit credentials provided - attempt DefaultAzureCredential auto-detection.
		// This covers managed identity on Azure VMs/containers, workload identity in AKS,
		// environment variables, Azure CLI, and more - with no config required.
		scopes := getAzureScopes(nil)
		if key.AzureKeyConfig != nil {
			scopes = getAzureScopes(key.AzureKeyConfig.Scopes)
		}

		cred, err := provider.getOrCreateDefaultAzureCredential()
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("no credentials provided and DefaultAzureCredential unavailable", err)
		}

		token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopes})
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("no credentials provided and DefaultAzureCredential failed to get token", err)
		}

		if token.Token == "" {
			return nil, providerUtils.NewBifrostOperationError("no credentials provided and DefaultAzureCredential returned empty token", errors.New("token is empty"))
		}

		authHeader["Authorization"] = fmt.Sprintf("Bearer %s", token.Token)
		return authHeader, nil
	}

	// API key authentication
	if isAnthropicModel {
		authHeader["x-api-key"] = value
	} else {
		authHeader["api-key"] = value
	}
	return authHeader, nil
}

// NewAzureProvider creates a new Azure provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewAzureProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*AzureProvider, error) {
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
	return &AzureProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for Azure.
func (provider *AzureProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.Azure
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
func (provider *AzureProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	endpoint := resolveAzureEndpoint(ctx, key)
	if endpoint == "" {
		return nil, providerUtils.NewConfigurationError("endpoint not set")
	}

	// Create the request
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Set any extra headers from network config
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	req.SetRequestURI(endpoint + providerUtils.GetPathFromContext(ctx, "/openai/v1/models"))
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")

	// Set Azure authentication
	authHeaders, bifrostErr := provider.getAzureAuthHeaders(ctx, key, false)
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}

	// Send the request and measure latency
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Store provider response headers in context before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	// Handle error response
	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, providerUtils.SetErrorLatency(openai.ParseOpenAIError(resp), latency)
	}

	body, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseDecode, err)
	}

	// Read the response body and copy it before releasing the response
	// to avoid use-after-free since resp.Body() references fasthttp's internal buffer
	responseBody := append([]byte(nil), body...)

	// Parse Azure-specific response
	azureResponse := &AzureListModelsResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, azureResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Convert to Bifrost response
	response := azureResponse.ToBifrostListModelsResponse(key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)
	if response == nil {
		return nil, providerUtils.NewBifrostOperationError("failed to convert Azure model list response", nil)
	}

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

// ListModels performs a list models request to Azure's API.
// It retrieves all models accessible by the Azure resource
// Requests are made concurrently for improved performance.
func (provider *AzureProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}


// ChatCompletion performs a chat completion request to Azure's API.
// It formats the request, sends it to Azure, and processes the response.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *AzureProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	endpoint := resolveAzureEndpoint(ctx, key)
	if endpoint == "" {
		return nil, providerUtils.NewConfigurationError("endpoint not set")
	}

	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		// Anthropic-family models use the native Anthropic Messages endpoint via the shared handler.
		authHeader, bifrostErr := provider.getAzureAuthHeaders(ctx, key, true)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		authHeader["anthropic-version"] = resolveAnthropicVersion(ctx)
		return anthropic.HandleAnthropicChatCompletionRequest(
			ctx,
			provider.client,
			fmt.Sprintf("%s/anthropic/v1/messages", endpoint),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.Azure,
				Model:                     request.Model,
				IsStreaming:               false,
				BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			authHeader,
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	// OpenAI-family models use the OpenAI-compatible Azure endpoint via the shared handler.
	authHeader, bifrostErr := provider.getAzureAuthHeaders(ctx, key, false)
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		provider.client,
		fmt.Sprintf("%s/openai/v1/chat/completions", endpoint),
		request,
		authHeader,
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

// ChatCompletionStream performs a streaming chat completion request to Azure's API.
// It supports real-time streaming of responses using Server-Sent Events (SSE).
// Uses Azure-specific URL construction with deployments and supports both api-key and Bearer token authentication.
// Returns a channel containing BifrostResponse objects representing the stream or an error if the request fails.
func (provider *AzureProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	endpoint := resolveAzureEndpoint(ctx, key)
	if endpoint == "" {
		return nil, providerUtils.NewConfigurationError("endpoint not set")
	}
	var url string
	if schemas.ResolveFamily(ctx, request.Model) == schemas.ModelFamilyAnthropic {
		authHeader, err := provider.getAzureAuthHeaders(ctx, key, true)
		if err != nil {
			return nil, err
		}
		authHeader["anthropic-version"] = resolveAnthropicVersion(ctx)
		url = fmt.Sprintf("%s/anthropic/v1/messages", endpoint)

		jsonData, err := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Azure,
			Model:                     request.Model,
			IsStreaming:               true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if err != nil {
			return nil, err
		}

		// Use shared streaming logic from Anthropic
		return anthropic.HandleAnthropicChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			url,
			jsonData,
			authHeader,
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
	} else {
		authHeader, err := provider.getAzureAuthHeaders(ctx, key, false)
		if err != nil {
			return nil, err
		}
		url = fmt.Sprintf("%s/openai/v1/chat/completions", endpoint)

		// Use shared streaming logic from OpenAI
		return openai.HandleOpenAIChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			url,
			request,
			authHeader,
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
}

// Responses performs a responses request to Azure's API.
// It formats the request, sends it to Azure, and processes the response.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *AzureProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	endpoint := resolveAzureEndpoint(ctx, key)
	if endpoint == "" {
		return nil, providerUtils.NewConfigurationError("endpoint not set")
	}

	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		// Anthropic-family models use the native Anthropic Messages endpoint via the shared handler.
		authHeader, bifrostErr := provider.getAzureAuthHeaders(ctx, key, true)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		authHeader["anthropic-version"] = resolveAnthropicVersion(ctx)
		return anthropic.HandleAnthropicResponsesRequest(
			ctx,
			provider.client,
			fmt.Sprintf("%s/anthropic/v1/messages", endpoint),
			request,
			anthropic.AnthropicRequestBuildConfig{
				Provider:                  schemas.Azure,
				Model:                     request.Model,
				IsStreaming:               false,
				ValidateTools:             true,
				BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
				ShouldSendBackRawRequest:  provider.sendBackRawRequest,
				ShouldSendBackRawResponse: provider.sendBackRawResponse,
			},
			authHeader,
			provider.networkConfig.ExtraHeaders,
			nil,
			provider.logger,
		)
	}

	// OpenAI-family models use the OpenAI-compatible Azure endpoint via the shared handler.
	authHeader, bifrostErr := provider.getAzureAuthHeaders(ctx, key, false)
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	path := "openai/v1/responses"
	if v := resolveAPIVersion(ctx, ""); v != "" {
		path += "?api-version=" + v
	}
	return openai.HandleOpenAIResponsesRequest(
		ctx,
		provider.client,
		fmt.Sprintf("%s/%s", endpoint, path),
		request,
		authHeader,
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

// ResponsesStream performs a streaming responses request to Azure's API.
func (provider *AzureProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	endpoint := resolveAzureEndpoint(ctx, key)
	if endpoint == "" {
		return nil, providerUtils.NewConfigurationError("endpoint not set")
	}
	var url string
	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		authHeader, err := provider.getAzureAuthHeaders(ctx, key, true)
		if err != nil {
			return nil, err
		}
		authHeader["anthropic-version"] = resolveAnthropicVersion(ctx)
		url = fmt.Sprintf("%s/anthropic/v1/messages", endpoint)

		jsonData, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Azure,
			Model:                     request.Model,
			IsStreaming:               true,
			ValidateTools:             true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		// Use shared streaming logic from Anthropic
		return anthropic.HandleAnthropicResponsesStream(
			ctx,
			provider.streamingClient,
			url,
			jsonData,
			authHeader,
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
	} else {
		authHeader, err := provider.getAzureAuthHeaders(ctx, key, false)
		if err != nil {
			return nil, err
		}
		path := "openai/v1/responses"
		if v := resolveAPIVersion(ctx, ""); v != "" {
			path += "?api-version=" + v
		}
		url = fmt.Sprintf("%s/%s", endpoint, path)

		// Use shared streaming logic from OpenAI
		return openai.HandleOpenAIResponsesStreaming(
			ctx,
			provider.streamingClient,
			url,
			request,
			authHeader,
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
}
