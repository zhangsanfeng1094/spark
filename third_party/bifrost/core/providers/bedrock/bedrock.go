package bedrock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	openai "github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// BedrockProvider implements the Provider interface for AWS Bedrock.
type BedrockProvider struct {
	logger                schemas.Logger                // Logger for provider operations
	client                *http.Client                  // HTTP client for unary API requests (Client.Timeout bounds overall response)
	streamingClient       *http.Client                  // HTTP client for streaming API requests (no Timeout; idle governed by NewIdleTimeoutReader)
	mantleClient          *fasthttp.Client              // fasthttp client for Bedrock Mantle unary requests (OpenAI-compatible and native-Anthropic paths)
	mantleStreamingClient *fasthttp.Client              // fasthttp streaming client for Bedrock Mantle streaming requests
	networkConfig         schemas.NetworkConfig         // Network configuration including extra headers
	customProviderConfig  *schemas.CustomProviderConfig // Custom provider config
	sendBackRawRequest    bool                          // Whether to include raw request in BifrostResponse
	sendBackRawResponse   bool                          // Whether to include raw response in BifrostResponse
}

// assumeRoleCredsCache caches *aws.CredentialsCache instances keyed by the
// unique combination of role parameters so that STS AssumeRole is not called
// on every request.
var assumeRoleCredsCache sync.Map

// bedrockChatResponsePool provides a pool for Bedrock response objects.
var bedrockChatResponsePool = sync.Pool{
	New: func() interface{} {
		return &BedrockConverseResponse{}
	},
}

// acquireBedrockChatResponse gets a Bedrock response from the pool and resets it.
func acquireBedrockChatResponse() *BedrockConverseResponse {
	resp := bedrockChatResponsePool.Get().(*BedrockConverseResponse)
	*resp = BedrockConverseResponse{} // Reset the struct
	return resp
}

// releaseBedrockChatResponse returns a Bedrock response to the pool.
func releaseBedrockChatResponse(resp *BedrockConverseResponse) {
	if resp != nil {
		bedrockChatResponsePool.Put(resp)
	}
}

// NewBedrockProvider creates a new Bedrock provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts and AWS-specific settings.
func NewBedrockProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*BedrockProvider, error) {
	config.CheckAndSetDefaults()

	requestTimeout := time.Second * time.Duration(config.NetworkConfig.DefaultRequestTimeoutInSeconds)

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxConnsPerHost:       config.NetworkConfig.MaxConnsPerHost,
		MaxIdleConns:          schemas.DefaultMaxIdleConnsPerHost,
		MaxIdleConnsPerHost:   schemas.DefaultMaxIdleConnsPerHost,
		IdleConnTimeout:       time.Second * time.Duration(config.NetworkConfig.KeepAliveTimeoutInSeconds),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: requestTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     config.NetworkConfig.EnforceHTTP2,
	}

	// Disable HTTP/2 auto-negotiation when not explicitly enforced.
	// ForceAttemptHTTP2=false alone does NOT prevent HTTP/2 — Go's http2 package
	// auto-registers h2 via TLSNextProto in init(). Setting TLSNextProto to an
	// empty map prevents ALPN negotiation from upgrading connections to h2.
	if !config.NetworkConfig.EnforceHTTP2 {
		transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	}

	// Apply TLS settings from NetworkConfig
	caCertPEM := ""
	if config.NetworkConfig.CACertPEM != nil {
		caCertPEM = config.NetworkConfig.CACertPEM.GetValue()
	}
	if config.NetworkConfig.InsecureSkipVerify || caCertPEM != "" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if config.NetworkConfig.InsecureSkipVerify {
			tlsConfig.InsecureSkipVerify = true
		}
		if caCertPEM != "" {
			certPool, err := x509.SystemCertPool()
			if err != nil {
				certPool = x509.NewCertPool()
			}
			if !certPool.AppendCertsFromPEM([]byte(caCertPEM)) {
				return nil, fmt.Errorf("failed to parse CA certificate PEM")
			}
			tlsConfig.RootCAs = certPool
		}
		transport.TLSClientConfig = tlsConfig
	}

	// When HTTP/2 is enforced and a ping interval is configured, send client-initiated
	// PING keepalives so an idle streaming connection isn't closed by an intermediary
	// (surfaces as "unexpected EOF"). Left off by default; opt in via the interval.
	if config.NetworkConfig.EnforceHTTP2 && config.NetworkConfig.HTTP2PingIntervalInSeconds > 0 {
		transport.HTTP2 = &http.HTTP2Config{
			SendPingTimeout: time.Duration(config.NetworkConfig.HTTP2PingIntervalInSeconds) * time.Second,
		}
	}

	client := &http.Client{Transport: transport, Timeout: requestTimeout}
	streamingClient := providerUtils.BuildStreamingHTTPClient(client)

	// fasthttp clients for Bedrock Mantle (shared by OpenAI-compatible and native-Anthropic paths).
	// ReadTimeout is the shared provider request timeout, not an OpenAI-specific value; oversized
	// Anthropic responses are handled by PrepareResponseStreaming, not by these static settings.
	mantleFasthttpClient := &fasthttp.Client{
		ReadTimeout:         requestTimeout,
		WriteTimeout:        requestTimeout,
		MaxConnsPerHost:     config.NetworkConfig.MaxConnsPerHost,
		MaxIdleConnDuration: time.Second * time.Duration(config.NetworkConfig.KeepAliveTimeoutInSeconds),
		MaxConnWaitTimeout:  requestTimeout,
		MaxConnDuration:     time.Second * time.Duration(schemas.DefaultMaxConnDurationInSeconds),
		ConnPoolStrategy:    fasthttp.FIFO,
	}
	mantleFasthttpClient = providerUtils.ConfigureProxy(mantleFasthttpClient, config.ProxyConfig, logger)
	mantleFasthttpClient = providerUtils.ConfigureDialer(mantleFasthttpClient, config.NetworkConfig.AllowPrivateNetwork)
	mantleFasthttpClient = providerUtils.ConfigureTLS(mantleFasthttpClient, config.NetworkConfig, logger)
	mantleStreamingFasthttpClient := providerUtils.BuildStreamingClient(mantleFasthttpClient)

	// Pre-warm response pools
	for i := 0; i < config.ConcurrencyAndBufferSize.Concurrency; i++ {
		bedrockChatResponsePool.Put(&BedrockConverseResponse{})
	}

	return &BedrockProvider{
		logger:                logger,
		client:                client,
		streamingClient:       streamingClient,
		mantleClient:          mantleFasthttpClient,
		mantleStreamingClient: mantleStreamingFasthttpClient,
		networkConfig:         config.NetworkConfig,
		customProviderConfig:  config.CustomProviderConfig,
		sendBackRawRequest:    config.SendBackRawRequest,
		sendBackRawResponse:   config.SendBackRawResponse,
	}, nil
}

// GetProviderKey returns the provider identifier for Bedrock.
func (provider *BedrockProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.Bedrock, provider.customProviderConfig)
}

// isStreamTransportError reports whether err is a transport-level connection
// failure that occurred while reading the EventStream body — as opposed to a
// semantic error (JSON parse failure, AWS exception event, etc.).
//
// Transport errors are caused by the underlying TCP/HTTP/2 connection being
// closed or reset (e.g. AWS Bedrock closing idle connections after ~60 s).
// They are retryable: the request has not yet been partially processed by the
// provider, so a fresh connection can be used to retry transparently.
//
// Detected cases:
//   - *net.OpError  — "use of closed network connection", connection reset, etc.
//   - *net.DNSError — transient DNS failure
//   - io.ErrUnexpectedEOF — HTTP/2 stream closed mid-frame (body abruptly ended)
func isStreamTransportError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var checksumErr eventstream.ChecksumError
	if errors.As(err, &checksumErr) {
		return true
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	return errors.As(err, &opErr) || errors.As(err, &dnsErr)
}

