// Package opencode implements the Opencode Zen and Go AI gateway providers.
// Both gateways expose an OpenAI-compatible API and share the same implementation,
// differing only in default base URL and provider key.
package opencode

import (
	"context"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// opencodeProvider implements the Provider interface for Opencode Zen and Go gateways.
type opencodeProvider struct {
	providerKey         schemas.ModelProvider
	logger              schemas.Logger
	client              *fasthttp.Client
	streamingClient     *fasthttp.Client
	networkConfig       schemas.NetworkConfig
	sendBackRawRequest  bool
	sendBackRawResponse bool
}

// NewOpencodeZenProvider creates a new Opencode Zen provider instance.
// Zen is the pay-as-you-go gateway at https://opencode.ai/zen/v1.
func NewOpencodeZenProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*opencodeProvider, error) {
	return newOpencodeProvider(config, schemas.OpencodeZen, "https://opencode.ai/zen", logger)
}

// NewOpencodeGoProvider creates a new Opencode Go provider instance.
// Go is the subscription-based gateway at https://opencode.ai/zen/go/v1.
func NewOpencodeGoProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*opencodeProvider, error) {
	return newOpencodeProvider(config, schemas.OpencodeGo, "https://opencode.ai/zen/go", logger)
}

// newOpencodeProvider initializes the shared provider infrastructure.
func newOpencodeProvider(
	config *schemas.ProviderConfig,
	providerKey schemas.ModelProvider,
	defaultBaseURL string,
	logger schemas.Logger,
) (*opencodeProvider, error) {
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
		config.NetworkConfig.BaseURL = defaultBaseURL
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &opencodeProvider{
		providerKey:         providerKey,
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier stored at construction time.
func (p *opencodeProvider) GetProviderKey() schemas.ModelProvider {
	return p.providerKey
}

// ListModels performs a list models request to the Opencode API.
func (p *opencodeProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	return openai.HandleOpenAIListModelsRequest(
		ctx,
		p.client,
		request,
		p.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/models"),
		keys,
		p.networkConfig.ExtraHeaders,
		p.providerKey,
		providerUtils.ShouldSendBackRawRequest(ctx, p.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, p.sendBackRawResponse),
	)
}

// ChatCompletion performs a chat completion request to the Opencode API.
func (p *opencodeProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	return openai.HandleOpenAIChatCompletionRequest(
		ctx,
		p.client,
		p.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		p.networkConfig.ExtraHeaders,
		providerUtils.ShouldSendBackRawRequest(ctx, p.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, p.sendBackRawResponse),
		p.GetProviderKey(),
		nil,
		parseOpencodeError,
		nil,
		p.logger,
	)
}

// ChatCompletionStream performs a streaming chat completion request to the Opencode API.
func (p *opencodeProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return openai.HandleOpenAIChatCompletionStreaming(
		ctx,
		p.streamingClient,
		p.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/chat/completions"),
		request,
		openai.BearerAuthHeader(key),
		p.networkConfig.ExtraHeaders,
		p.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, p.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, p.sendBackRawResponse),
		p.providerKey,
		postHookRunner,
		nil,
		nil,
		parseOpencodeError,
		nil,
		nil,
		nil,
		p.logger,
		postHookSpanFinalizer,
	)
}

// Responses performs a responses request to the Opencode API.
func (p *opencodeProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	return openai.HandleOpenAIResponsesRequest(
		ctx,
		p.client,
		p.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
		request,
		openai.BearerAuthHeader(key),
		p.networkConfig.ExtraHeaders,
		providerUtils.ShouldSendBackRawRequest(ctx, p.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, p.sendBackRawResponse),
		p.providerKey,
		nil,
		parseOpencodeError,
		nil,
		p.logger,
	)
}

// ResponsesStream performs a streaming responses request to the Opencode API.
func (p *opencodeProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return openai.HandleOpenAIResponsesStreaming(
		ctx,
		p.streamingClient,
		p.networkConfig.BaseURL+providerUtils.GetPathFromContext(ctx, "/v1/responses"),
		request,
		openai.BearerAuthHeader(key),
		p.networkConfig.ExtraHeaders,
		p.networkConfig.StreamIdleTimeoutInSeconds,
		providerUtils.ShouldSendBackRawRequest(ctx, p.sendBackRawRequest),
		providerUtils.ShouldSendBackRawResponse(ctx, p.sendBackRawResponse),
		p.providerKey,
		postHookRunner,
		nil,
		parseOpencodeError,
		nil,
		nil,
		nil,
		p.logger,
		postHookSpanFinalizer,
	)
}