// retryableBedrockExceptions maps AWS Bedrock EventStream exception types (the
// camelCase shape names AWS uses for ConverseStream / InvokeModelWithResponseStream
// in-stream exception members) to a retryable HTTP status code. These exceptions
// are transient and should be retried — the retry gate in executeRequestWithRetries
// checks StatusCode against transientServerStatusCodes (500, 502, 503, 504) for
// same-key retries and perKeyFailureStatusCodes (429) for rotation-triggered retries.
//
// Some AWS exceptions have a native status code the gate does not recognize
// (modelStreamErrorException=424, modelTimeoutException=408); they are mapped to
// the nearest gate-recognized transient code so the retry still fires. The client
// still sees the original exception type — only the internal retry hint is mapped.
//
// modelNotReadyException is an HTTP-level error (not an in-stream member) but is
// kept here because AWS auto-retries it; it is harmless when it never matches a
// stream event. validationException / accessDeniedException / resourceNotFoundException
// are intentionally absent — they are terminal request-bound errors.
var retryableBedrockExceptions = map[string]int{
	"throttlingException":         429,
	"serviceUnavailableException": 503,
	"modelNotReadyException":      503,
	"internalServerException":     500,
	"modelStreamErrorException":   503, // native 424; AWS guidance: "Retry your request"
	"modelTimeoutException":       504, // native 408; processing timeout, transient
}

// newBedrockStreamException builds a BifrostError from an AWS EventStream
// exception message (any :message-type other than "event"). It preserves the
// upstream exception type — the payload's "__type" when present, else the
// :exception-type header value (excType) — so downstream conversion
// (ToBedrockError) forwards it instead of falling back to "InternalServerError".
//
// Retryable exceptions are emitted with IsBifrostError:false and the equivalent
// HTTP status so the retry gate in executeRequestWithRetries handles them;
// non-retryable ones are terminal (IsBifrostError:true). providerName is an
// optional label prefix for the message.
func newBedrockStreamException(providerName, excType string, payload []byte) *schemas.BifrostError {
	errMsg := string(payload)
	var bedrockErr BedrockError
	if err := sonic.Unmarshal(payload, &bedrockErr); err == nil && bedrockErr.Message != "" {
		errMsg = bedrockErr.Message
	}

	fwdType := bedrockErr.Type
	if fwdType == "" {
		fwdType = excType
	}

	prefix := "stream"
	if providerName != "" {
		prefix = providerName + " stream"
	}

	streamErr := &schemas.BifrostError{
		IsBifrostError: false,
		Error: &schemas.ErrorField{
			Message: fmt.Sprintf("%s %s: %s", prefix, excType, errMsg),
		},
	}
	if fwdType != "" {
		streamErr.Type = &fwdType
	}
	if statusCode, ok := retryableBedrockExceptions[excType]; ok {
		sc := statusCode
		streamErr.StatusCode = &sc
	} else {
		streamErr.IsBifrostError = true
	}
	return streamErr
}

// completeRequest sends a request to Bedrock's API and handles the response.
// It constructs the API URL, sets up AWS authentication, and processes the response.
// Returns the response body, request latency, or an error if the request fails.
func (provider *BedrockProvider) completeRequest(ctx *schemas.BifrostContext, jsonData []byte, path string, key schemas.Key, model string) ([]byte, time.Duration, map[string]string, *schemas.BifrostError) {
	config := key.BedrockKeyConfig
	region := resolveBedrockRegion(ctx, key, model)

	// Create the request with the JSON body
	requestURL := fmt.Sprintf("https://%s/model/%s", resolveBedrockHost(bedrockEndpoints(config), bedrockServiceRuntime, region), path)
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, nil, &schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error creating request",
				Error:   err,
			},
		}
	}

	// Set any extra headers from network config
	providerUtils.SetExtraHeadersHTTP(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	if filtered := anthropic.FilterBetaHeadersForProvider(anthropic.MergeBetaHeaders(ctx, provider.networkConfig.ExtraHeaders), schemas.Bedrock, provider.networkConfig.BetaHeaderOverrides); len(filtered) > 0 {
		req.Header.Set(anthropic.AnthropicBetaHeader, strings.Join(filtered, ","))
	} else {
		req.Header.Del(anthropic.AnthropicBetaHeader)
	}

	// If Value is set, use API Key authentication - else use IAM role authentication
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	} else {
		// Sign the request using either explicit credentials or IAM role authentication
		if err := signAWSRequest(ctx, req, config, region, bedrockSigningService); err != nil {
			return nil, 0, nil, err
		}
	}

	body, latency, providerResponseHeaders, bErr := provider.executeBedrockRequest(req)
	return body, latency, providerResponseHeaders, bErr
}

// executeBedrockRequest sends an already-built (and authenticated) request via the
// unary HTTP client, measures latency, and parses a Bedrock error envelope on non-200
// responses. Used by completeRequest for the bedrock-runtime (Converse) path.
func (provider *BedrockProvider) executeBedrockRequest(req *http.Request) ([]byte, time.Duration, map[string]string, *schemas.BifrostError) {
	// Execute the request and measure latency
	startTime := time.Now()
	resp, err := providerUtils.DoHTTPRequest(provider.client, req)
	latency := time.Since(startTime)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   err,
				},
			}, latency)
		}
		// Check for timeout first using net.Error before checking net.OpError
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, latency, nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		if errors.Is(err, http.ErrHandlerTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, latency, nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		// Check for DNS lookup and network errors after timeout checks
		var opErr *net.OpError
		var dnsErr *net.DNSError
		if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
			return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Message: schemas.ErrProviderNetworkError,
					Error:   err,
				},
			}, latency)
		}
		return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: schemas.ErrProviderDoRequest,
				Error:   err,
			},
		}, latency)
	}

	// Extract provider response headers before closing the body
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeadersFromHTTP(resp)
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error reading request",
				Error:   err,
			},
		}, latency)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(parseBedrockHTTPError(resp.StatusCode, resp.Header, body), latency)
	}

	return body, latency, providerResponseHeaders, nil
}

// completeAgentRuntimeRequest sends a request to Bedrock Agent Runtime API and handles the response.
// This is used for operations (like rerank) that are served by bedrock-agent-runtime.
func (provider *BedrockProvider) completeAgentRuntimeRequest(ctx *schemas.BifrostContext, jsonData []byte, path string, key schemas.Key) ([]byte, time.Duration, map[string]string, *schemas.BifrostError) {
	config := key.BedrockKeyConfig

	region := DefaultBedrockRegion
	if config.Region != nil && config.Region.GetValue() != "" {
		region = config.Region.GetValue()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s%s", resolveBedrockHost(bedrockEndpoints(config), bedrockServiceAgentRuntime, region), path), bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, nil, &schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error creating request",
				Error:   err,
			},
		}
	}

	providerUtils.SetExtraHeadersHTTP(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	} else {
		if err := signAWSRequest(ctx, req, config, region, bedrockSigningService); err != nil {
			return nil, 0, nil, err
		}
	}

	startTime := time.Now()
	resp, err := providerUtils.DoHTTPRequest(provider.client, req)
	latency := time.Since(startTime)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   err,
				},
			}, latency)
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, latency, nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		if errors.Is(err, http.ErrHandlerTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, latency, nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		var opErr *net.OpError
		var dnsErr *net.DNSError
		if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
			return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Message: schemas.ErrProviderNetworkError,
					Error:   err,
				},
			}, latency)
		}
		return nil, latency, nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: schemas.ErrProviderDoRequest,
				Error:   err,
			},
		}, latency)
	}

	// Extract provider response headers before closing the body
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeadersFromHTTP(resp)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error reading request",
				Error:   err,
			},
		}, latency)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, latency, providerResponseHeaders, providerUtils.SetErrorLatency(parseBedrockHTTPError(resp.StatusCode, resp.Header, body), latency)
	}

	return body, latency, providerResponseHeaders, nil
}

// makeStreamingRequest creates a streaming request to Bedrock's API.
// It formats the request, sends it to Bedrock, and returns the response.
// Returns the response body and an error if the request fails.
func (provider *BedrockProvider) makeStreamingRequest(ctx *schemas.BifrostContext, jsonData []byte, key schemas.Key, model string, action string) (*http.Response, *schemas.BifrostError) {
	// Parse region and path in one pass to avoid running the regex twice.
	path, region := provider.getModelPathAndRegion(ctx, action, model, key)

	// Create HTTP request for streaming
	requestURL := fmt.Sprintf("https://%s/model/%s", resolveBedrockHost(bedrockEndpoints(key.BedrockKeyConfig), bedrockServiceRuntime, region), path)
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(jsonData))
	if reqErr != nil {
		return nil, providerUtils.NewBifrostOperationError("error creating request", reqErr)
	}

	// Set any extra headers from network config
	providerUtils.SetExtraHeadersHTTP(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	if filtered := anthropic.FilterBetaHeadersForProvider(anthropic.MergeBetaHeaders(ctx, provider.networkConfig.ExtraHeaders), schemas.Bedrock, provider.networkConfig.BetaHeaderOverrides); len(filtered) > 0 {
		req.Header.Set(anthropic.AnthropicBetaHeader, strings.Join(filtered, ","))
	} else {
		req.Header.Del(anthropic.AnthropicBetaHeader)
	}

	// If Value is set, use API Key authentication - else use IAM role authentication
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	// Force identity encoding so Go's net/http transport does NOT auto-negotiate
	// gzip. A gzip-compressed eventstream buffers upstream until the stream
	// completes, collapsing TTFB to the total generation time (issue #4542).
	req.Header.Set("Accept-Encoding", "identity")
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	} else {
		// Sign the request using either explicit credentials or IAM role authentication
		if err := signAWSRequest(ctx, req, key.BedrockKeyConfig, region, bedrockSigningService); err != nil {
			return nil, err
		}
	}

	// Make the request
	startTime := time.Now()
	resp, respErr := providerUtils.DoHTTPRequest(provider.streamingClient, req)
	latency := time.Since(startTime)
	if respErr != nil {
		if errors.Is(respErr, context.Canceled) {
			return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   respErr,
				},
			}, latency)
		}
		// Check for timeout first using net.Error before checking net.OpError
		var netErr net.Error
		if errors.As(respErr, &netErr) && netErr.Timeout() {
			return nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, respErr), latency)
		}
		if errors.Is(respErr, http.ErrHandlerTimeout) || errors.Is(respErr, context.DeadlineExceeded) {
			return nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, respErr), latency)
		}
		// Check for DNS lookup and network errors after timeout checks
		var opErr *net.OpError
		var dnsErr *net.DNSError
		if errors.As(respErr, &opErr) || errors.As(respErr, &dnsErr) {
			return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Message: schemas.ErrProviderNetworkError,
					Error:   respErr,
				},
			}, latency)
		}
		return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: schemas.ErrProviderDoRequest,
				Error:   respErr,
			},
		}, latency)
	}

	// Extract provider response headers before status check so error responses also forward them
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeadersFromHTTP(resp))

	// Check for HTTP errors — use parseBedrockHTTPError to preserve upstream error details
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, providerUtils.SetErrorLatency(parseBedrockHTTPError(resp.StatusCode, resp.Header, body), latency)
	}

	return resp, nil
}

// Returns a BifrostError if signing fails.
// unsignableHeaders must not be covered by the SigV4 signature: hop-by-hop and
// proxy-managed headers that an intermediary may rewrite in transit, which invalidates the
// signature. AWS's signing guide requires only host and x-amz-* to be signed and warns
// against "volatile transport headers that are mutated by proxies, load balancers, and the
// nodes in a distributed system". The AWS SDK itself skips only authorization, user-agent,
// x-amzn-trace-id, expect and transfer-encoding, so the rest is on us. These are removed for
// signing and restored afterwards, so what goes on the wire is unchanged.
var unsignableHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"te":                  {},
	"trailer":             {},
	"upgrade":             {},
	"proxy-authorization": {},
	"proxy-authenticate":  {},
	"x-real-ip":           {},
	"x-request-id":        {},
}

// internalHeaderPrefix marks Bifrost's own headers. They carry the caller's virtual key, so
// they are dropped outright rather than restored — they must never reach a provider.
const internalHeaderPrefix = "x-bf-"

// restoredHeader is a header lifted off the request for signing, to be put back after.
type restoredHeader struct {
	name   string
	values []string
}

// stripHeadersForSigning removes headers that must not be signed and returns the ones to put
// back after signing. Bifrost-internal headers are dropped and never returned. A request with
// no volatile headers — the common case — allocates nothing and leaves the request untouched.
func stripHeadersForSigning(header http.Header) []restoredHeader {
	var restore []restoredHeader
	for name, values := range header {
		lower := strings.ToLower(name)
		internal := strings.HasPrefix(lower, internalHeaderPrefix)
		_, volatile := unsignableHeaders[lower]
		if !internal && !volatile && !strings.HasPrefix(lower, "x-forwarded-") {
			continue
		}
		if !internal {
			restore = append(restore, restoredHeader{name: name, values: values})
		}
		// The name came straight off the map, so skip Del's redundant canonicalization.
		delete(header, name)
	}
	return restore
}

func signAWSRequest(
	ctx *schemas.BifrostContext,
	req *http.Request,
	keyCfg *schemas.BedrockKeyConfig,
	region, service string,
) (signErr *schemas.BifrostError) {
	// "request-sign" overhead phase: AWS SigV4 signing is real per-request work that
	// otherwise hides in "core". The nested "credentials-fetch" span (below) isolates
	// the network portion (STS AssumeRole / credential-provider Retrieve) from the CPU
	// crypto, so a cold credential cache on an idle box reads as its own bucket instead
	// of inflating core.
	// Scoped so the nested "credentials-fetch" span below is a true child and its
	// time is subtracted from request-sign's self-time exactly once (not double-counted
	// in both buckets). restore() reinstates the prior parent before the span ends.
	if st, sh, restore := providerUtils.StartScopedPhaseSpan(ctx, "request-sign"); st != nil {
		defer func() {
			restore()
			if signErr != nil {
				st.EndSpan(sh, schemas.SpanStatusError, "request signing failed")
			} else {
				st.EndSpan(sh, schemas.SpanStatusOk, "")
			}
		}()
	}

	var accessKey, secretKey schemas.SecretVar
	var sessionToken, roleARN, externalID, sessionName *schemas.SecretVar

	if keyCfg != nil {
		accessKey = keyCfg.AccessKey
		secretKey = keyCfg.SecretKey
		sessionToken = keyCfg.SessionToken
		roleARN = keyCfg.RoleARN
		externalID = keyCfg.ExternalID
		sessionName = keyCfg.RoleSessionName
	}

	// Set required headers before signing (only if not already set)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// Calculate SHA256 hash of the request body
	var bodyHash string
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return providerUtils.NewBifrostOperationError("error reading request body", err)
		}
		// Restore the body for subsequent reads
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		hash := sha256.Sum256(bodyBytes)
		bodyHash = hex.EncodeToString(hash[:])
	} else {
		// For empty body, use the hash of an empty string
		hash := sha256.Sum256([]byte{})
		bodyHash = hex.EncodeToString(hash[:])
	}

	// Set x-amz-content-sha256 header (required for S3, harmless for other AWS services)
	req.Header.Set("x-amz-content-sha256", bodyHash)

	var cfg aws.Config
	var err error

	// If both accessKey and secretKey are empty, use the default credential provider chain
	// This will automatically use IAM roles, environment variables, shared credentials, etc.
	if accessKey.GetValue() == "" && secretKey.GetValue() == "" {
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
	} else {
		// Use explicit credentials when provided
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				creds := aws.Credentials{
					AccessKeyID:     accessKey.GetValue(),
					SecretAccessKey: secretKey.GetValue(),
				}
				if sessionToken != nil && sessionToken.GetValue() != "" {
					creds.SessionToken = sessionToken.GetValue()
				}
				return creds, nil
			})),
		)
	}
	if err != nil {
		return providerUtils.NewBifrostOperationError("failed to load aws config", err)
	}

	if roleARN != nil && roleARN.GetValue() != "" {
		extID := ""
		if externalID != nil {
			extID = externalID.GetValue()
		}
		sessName := "bifrost-session"
		if sessionName != nil && sessionName.GetValue() != "" {
			sessName = sessionName.GetValue()
		}
		sourceIdentity := "default_chain"
		if accessKey.GetValue() != "" || secretKey.GetValue() != "" {
			sourceIdentity = accessKey.GetValue()
			if sessionToken != nil && sessionToken.GetValue() != "" {
				tokenHash := sha256.Sum256([]byte(sessionToken.GetValue()))
				sourceIdentity = sourceIdentity + "|" + hex.EncodeToString(tokenHash[:8])
			}
		}
		cacheKey := strings.Join([]string{
			region,
			roleARN.GetValue(),
			extID,
			sessName,
			sourceIdentity,
		}, "|")

		if cached, ok := assumeRoleCredsCache.Load(cacheKey); ok {
			cfg.Credentials = cached.(*aws.CredentialsCache)
		} else {
			stsClient := sts.NewFromConfig(cfg)

			opts := func(o *stscreds.AssumeRoleOptions) {
				if extID != "" {
					o.ExternalID = aws.String(extID)
				}
				o.RoleSessionName = sessName
			}

			credsCache := aws.NewCredentialsCache(
				stscreds.NewAssumeRoleProvider(
					stsClient,
					roleARN.GetValue(),
					opts,
				),
			)
			actual, _ := assumeRoleCredsCache.LoadOrStore(cacheKey, credsCache)
			cfg.Credentials = actual.(*aws.CredentialsCache)
		}
	}

	// The SDK signs every header still on the request, so lift the volatile ones out for
	// the duration of signing and put them back before the request goes out.
	restoreHeaders := stripHeadersForSigning(req.Header)

	// Create the AWS signer
	signer := v4.NewSigner()

	// Get credentials. Nested "credentials-fetch" span: on a cache miss this triggers a
	// network round trip (STS AssumeRole when RoleARN is set, or the default provider
	// chain's IMDS/env/STS lookup), which is the dominant cost on an idle box whose
	// credential cache has expired between sparse requests.
	credT, credH := providerUtils.StartPhaseSpan(ctx, "credentials-fetch")
	creds, err := cfg.Credentials.Retrieve(ctx)
	if credT != nil {
		if err != nil {
			credT.EndSpan(credH, schemas.SpanStatusError, "credential retrieval failed")
		} else {
			credT.EndSpan(credH, schemas.SpanStatusOk, "")
		}
	}
	if err != nil {
		return providerUtils.NewBifrostOperationError("failed to retrieve aws credentials", err)
	}

	// Sign the request with AWS Signature V4
	err = signer.SignHTTP(ctx, creds, req, bodyHash, service, region, time.Now())
	for _, h := range restoreHeaders {
		req.Header[h.name] = h.values
	}
	if err != nil {
		return providerUtils.NewBifrostOperationError("failed to sign request", err)
	}

	return nil
}

// listModelsByKey performs a list models request to Bedrock's API for a single key.
// It retrieves all foundation models available in Amazon Bedrock for a specific key.
// listMantleModels lists models from the Bedrock Mantle (OpenAI-compatible) /v1/models
// endpoint, converted to a Bifrost response with the same allow/blacklist/alias gating as
// the foundation-model path. The bare /v1/models path returns the full mantle catalog
// (including the mantle-only gpt-5.x / gemma-4 models that ListFoundationModels omits).
// The request is signed as it is sent (mantleSigV4Headers signs POST and can't be reused
// for this GET). Best-effort: returns nil on any failure so the foundation-model list is
// still returned.
func (provider *BedrockProvider) listMantleModels(ctx *schemas.BifrostContext, key schemas.Key, region string, unfiltered bool) *schemas.BifrostListModelsResponse {
	mURL := mantleOpenAIURL(bedrockEndpoints(key.BedrockKeyConfig), region, "", "models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mURL, nil)
	if err != nil {
		provider.logger.Warn("failed to build mantle list-models request: %v", err)
		return nil
	}
	providerUtils.SetExtraHeadersHTTP(ctx, req, WithMantleProject(provider.networkConfig.ExtraHeaders, MantleOpenAIProjectHeader, resolveMantleProjectID(ctx, key)), nil)
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	} else if bifrostErr := signAWSRequest(ctx, req, key.BedrockKeyConfig, region, bedrockMantleSigningService); bifrostErr != nil {
		provider.logger.Warn("failed to sign mantle list-models request: %v", bifrostErr.Error.Message)
		return nil
	}

	resp, err := providerUtils.DoHTTPRequest(provider.client, req)
	if err != nil {
		provider.logger.Warn("mantle list-models request failed: %v", err)
		return nil
	}
	responseBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		provider.logger.Warn("failed to read mantle list-models response: %v", err)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		provider.logger.Warn("mantle list-models returned status %d", resp.StatusCode)
		return nil
	}

	mantleResponse := &openai.OpenAIListModelsResponse{}
	if err := sonic.Unmarshal(responseBody, mantleResponse); err != nil {
		provider.logger.Warn("failed to parse mantle list-models response: %v", err)
		return nil
	}
	return mantleResponse.ToBifrostListModelsResponse(provider.GetProviderKey(), key.Models, key.BlacklistedModels, key.Aliases, unfiltered)
}

func (provider *BedrockProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	providerName := provider.GetProviderKey()
	config := key.BedrockKeyConfig
	region := DefaultBedrockRegion
	if config.Region != nil && config.Region.GetValue() != "" {
		region = config.Region.GetValue()
	}

	// Build query parameters
	params := url.Values{}
	if request.ExtraParams != nil {
		if byCustomizationType, ok := request.ExtraParams["byCustomizationType"].(string); ok && byCustomizationType != "" {
			params.Set("byCustomizationType", byCustomizationType)
		}
		if byInferenceType, ok := request.ExtraParams["byInferenceType"].(string); ok && byInferenceType != "" {
			params.Set("byInferenceType", byInferenceType)
		}
		if byOutputModality, ok := request.ExtraParams["byOutputModality"].(string); ok && byOutputModality != "" {
			params.Set("byOutputModality", byOutputModality)
		}
		if byProvider, ok := request.ExtraParams["byProvider"].(string); ok && byProvider != "" {
			params.Set("byProvider", byProvider)
		}
	}

	// List models endpoint uses the bedrock service (not bedrock-runtime)
	url := fmt.Sprintf("https://%s/foundation-models?%s", resolveBedrockHost(bedrockEndpoints(config), bedrockServiceControlPlane, region), params.Encode())

	// Create the GET request without a body
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error creating request",
				Error:   err,
			},
		}
	}

	// Set any extra headers from network config
	providerUtils.SetExtraHeadersHTTP(ctx, req, provider.networkConfig.ExtraHeaders, nil)

	// If Value is set, use API Key authentication - else use IAM role authentication
	if key.Value.GetValue() != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key.Value.GetValue()))
	} else {
		// Sign the request using either explicit credentials or IAM role authentication

		if err := signAWSRequest(ctx, req, config, region, bedrockSigningService); err != nil {
			return nil, err
		}
	}

	startTime := time.Now()

	// Execute the request
	resp, err := providerUtils.DoHTTPRequest(provider.client, req)
	latency := time.Since(startTime)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Type:    schemas.Ptr(schemas.RequestCancelled),
					Message: schemas.ErrRequestCancelled,
					Error:   err,
				},
			}, latency)
		}
		// Check for timeout first using net.Error before checking net.OpError
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		if errors.Is(err, http.ErrHandlerTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, providerUtils.SetErrorLatency(providerUtils.NewBifrostTimeoutError(schemas.ErrProviderRequestTimedOut, err), latency)
		}
		// Check for DNS lookup and network errors after timeout checks
		var opErr *net.OpError
		var dnsErr *net.DNSError
		if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
			return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
				IsBifrostError: false,
				Error: &schemas.ErrorField{
					Message: schemas.ErrProviderNetworkError,
					Error:   err,
				},
			}, latency)
		}
		return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: schemas.ErrProviderDoRequest,
				Error:   err,
			},
		}, latency)
	}

	// Read response body and close
	responseBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, providerUtils.SetErrorLatency(&schemas.BifrostError{
			IsBifrostError: true,
			Error: &schemas.ErrorField{
				Message: "error reading request",
				Error:   err,
			},
		}, latency)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providerUtils.SetErrorLatency(parseBedrockHTTPError(resp.StatusCode, resp.Header, responseBody), latency)
	}

	// Parse Bedrock-specific response
	bedrockResponse := &BedrockListModelsResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, bedrockResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	// Convert to Bifrost response
	response := bedrockResponse.ToBifrostListModelsResponse(providerName, key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)
	if response == nil {
		return nil, providerUtils.NewBifrostOperationError("failed to convert Bedrock model list response", nil)
	}

	// Merge in the mantle catalog: ListFoundationModels omits the mantle-only models
	// (gpt-5.x, gemma-4, ...) served on the OpenAI-compatible endpoint. Same gating, dedup by id.
	if mantleResponse := provider.listMantleModels(ctx, key, region, request.Unfiltered); mantleResponse != nil {
		seen := make(map[string]struct{}, len(response.Data))
		for _, m := range response.Data {
			seen[m.ID] = struct{}{}
		}
		for _, m := range mantleResponse.Data {
			if _, ok := seen[m.ID]; ok {
				continue
			}
			seen[m.ID] = struct{}{}
			response.Data = append(response.Data, m)
		}
	}

	response.ExtraFields.Latency = time.Since(startTime).Milliseconds()

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

// ListModels performs a list models request to Bedrock's API.
// It retrieves all foundation models available in Amazon Bedrock.
// Requests are made concurrently for improved performance.
func (provider *BedrockProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Bedrock, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
		return nil, err
	}
	return providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
}

// ChatCompletion performs a chat completion request to Bedrock's API.
// OpenAI-family and Gemma 4 models route via the Bedrock Mantle OpenAI-compatible endpoint.
// All other models (including Anthropic/Claude) use the Bedrock Converse API.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *BedrockProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Bedrock, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	if isMantleModel(ctx, request.Model) {
		return provider.mantleChatCompletions(ctx, key, request)
	}

	// Use Bedrock Converse API for all other models
	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToBedrockChatCompletionRequest(ctx, request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	path, _ := provider.getModelPathAndRegion(ctx, "converse", request.Model, key)

	// Create the signed request
	responseBody, latency, providerResponseHeaders, bifrostErr := provider.completeRequest(ctx, jsonData, path, key, request.Model)
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Parse Bedrock Converse API response
	bedrockResponse := acquireBedrockChatResponse()
	defer releaseBedrockChatResponse(bedrockResponse)

	// Parse the response using the new Bedrock type. Timed as the "response-parse"
	// overhead phase, matching HandleProviderResponseCtx on the other completion paths.
	parseTracer, parseHandle := providerUtils.StartResponseParseSpan(ctx)
	if err := sonic.Unmarshal(responseBody, bedrockResponse); err != nil {
		if parseTracer != nil {
			parseTracer.EndSpan(parseHandle, schemas.SpanStatusError, err.Error())
		}
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError("failed to parse bedrock response", err), jsonData, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if parseTracer != nil {
		parseTracer.EndSpan(parseHandle, schemas.SpanStatusOk, "")
	}

	// Convert using the new response converter
	convTracer, convHandle := providerUtils.StartResponseConvertorSpan(ctx)
	bifrostResponse, err := bedrockResponse.ToBifrostChatResponse(ctx, request.Model)
	if err != nil {
		if convTracer != nil {
			convTracer.EndSpan(convHandle, schemas.SpanStatusError, err.Error())
		}
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError("failed to convert bedrock response", err), jsonData, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if convTracer != nil {
		convTracer.EndSpan(convHandle, schemas.SpanStatusOk, "")
	}

	// Override finish reason for structured output (Converse API only)
	if _, ok := ctx.Value(schemas.BifrostContextKeyStructuredOutputToolName).(string); ok {
		if len(bifrostResponse.Choices) > 0 && bifrostResponse.Choices[0].FinishReason != nil {
			if *bifrostResponse.Choices[0].FinishReason == string(schemas.BifrostFinishReasonToolCalls) {
				bifrostResponse.Choices[0].FinishReason = schemas.Ptr(string(schemas.BifrostFinishReasonStop))
			}
		}
	}

	// Set ExtraFields
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	applyDroppedUnsupportedTools(ctx, &bifrostResponse.ExtraFields, provider.logger)

	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		providerUtils.ParseAndSetRawRequest(&bifrostResponse.ExtraFields, jsonData)
	}
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		var rawResponse interface{}
		if err := sonic.Unmarshal(responseBody, &rawResponse); err == nil {
			bifrostResponse.ExtraFields.RawResponse = rawResponse
		}
	}

	return bifrostResponse, nil
}

// applyDroppedUnsupportedTools reads tool-drop info stashed in context during
// request building (ValidateChatToolsForProvider / ValidateResponsesToolsForProvider,
// plus the Nova-model gate in convertToolConfigFromFiltered/ToBedrockResponsesRequest)
// and surfaces it on the response so callers aren't left guessing why a requested
// tool never showed up — mirrors how ProviderResponseHeaders is threaded via context.
func applyDroppedUnsupportedTools(ctx *schemas.BifrostContext, extraFields *schemas.BifrostResponseExtraFields, logger schemas.Logger) {
	dropped, ok := ctx.Value(schemas.BifrostContextKeyDroppedUnsupportedTools).([]string)
	if !ok || len(dropped) == 0 {
		return
	}
	extraFields.DroppedUnsupportedTools = dropped
	logger.Warn(fmt.Sprintf("bedrock: dropped unsupported tools from request: %v", dropped))
}

// normalizeCachedUsage folds the accumulated cached read/write token counts into
// PromptTokens. Bedrock reports TotalTokens directly on the stream, so only the
// prompt counter needs the fold. The accumulator must apply it before billing -
// including on a mid-stream cancel/timeout. The += is not idempotent; callers
// guard with a flag to apply it exactly once.
func normalizeCachedUsage(usage *schemas.BifrostLLMUsage) {
	if usage == nil || usage.PromptTokensDetails == nil {
		return
	}
	usage.PromptTokens += usage.PromptTokensDetails.CachedReadTokens + usage.PromptTokensDetails.CachedWriteTokens
}

func accumulateBedrockResponsesUsage(usage *schemas.ResponsesResponseUsage, billedUsage *schemas.BifrostLLMUsage, usageToProcess *BedrockTokenUsage) {
	if usage == nil || usageToProcess == nil {
		return
	}
	if usageToProcess.InputTokens > usage.InputTokens {
		usage.InputTokens = usageToProcess.InputTokens
		if billedUsage != nil {
			billedUsage.PromptTokens = usageToProcess.InputTokens
		}
	}
	if usageToProcess.OutputTokens > usage.OutputTokens {
		usage.OutputTokens = usageToProcess.OutputTokens
		if billedUsage != nil {
			billedUsage.CompletionTokens = usageToProcess.OutputTokens
		}
	}
	if usageToProcess.TotalTokens > usage.TotalTokens {
		usage.TotalTokens = usageToProcess.TotalTokens
		if billedUsage != nil {
			billedUsage.TotalTokens = usageToProcess.TotalTokens
		}
	}
	if usageToProcess.CacheReadInputTokens > 0 {
		if usage.InputTokensDetails == nil {
			usage.InputTokensDetails = &schemas.ResponsesResponseInputTokens{}
		}
		if billedUsage != nil && billedUsage.PromptTokensDetails == nil {
			billedUsage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{}
		}
		if usageToProcess.CacheReadInputTokens > usage.InputTokensDetails.CachedReadTokens {
			usage.InputTokensDetails.CachedReadTokens = usageToProcess.CacheReadInputTokens
			if billedUsage != nil {
				billedUsage.PromptTokensDetails.CachedReadTokens = usageToProcess.CacheReadInputTokens
			}
		}
	}
	if usageToProcess.CacheWriteInputTokens > 0 {
		if usage.InputTokensDetails == nil {
			usage.InputTokensDetails = &schemas.ResponsesResponseInputTokens{}
		}
		if billedUsage != nil && billedUsage.PromptTokensDetails == nil {
			billedUsage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{}
		}
		if usageToProcess.CacheWriteInputTokens > usage.InputTokensDetails.CachedWriteTokens {
			usage.InputTokensDetails.CachedWriteTokens = usageToProcess.CacheWriteInputTokens
			if billedUsage != nil {
				billedUsage.PromptTokensDetails.CachedWriteTokens = usageToProcess.CacheWriteInputTokens
			}
		}
		if usageToProcess.CacheDetails != nil {
			if usage.InputTokensDetails.CachedWriteTokenDetails == nil {
				usage.InputTokensDetails.CachedWriteTokenDetails = &schemas.ChatCachedWriteTokenDetails{}
			}
			if billedUsage != nil && billedUsage.PromptTokensDetails.CachedWriteTokenDetails == nil {
				billedUsage.PromptTokensDetails.CachedWriteTokenDetails = &schemas.ChatCachedWriteTokenDetails{}
			}
			for _, cacheDetail := range *usageToProcess.CacheDetails {
				if cacheDetail.TTL == BedrockCacheWriteTTL5m {
					usage.InputTokensDetails.CachedWriteTokenDetails.CachedWriteTokens5m = cacheDetail.InputTokens
					if billedUsage != nil {
						billedUsage.PromptTokensDetails.CachedWriteTokenDetails.CachedWriteTokens5m = cacheDetail.InputTokens
					}
				}
				if cacheDetail.TTL == BedrockCacheWriteTTL1h {
					usage.InputTokensDetails.CachedWriteTokenDetails.CachedWriteTokens1h = cacheDetail.InputTokens
					if billedUsage != nil {
						billedUsage.PromptTokensDetails.CachedWriteTokenDetails.CachedWriteTokens1h = cacheDetail.InputTokens
					}
				}
			}
		}
	}
}

// ChatCompletionStream performs a streaming chat completion request to Bedrock's API.
// OpenAI-family and Gemma 4 models route via the Bedrock Mantle OpenAI-compatible endpoint.
// All other models (including Anthropic/Claude) use the Bedrock Converse streaming API.
// Returns a channel for streaming BifrostStreamChunk objects or an error if the request fails.
func (provider *BedrockProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Bedrock, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}

	if isMantleModel(ctx, request.Model) {
		return provider.mantleChatCompletionsStream(ctx, postHookRunner, postHookSpanFinalizer, key, request)
	}

	// Use Bedrock Converse streaming API for all other models
	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToBedrockChatCompletionRequest(ctx, request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	startTime := time.Now()

	resp, bifrostErr := provider.makeStreamingRequest(ctx, jsonData, key, request.Model, "converse-stream")
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse)
	}

	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeadersFromHTTP(resp))

	// Create response channel
	responseChan := make(chan *schemas.BifrostStreamChunk, schemas.DefaultStreamBufferSize)

	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, provider.networkConfig.StreamIdleTimeoutInSeconds)
	// Start streaming in a goroutine
	go func() {
		defer providerUtils.EnsureStreamFinalizerCalled(ctx, postHookSpanFinalizer)
		defer func() {
			if ctx.Err() == context.Canceled {
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonData)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonData)
			}
			providerUtils.CloseStream(ctx, responseChan)
		}()
		defer resp.Body.Close()

		// Wrap body with idle timeout to detect stalled streams.
		idleReader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(resp.Body, resp.Body, providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close body stream on ctx cancellation
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.Body, provider.logger)
		defer stopCancellation()

		// Process AWS Event Stream format
		usage := &schemas.BifrostLLMUsage{}
		// Register the accumulating usage handle so a mid-stream
		// cancel/timeout can bill for tokens the provider already processed.
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, usage)

		// Fold cached tokens into PromptTokens exactly once at stream end. The EOF
		// path calls normalizeUsage() after the loop; on a mid-stream cancel/timeout
		// the deferred call below runs first (LIFO, registered after the cancellation
		// handler at the top of the goroutine) so HandleStreamCancellation/Timeout
		// bills the normalized totals.
		usageNormalized := false
		normalizeUsage := func() {
			if usageNormalized {
				return
			}
			usageNormalized = true
			normalizeCachedUsage(usage)
		}
		defer func() {
			if ctx.Err() != nil {
				normalizeUsage()
			}
		}()
		var finishReason *string
		chunkIndex := 0

		// Process AWS Event Stream format using proper decoder
		lastChunkTime := startTime
		decoder := eventstream.NewDecoder()
		payloadBuf := make([]byte, 0, 1024*1024) // 1MB payload buffer

		// Bedrock does not provide a unique identifier for the stream, so we generate one ourselves
		id := uuid.New().String()

		// Check for structured output mode - if set, we need to intercept tool calls
		// and convert them to content instead of forwarding as tool calls
		var structuredOutputToolName string
		if toolName, ok := ctx.Value(schemas.BifrostContextKeyStructuredOutputToolName).(string); ok {
			structuredOutputToolName = toolName
		}

		streamState := NewBedrockStreamStateWithContext(ctx)
		var isAccumulatingStructuredOutput bool
		var structuredOutputBuilder strings.Builder

		for {
			if ctx.Err() != nil {
				return
			}
			// Decode a single EventStream message
			message, err := decoder.Decode(idleReader, payloadBuf)
			if err != nil {
				// If context was cancelled/timed out, let defer handle it
				if ctx.Err() != nil {
					return
				}
				// End of stream - this is normal
				if err == io.EOF {
					break
				}
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				provider.logger.Warn("Error decoding EventStream message: %v", err)
				// Transport-level errors (stale/closed connection, unexpected EOF) are retryable.
				// Use IsBifrostError:false so the retry gate in executeRequestWithRetries can retry.
				if isStreamTransportError(err) {
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, &schemas.BifrostError{
						IsBifrostError: false,
						Error: &schemas.ErrorField{
							Message: schemas.ErrProviderNetworkError,
							Error:   err,
						},
					}, responseChan, provider.logger, postHookSpanFinalizer)
				} else {
					providerUtils.ProcessAndSendError(ctx, postHookRunner, err, responseChan, provider.logger, postHookSpanFinalizer)
				}
				return
			}

			// Process the decoded message payload (contains JSON for normal events)
			if len(message.Payload) > 0 {
				if msgTypeHeader := message.Headers.Get(":message-type"); msgTypeHeader != nil {
					if msgType := msgTypeHeader.String(); msgType != "event" {
						excType := msgType
						if excHeader := message.Headers.Get(":exception-type"); excHeader != nil {
							if v := excHeader.String(); v != "" {
								excType = v
							}
						}
						streamErr := newBedrockStreamException("", excType, message.Payload)
						providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, streamErr, responseChan, provider.logger, postHookSpanFinalizer)
						return
					}
				}

				// Converse API path: parse Bedrock Converse-specific stream events.
				// Per-event decode -> "response-parse" (Serialization) stream phase.
				var streamEvent BedrockStreamEvent
				parseStart := time.Now()
				umErr := sonic.Unmarshal(message.Payload, &streamEvent)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if umErr != nil {
					provider.logger.Debug("Failed to parse JSON from event buffer: %v, data: %s", umErr, string(message.Payload))
					providerUtils.ProcessAndSendError(ctx, postHookRunner, umErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}

				if streamEvent.Usage != nil {
					// Accumulate usage information instead of overwriting
					// In some cases usage comes in multiple events, so we need to take the maximum values
					if streamEvent.Usage.InputTokens > usage.PromptTokens {
						usage.PromptTokens = streamEvent.Usage.InputTokens
					}
					if streamEvent.Usage.OutputTokens > usage.CompletionTokens {
						usage.CompletionTokens = streamEvent.Usage.OutputTokens
					}
					if streamEvent.Usage.TotalTokens > usage.TotalTokens {
						usage.TotalTokens = streamEvent.Usage.TotalTokens
					}
					// Handle cached tokens if present
					if streamEvent.Usage.CacheReadInputTokens > 0 {
						if usage.PromptTokensDetails == nil {
							usage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{}
						}
						if streamEvent.Usage.CacheReadInputTokens > usage.PromptTokensDetails.CachedReadTokens {
							usage.PromptTokensDetails.CachedReadTokens = streamEvent.Usage.CacheReadInputTokens
						}
					}
					if streamEvent.Usage.CacheWriteInputTokens > 0 {
						if usage.PromptTokensDetails == nil {
							usage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{}
						}
						if streamEvent.Usage.CacheWriteInputTokens > usage.PromptTokensDetails.CachedWriteTokens {
							usage.PromptTokensDetails.CachedWriteTokens = streamEvent.Usage.CacheWriteInputTokens
						}
						if streamEvent.Usage.CacheDetails != nil {
							if usage.PromptTokensDetails.CachedWriteTokenDetails == nil {
								usage.PromptTokensDetails.CachedWriteTokenDetails = &schemas.ChatCachedWriteTokenDetails{}
							}
							for _, cacheDetail := range *streamEvent.Usage.CacheDetails {
								if cacheDetail.TTL == BedrockCacheWriteTTL5m {
									usage.PromptTokensDetails.CachedWriteTokenDetails.CachedWriteTokens5m = cacheDetail.InputTokens
								}
								if cacheDetail.TTL == BedrockCacheWriteTTL1h {
									usage.PromptTokensDetails.CachedWriteTokenDetails.CachedWriteTokens1h = cacheDetail.InputTokens
								}
							}
						}
					}
				}

				if streamEvent.StopReason != nil {
					finishReason = schemas.Ptr(anthropic.ConvertAnthropicFinishReasonToBifrost(anthropic.AnthropicStopReason(*streamEvent.StopReason)))

					// Override finish reason for structured output
					// When structured output is used, tool_use stop reason should appear as "stop" to the client
					if structuredOutputToolName != "" && *finishReason == string(schemas.BifrostFinishReasonToolCalls) {
						finishReason = schemas.Ptr(string(schemas.BifrostFinishReasonStop))
					}
				}

				// Handle structured output: intercept tool calls for the structured output tool
				// and convert them to content instead of forwarding as tool calls
				if structuredOutputToolName != "" {
					// Check for tool use start event
					if streamEvent.Start != nil && streamEvent.Start.ToolUse != nil {
						if streamEvent.Start.ToolUse.Name == structuredOutputToolName {
							// This is the structured output tool - start accumulating, don't forward
							isAccumulatingStructuredOutput = true
							continue
						}
					}

					// Check for tool use delta event
					if streamEvent.Delta != nil && streamEvent.Delta.ToolUse != nil && isAccumulatingStructuredOutput {
						// Accumulate the input for tracking purposes
						structuredOutputBuilder.WriteString(streamEvent.Delta.ToolUse.Input)

						// Convert tool use delta to content delta
						content := streamEvent.Delta.ToolUse.Input
						response := &schemas.BifrostChatResponse{
							ID:     id,
							Model:  request.Model,
							Object: "chat.completion.chunk",
							Choices: []schemas.BifrostResponseChoice{
								{
									Index: 0,
									ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
										Delta: &schemas.ChatStreamResponseChoiceDelta{
											Content: &content,
										},
									},
								},
							},
							ExtraFields: schemas.BifrostResponseExtraFields{
								ChunkIndex: chunkIndex,
								Latency:    time.Since(lastChunkTime).Milliseconds(),
							},
						}
						chunkIndex++
						lastChunkTime = time.Now()

						if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
							response.ExtraFields.RawResponse = string(message.Payload)
						}

						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
						continue
					}

					// Suppress non-tool content events that would leak into the
					// assembled structured output (mirrors ResponsesStreamRequest).
					if streamEvent.Delta != nil && (streamEvent.Delta.Text != nil || streamEvent.Delta.ReasoningContent != nil) {
						continue
					}
					if streamEvent.Start != nil && streamEvent.Start.ToolUse == nil {
						continue // non-tool content-block start (text block) — drop
					}
				}

				// Per-event mapping -> "convertor" (Convertor) stream phase.
				convStart := time.Now()
				response, bifrostErr, _ := streamEvent.ToBifrostChatCompletionStream(streamState)
				schemas.AddStreamConvert(ctx, time.Since(convStart))
				if bifrostErr != nil {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, bifrostErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}
				if response != nil {
					response.ID = id
					response.Model = request.Model
					response.ExtraFields = schemas.BifrostResponseExtraFields{
						ChunkIndex: chunkIndex,
						Latency:    time.Since(lastChunkTime).Milliseconds(),
					}
					chunkIndex++
					lastChunkTime = time.Now()

					if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
						response.ExtraFields.RawResponse = string(message.Payload)
					}

					providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
				}
			}
		}

		normalizeUsage()

		// Send final chunk with accumulated usage
		response := providerUtils.CreateBifrostChatCompletionChunkResponse(id, usage, finishReason, chunkIndex, request.Model, 0)
		// Set raw request if enabled
		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			providerUtils.ParseAndSetRawRequest(&response.ExtraFields, jsonData)
		}
		response.ExtraFields.Latency = time.Since(startTime).Milliseconds()
		ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
		providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, response, nil, nil, nil, nil), responseChan, postHookSpanFinalizer)
	}()

	return responseChan, nil
}

// Responses performs a responses request to Bedrock's API.
// OpenAI-family and Gemma 4 models route via the Bedrock Mantle OpenAI-compatible endpoint.
// All other models (including Anthropic/Claude) use the Bedrock Converse API.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *BedrockProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Bedrock, provider.customProviderConfig, schemas.ResponsesRequest); err != nil {
		return nil, err
	}

	if isMantleModel(ctx, request.Model) {
		return provider.mantleResponses(ctx, key, request)
	}

	// Use Bedrock Converse API for all other models
	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToBedrockResponsesRequest(ctx, request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	path, _ := provider.getModelPathAndRegion(ctx, "converse", request.Model, key)

	// Create the signed request
	responseBody, latency, providerResponseHeaders, bifrostErr := provider.completeRequest(ctx, jsonData, path, key, request.Model)
	if providerResponseHeaders != nil {
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)
	}
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	// Parse Bedrock Converse API response
	bedrockResponse := acquireBedrockChatResponse()
	defer releaseBedrockChatResponse(bedrockResponse)

	// Parse the response using the new Bedrock type. Timed as the "response-parse"
	// overhead phase, matching HandleProviderResponseCtx on the other completion paths.
	parseTracer, parseHandle := providerUtils.StartResponseParseSpan(ctx)
	if err := sonic.Unmarshal(responseBody, bedrockResponse); err != nil {
		if parseTracer != nil {
			parseTracer.EndSpan(parseHandle, schemas.SpanStatusError, err.Error())
		}
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError("failed to parse bedrock response", err), jsonData, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if parseTracer != nil {
		parseTracer.EndSpan(parseHandle, schemas.SpanStatusOk, "")
	}

	// Convert using the new response converter
	bifrostResponse, err := bedrockResponse.ToBifrostResponsesResponse(ctx)
	if err != nil {
		return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError("failed to convert bedrock response", err), jsonData, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	bifrostResponse.Model = request.Model

	// Set ExtraFields
	bifrostResponse.ExtraFields.Latency = latency.Milliseconds()
	bifrostResponse.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	applyDroppedUnsupportedTools(ctx, &bifrostResponse.ExtraFields, provider.logger)

	// Set raw request if enabled
	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		providerUtils.ParseAndSetRawRequest(&bifrostResponse.ExtraFields, jsonData)
	}

	// Set raw response if enabled
	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		var rawResponse interface{}
		if err := sonic.Unmarshal(responseBody, &rawResponse); err == nil {
			bifrostResponse.ExtraFields.RawResponse = rawResponse
		}
	}

	return bifrostResponse, nil
}

// ResponsesStream performs a streaming chat completion request to Bedrock's API.
// It formats the request, sends it to Bedrock, and processes the streaming response.
// Returns a channel for streaming BifrostResponse objects or an error if the request fails.
func (provider *BedrockProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.Bedrock, provider.customProviderConfig, schemas.ResponsesStreamRequest); err != nil {
		return nil, err
	}

	if isMantleModel(ctx, request.Model) {
		return provider.mantleResponsesStream(ctx, postHookRunner, postHookSpanFinalizer, key, request)
	}

	// Use Bedrock Converse streaming API for all other models
	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToBedrockResponsesRequest(ctx, request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	startTime := time.Now()

	resp, bifrostErr := provider.makeStreamingRequest(ctx, jsonData, key, request.Model, "converse-stream")
	latency := time.Since(startTime)
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeadersFromHTTP(resp))

	// Create response channel
	responseChan := make(chan *schemas.BifrostStreamChunk, schemas.DefaultStreamBufferSize)

	providerUtils.SetStreamIdleTimeoutIfEmpty(ctx, provider.networkConfig.StreamIdleTimeoutInSeconds)

	// Start streaming in a goroutine
	go func() {
		defer providerUtils.EnsureStreamFinalizerCalled(ctx, postHookSpanFinalizer)
		defer func() {
			if ctx.Err() == context.Canceled {
				providerUtils.HandleStreamCancellation(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonData)
			} else if ctx.Err() == context.DeadlineExceeded {
				providerUtils.HandleStreamTimeout(ctx, postHookRunner, responseChan, provider.logger, postHookSpanFinalizer, jsonData)
			}
			providerUtils.CloseStream(ctx, responseChan)
		}()
		// Always release response on exit; bodyStream close should prevent indefinite blocking.
		defer resp.Body.Close()

		// Wrap body with idle timeout to detect stalled streams.
		idleReader, stopIdleTimeout := providerUtils.NewIdleTimeoutReader(resp.Body, resp.Body, providerUtils.GetStreamIdleTimeout(ctx), ctx)
		defer stopIdleTimeout()

		// Setup cancellation handler to close body stream on ctx cancellation
		stopCancellation := providerUtils.SetupStreamCancellation(ctx, resp.Body, provider.logger)
		defer stopCancellation()

		// Process AWS Event Stream format
		usage := &schemas.ResponsesResponseUsage{}
		billedUsage := &schemas.BifrostLLMUsage{}
		// Register the accumulating usage handle so a mid-stream cancel/timeout
		// can bill for Bedrock Responses usage already reported by stream events
		// before the stream was interrupted.
		ctx.SetValue(schemas.BifrostContextKeyStreamAccumulatedUsage, billedUsage)

		usageNormalized := false
		normalizeUsage := func() {
			if usageNormalized {
				return
			}
			usageNormalized = true
			normalizeCachedUsage(billedUsage)
		}
		defer func() {
			if ctx.Err() != nil {
				normalizeUsage()
			}
		}()

		var streamTrace *BedrockConverseTrace
		chunkIndex := 0

		// Create stream state for stateful conversions (used by Converse API path)
		streamState := acquireBedrockResponsesStreamState()
		streamState.Model = &request.Model
		streamState.Ctx = ctx
		defer releaseBedrockResponsesStreamState(streamState)

		// Check for structured output mode - if set, we need to intercept tool calls
		// and convert them to content instead of forwarding as tool calls
		var structuredOutputToolName string
		if toolName, ok := ctx.Value(schemas.BifrostContextKeyStructuredOutputToolName).(string); ok {
			structuredOutputToolName = toolName
		}
		var isAccumulatingStructuredOutput bool

		// Process AWS Event Stream format using proper decoder
		lastChunkTime := startTime
		decoder := eventstream.NewDecoder()
		payloadBuf := make([]byte, 0, 1024*1024) // 1MB payload buffer
		for {
			// If context was cancelled/timed out, let defer handle it
			if ctx.Err() != nil {
				return
			}
			// Decode a single EventStream message
			message, err := decoder.Decode(idleReader, payloadBuf)
			if err != nil {
				// If context was cancelled/timed out, let defer handle it
				if ctx.Err() != nil {
					return
				}
				if err == io.EOF {
					// Converse API: finalize any open items at end of stream.
					finalResponses := FinalizeBedrockStream(streamState, chunkIndex, usage, streamTrace)
					for i, finalResponse := range finalResponses {
						finalResponse.ExtraFields = schemas.BifrostResponseExtraFields{
							ChunkIndex: chunkIndex,
							Latency:    time.Since(lastChunkTime).Milliseconds(),
						}
						chunkIndex++
						lastChunkTime = time.Now()

						if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
							finalResponse.ExtraFields.RawResponse = "{}" // Final event has no payload
						}

						if i == len(finalResponses)-1 {
							// Set raw request if enabled
							ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
							if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
								providerUtils.ParseAndSetRawRequest(&finalResponse.ExtraFields, jsonData)
							}
							finalResponse.ExtraFields.Latency = time.Since(startTime).Milliseconds()
						}

						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, finalResponse, nil, nil, nil), responseChan, postHookSpanFinalizer)
					}
					break
				}
				ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
				provider.logger.Warn("Error decoding EventStream message: %v", err)
				// Transport-level errors (stale/closed connection, unexpected EOF) are retryable.
				// Use IsBifrostError:false so the retry gate in executeRequestWithRetries can retry.
				if isStreamTransportError(err) {
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, &schemas.BifrostError{
						IsBifrostError: false,
						Error: &schemas.ErrorField{
							Message: schemas.ErrProviderNetworkError,
							Error:   err,
						},
					}, responseChan, provider.logger, postHookSpanFinalizer)
				} else {
					providerUtils.ProcessAndSendError(ctx, postHookRunner, err, responseChan, provider.logger, postHookSpanFinalizer)
				}
				return
			}

			// Process the decoded message payload (contains JSON for normal events)
			if len(message.Payload) > 0 {
				if msgTypeHeader := message.Headers.Get(":message-type"); msgTypeHeader != nil {
					if msgType := msgTypeHeader.String(); msgType != "event" {
						excType := msgType
						if excHeader := message.Headers.Get(":exception-type"); excHeader != nil {
							if v := excHeader.String(); v != "" {
								excType = v
							}
						}
						streamErr := newBedrockStreamException("", excType, message.Payload)
						providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, streamErr, responseChan, provider.logger, postHookSpanFinalizer)
						return
					}
				}

				// Converse API path: parse Bedrock Converse-specific stream events.
				// Per-event decode -> "response-parse" (Serialization) stream phase.
				var streamEvent BedrockStreamEvent
				parseStart := time.Now()
				umErr := sonic.Unmarshal(message.Payload, &streamEvent)
				schemas.AddStreamParse(ctx, time.Since(parseStart))
				if umErr != nil {
					provider.logger.Debug("Failed to parse JSON from event buffer: %v, data: %s", umErr, string(message.Payload))
					providerUtils.ProcessAndSendError(ctx, postHookRunner, umErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}

				if streamEvent.Trace != nil {
					streamTrace = streamEvent.Trace
				}

				if streamEvent.Usage != nil {
					// Accumulate usage information instead of overwriting
					// In some cases usage comes in multiple events, so we need to take the maximum values
					accumulateBedrockResponsesUsage(usage, billedUsage, streamEvent.Usage)
				}

				// Handle structured output: intercept tool calls for the structured output tool
				// and convert them to content instead of forwarding as tool calls
				if structuredOutputToolName != "" {
					// Check for tool use start event
					if streamEvent.Start != nil && streamEvent.Start.ToolUse != nil {
						if streamEvent.Start.ToolUse.Name == structuredOutputToolName {
							// This is the structured output tool - start accumulating, don't forward
							isAccumulatingStructuredOutput = true
							streamState.UsedStructuredOutputTool = true
							continue
						}
					}

					// Check for tool use delta event
					if streamEvent.Delta != nil && streamEvent.Delta.ToolUse != nil && isAccumulatingStructuredOutput {
						// Convert tool use delta to text delta
						content := streamEvent.Delta.ToolUse.Input
						response := &schemas.BifrostResponsesStreamResponse{
							Type:           schemas.ResponsesStreamResponseTypeOutputTextDelta,
							SequenceNumber: chunkIndex,
							Delta:          &content,
							ExtraFields: schemas.BifrostResponseExtraFields{
								ChunkIndex: chunkIndex,
								Latency:    time.Since(lastChunkTime).Milliseconds(),
							},
						}
						chunkIndex++
						lastChunkTime = time.Now()

						if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
							response.ExtraFields.RawResponse = string(message.Payload)
						}

						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
						continue
					}

					// Suppress non-tool content events that would leak into the
					// assembled structured output. Bedrock Claude can emit prose
					// alongside the forced tool call (markdown, preambles, reasoning
					// blocks); forwarding those as text deltas corrupts the JSON
					// the client assembles from the structured-output stream.
					if streamEvent.Delta != nil && (streamEvent.Delta.Text != nil || streamEvent.Delta.ReasoningContent != nil) {
						continue
					}
					if streamEvent.Start != nil && streamEvent.Start.ToolUse == nil {
						continue // non-tool content-block start (text block) — drop
					}
				}

				// Per-event mapping -> "convertor" (Convertor) stream phase.
				convStart := time.Now()
				responses, bifrostErr, _ := streamEvent.ToBifrostResponsesStream(chunkIndex, streamState)
				schemas.AddStreamConvert(ctx, time.Since(convStart))
				if bifrostErr != nil {
					ctx.SetValue(schemas.BifrostContextKeyStreamEndIndicator, true)
					providerUtils.ProcessAndSendBifrostError(ctx, postHookRunner, bifrostErr, responseChan, provider.logger, postHookSpanFinalizer)
					return
				}
				for _, response := range responses {
					if response != nil {
						response.ExtraFields = schemas.BifrostResponseExtraFields{
							ChunkIndex: chunkIndex,
							Latency:    time.Since(lastChunkTime).Milliseconds(),
						}
						chunkIndex++
						lastChunkTime = time.Now()

						if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
							response.ExtraFields.RawResponse = string(message.Payload)
						}

						providerUtils.ProcessAndSendResponse(ctx, postHookRunner, providerUtils.GetBifrostResponseForStreamResponse(nil, nil, response, nil, nil, nil), responseChan, postHookSpanFinalizer)
					}
				}
			}
		}
	}()

	return responseChan, nil
}
