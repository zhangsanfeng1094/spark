package vertex

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/bytedance/sonic"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/providers/gemini"
	"github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/gjson"
)

type VertexError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// vertexTokenSourcePool caches oauth2.TokenSource instances keyed by a hash of
// the auth credentials. The Google TokenSource internally handles token refresh
// and expiry, so caching the source avoids re-parsing credentials JSON and
// re-creating the credentials object on every request.
// Entries are evicted by removeVertexClient on 401/403 or token-acquisition errors.
var vertexTokenSourcePool sync.Map

// vertexLocationsPathRe matches /locations/{region} in Vertex API paths for region replacement.
var vertexLocationsPathRe = regexp.MustCompile(`/locations/[^/]+`)

var vertexProjectsPathRe = regexp.MustCompile(`/projects/[^/]+`)

// vertexBodyProjectsRe matches projects/{project} in body JSON values,
// where the path may appear as "projects/X (after a JSON quote) or /projects/X (mid-path).
var vertexBodyProjectsRe = regexp.MustCompile(`(["/])projects/[^/"]+`)

// vertexShortModelRe matches short-form model names like "models/X" in JSON bodies
// that need expanding to the full Vertex resource path.
var vertexShortModelRe = regexp.MustCompile(`"(models/[^/"]+)"`)

// defaultCredentialsCacheKey is the sentinel pool key used when AuthCredentials
// is empty and we fall back to google.FindDefaultCredentials.
const defaultCredentialsCacheKey = "__default_credentials__"

// geminiImageURLSchemes is the image URL scheme allowlist Vertex applies when it
// routes a request through the Gemini converter. Vertex natively accepts gs://
// FileData URIs (in addition to http(s)), so we extend the Gemini-default list
// with "gs".
var geminiImageURLSchemes = []string{"http", "https", "gs"}

// urlSourceDisposition says what to do with one URL-sourced image or document
// before it is handed to a Vertex converter. The right answer is a function of
// both the scheme and the target model family, not the scheme alone -- see
// classifyURLSource.
type urlSourceDisposition int

const (
	// urlSourceForward hands the reference to the converter untouched.
	urlSourceForward urlSourceDisposition = iota
	// urlSourceFetchHTTP downloads over http(s) via providerUtils.FetchAndEncodeURL.
	urlSourceFetchHTTP
	// urlSourceFetchGCS downloads from Cloud Storage with the request key's Google
	// credentials and inlines the bytes.
	urlSourceFetchGCS
)

// classifyURLSource decides how a single URL source reaches Vertex.
//
// The scheme alone is not enough: the two model families served by this provider accept
// different source types, so the same URL is forwarded for one and downloaded for the
// other.
//
// http(s) -- always fetched, for both families. Vertex's FileData is documented as Cloud
// Storage only ("fileUri: Required. The URI of the file in Google Cloud Storage" -- the v1
// aiplatform discovery document), so an https URI has no supported form here. Forwarding
// one was measured against harness 47.10 and Vertex rejected all six endpoint shapes with
// "Cannot fetch content from the provided URL ... Status:
// URL_REJECTED-REJECTED_FC_TOO_MANY_PENDING" after ~59s each: the server-side crawler is
// undocumented best-effort and saturates. Inlining costs a download but is deterministic.
//
// gs:// -- forwarded for Gemini/Gemma as fileData.fileUri. This is the documented form, it
// resolves under the caller's own project IAM with no crawler involved, and it sidesteps
// the 25 MiB inline cap, which is what makes multi-hundred-MB video inputs viable at all.
//
// gs:// -- fetched from Cloud Storage for the Anthropic family, using the request key's own
// Google credentials. Anthropic lists "Input sources (URL sources for images and documents,
// Files API)" under "Features not supported" for Claude on Google Cloud, and the vision and
// PDF-support guides both state that "On Amazon Bedrock and Google Cloud, only
// base64-encoded sources are currently available". No URL form reaches Claude-on-Vertex, so
// forwarding the URI would be a guaranteed 400.
//
// data: -- forwarded. The Gemini converter turns it into an inlineData blob, and the
// Anthropic path already expects a data URI.
//
// Anything else -- s3://, scheme-less, unparseable -- is unsupported for both families.
// Vertex cannot resolve it and Bifrost has no credentials to fetch it: AWS credentials
// live only on BedrockKeyConfig, and core cannot reach framework/objectstore without a
// module cycle. Callers should presign to https instead.
func classifyURLSource(rawURL string, isAnthropicFamily bool) urlSourceDisposition {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return urlSourceForward
	}
	switch parsed.Scheme {
	case "http", "https":
		return urlSourceFetchHTTP
	case "data":
		return urlSourceForward
	case "gs":
		if isAnthropicFamily {
			return urlSourceFetchGCS
		}
		return urlSourceForward
	default:
		return urlSourceForward
	}
}

// getClientKey generates a unique key for caching token sources.
// It uses a hash of the auth credentials for security.
func getClientKey(authCredentials string) string {
	if authCredentials == "" {
		return defaultCredentialsCacheKey
	}
	hash := sha256.Sum256([]byte(authCredentials))
	return hex.EncodeToString(hash[:])
}

// removeVertexClient evicts a cached token source from the pool.
// This should be called when:
// - API returns authentication/authorization errors (401, 403)
// - Token acquisition fails (tokenSource.Token() error)
// This forces the next request to re-create the token source from scratch.
func removeVertexClient(authCredentials string) {
	clientKey := getClientKey(authCredentials)
	vertexTokenSourcePool.Delete(clientKey)
}

// VertexProvider implements the Provider interface for Google's Vertex AI API.
type VertexProvider struct {
	logger              schemas.Logger        // Logger for provider operations
	client              *fasthttp.Client      // HTTP client for unary API requests (ReadTimeout bounds overall response)
	streamingClient     *fasthttp.Client      // HTTP client for streaming API requests (no ReadTimeout; idle governed by NewIdleTimeoutReader)
	networkConfig       schemas.NetworkConfig // Network configuration including extra headers
	sendBackRawRequest  bool                  // Whether to include raw request in BifrostResponse
	sendBackRawResponse bool                  // Whether to include raw response in BifrostResponse
}

// NewVertexProvider creates a new Vertex provider instance.
// It initializes the HTTP client with the provided configuration and sets up response pools.
// The client is configured with timeouts, concurrency limits, and optional proxy settings.
func NewVertexProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*VertexProvider, error) {
	config.CheckAndSetDefaults()
	requestTimeout := time.Second * time.Duration(config.NetworkConfig.DefaultRequestTimeoutInSeconds)
	client := &fasthttp.Client{
		ReadTimeout:            requestTimeout,
		WriteTimeout:           requestTimeout,
		MaxConnsPerHost:        config.NetworkConfig.MaxConnsPerHost,
		MaxIdleConnDuration:    time.Second * time.Duration(config.NetworkConfig.KeepAliveTimeoutInSeconds),
		MaxConnWaitTimeout:     requestTimeout,
		MaxConnDuration:        time.Second * time.Duration(schemas.DefaultMaxConnDurationInSeconds),
		ConnPoolStrategy:       fasthttp.FIFO,
		DisablePathNormalizing: true,
	}
	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client, config.NetworkConfig.AllowPrivateNetwork)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)
	return &VertexProvider{
		logger:              logger,
		client:              client,
		streamingClient:     streamingClient,
		networkConfig:       config.NetworkConfig,
		sendBackRawRequest:  config.SendBackRawRequest,
		sendBackRawResponse: config.SendBackRawResponse,
	}, nil
}

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// getAuthTokenSource returns an authenticated token source for Vertex AI API requests.
// Token sources are cached in vertexTokenSourcePool keyed by a hash of the auth
// credentials. The Google oauth2.TokenSource handles token refresh and expiry
// internally, so caching the source is safe and avoids re-parsing credentials
// on every request.
func getAuthTokenSource(key schemas.Key) (oauth2.TokenSource, error) {
	authCredentials := key.VertexKeyConfig.AuthCredentials
	clientKey := getClientKey(authCredentials.GetValue())

	// Fast path: return cached token source.
	if cached, ok := vertexTokenSourcePool.Load(clientKey); ok {
		return cached.(oauth2.TokenSource), nil
	}

	// Slow path: create a new token source and cache it.
	var tokenSource oauth2.TokenSource
	if authCredentials.GetValue() == "" {
		creds, err := google.FindDefaultCredentials(context.Background(), cloudPlatformScope)
		if err != nil {
			return nil, fmt.Errorf("failed to find default credentials in environment: %w", err)
		}
		tokenSource = creds.TokenSource
	} else {
		jsonData := []byte(authCredentials.GetValue())

		// Peek at the JSON to detect the "type" field
		var meta struct {
			Type string `json:"type"`
		}
		if err := sonic.Unmarshal(jsonData, &meta); err != nil {
			return nil, fmt.Errorf("failed to parse auth credentials JSON: %w", err)
		}

		// Map string to google.CredentialsType with a security whitelist
		var credType google.CredentialsType
		switch meta.Type {
		case string(google.ServiceAccount):
			credType = google.ServiceAccount
		case string(google.ImpersonatedServiceAccount):
			credType = google.ImpersonatedServiceAccount
		case string(google.AuthorizedUser):
			credType = google.AuthorizedUser
		case string(google.ExternalAccount):
			credType = google.ExternalAccount
		case string(google.ExternalAccountAuthorizedUser):
			credType = google.ExternalAccountAuthorizedUser
		case "":
			return nil, fmt.Errorf("invalid google auth credentials: missing 'type'")
		default:
			return nil, fmt.Errorf("unsupported or restricted credential type: %s", meta.Type)
		}

		conf, err := google.CredentialsFromJSONWithType(context.Background(), jsonData, credType, cloudPlatformScope)
		if err != nil {
			return nil, fmt.Errorf("failed to create credentials from auth credentials JSON: %w", err)
		}
		tokenSource = conf.TokenSource
	}

	// Cache the token source. If another goroutine raced and stored first, use
	// that one — both are equally valid, but sharing maximises token reuse.
	actual, _ := vertexTokenSourcePool.LoadOrStore(clientKey, tokenSource)
	return actual.(oauth2.TokenSource), nil
}

// GetProviderKey returns the provider identifier for Vertex.
func (provider *VertexProvider) GetProviderKey() schemas.ModelProvider {
	return schemas.Vertex
}

// listModelsByKey performs a list models request for a single key.
// Returns the response and latency, or an error if the request fails.
//
// The logic is:
// 1. If deployments or allowedModels are configured, return those (no API call needed)
// 2. Otherwise, fetch from the publishers.models.list API endpoint (Model Garden)
func (provider *VertexProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	region := resolveVertexRegion(ctx, key)
	if region == "" {
		return nil, providerUtils.NewConfigurationError("region is not set in key config")
	}

	deployments := key.Aliases
	allowedModels := key.Models

	if !request.Unfiltered && (allowedModels.IsEmpty() && len(deployments) == 0 || key.BlacklistedModels.IsBlockAll()) {
		return &schemas.BifrostListModelsResponse{Data: make([]schemas.Model, 0)}, nil
	}

	// If deployments or allowedModels are configured, return those directly without API call
	// Skip this fast path when Unfiltered is set so the full Vertex catalog can be retrieved
	if !request.Unfiltered && (len(deployments) > 0 || allowedModels.IsRestricted()) {
		return buildResponseFromConfig(deployments, allowedModels, key.BlacklistedModels), nil
	}

	// No deployments configured - fetch from Model Garden API
	host := getVertexModelListingAPIHost(region)

	// Accumulate all publisher models from paginated requests
	var allPublisherModels []VertexPublisherModel
	var rawRequests []interface{}
	var rawResponses []interface{}
	pageToken := ""

	// Getting oauth2 token
	tokenSource, err := getAuthTokenSource(key)
	if err != nil {
		return nil, providerUtils.NewBifrostOperationError("error creating auth token source (api key auth not supported for list models)", err)
	}
	token, err := tokenSource.Token()
	if err != nil {
		return nil, providerUtils.NewBifrostOperationError("error getting token (api key auth not supported for list models)", err)
	}

	// Iterate over all supported Vertex publishers to include Google, Anthropic, and Mistral models
	publishers := []string{"google", "anthropic", "mistralai"}
	for _, publisher := range publishers {
		pageToken = ""
		// Loop through all pages until no nextPageToken is returned
		for {
			// Build URL for publishers.models.list endpoint (Model Garden)
			// Format: https://{vertex-api-host}/v1beta1/publishers/{publisher}/models
			requestURL := fmt.Sprintf("https://%s/v1beta1/publishers/%s/models?pageSize=%d", host, publisher, MaxPageSize)
			if pageToken != "" {
				requestURL = fmt.Sprintf("%s&pageToken=%s", requestURL, url.QueryEscape(pageToken))
			}

			// Create HTTP request for listing models
			req := fasthttp.AcquireRequest()
			resp := fasthttp.AcquireResponse()

			req.Header.SetMethod(http.MethodGet)
			req.SetRequestURI(requestURL)
			req.Header.SetContentType("application/json")
			providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)

			latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
			if bifrostErr != nil {
				wait()
				respBody := append([]byte(nil), resp.Body()...)
				fasthttp.ReleaseRequest(req)
				fasthttp.ReleaseResponse(resp)
				// Non-Google publishers may not be available in all regions; skip on error
				if publisher != "google" {
					break
				}
				return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, respBody, provider.sendBackRawRequest, provider.sendBackRawResponse)
			}
			ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

			// Handle error response
			if resp.StatusCode() != fasthttp.StatusOK {
				if resp.StatusCode() == fasthttp.StatusUnauthorized || resp.StatusCode() == fasthttp.StatusForbidden {
					removeVertexClient(key.VertexKeyConfig.AuthCredentials.GetValue())
				}

				// Non-Google publishers may not be available in all regions;
				// skip only on 403/404 which indicate regional unavailability.
				// Surface other errors (401, 429, 5xx) so they aren't silently swallowed.
				if publisher != "google" && (resp.StatusCode() == fasthttp.StatusForbidden || resp.StatusCode() == fasthttp.StatusNotFound) {
					wait()
					fasthttp.ReleaseRequest(req)
					fasthttp.ReleaseResponse(resp)
					break
				}

				respBody := append([]byte(nil), resp.Body()...)
				statusCode := resp.StatusCode()
				wait()
				fasthttp.ReleaseRequest(req)
				fasthttp.ReleaseResponse(resp)

				var errorResp VertexError
				if err := sonic.Unmarshal(respBody, &errorResp); err != nil {
					return nil, providerUtils.EnrichError(ctx, providerUtils.NewBifrostOperationError(schemas.ErrProviderResponseUnmarshal, err), nil, respBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
				}
				return nil, providerUtils.EnrichError(ctx, providerUtils.NewProviderAPIError(errorResp.Error.Message, nil, statusCode, nil, nil), nil, respBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
			}

			// Parse Vertex's publisher models response
			var vertexResponse VertexListPublisherModelsResponse
			rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(resp.Body(), &vertexResponse, nil, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
			if bifrostErr != nil {
				respBody := append([]byte(nil), resp.Body()...)
				wait()
				fasthttp.ReleaseRequest(req)
				fasthttp.ReleaseResponse(resp)
				return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, respBody, provider.sendBackRawRequest, provider.sendBackRawResponse)
			}
			if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
				rawRequests = append(rawRequests, rawRequest)
			}
			if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
				rawResponses = append(rawResponses, rawResponse)
			}

			// Accumulate models from this page
			allPublisherModels = append(allPublisherModels, vertexResponse.PublisherModels...)

			wait()
			fasthttp.ReleaseRequest(req)
			fasthttp.ReleaseResponse(resp)

			// Check if there are more pages
			if vertexResponse.NextPageToken == "" {
				break
			}
			pageToken = vertexResponse.NextPageToken
		}
	}

	// Create aggregated response from all pages
	aggregatedResponse := &VertexListPublisherModelsResponse{
		PublisherModels: allPublisherModels,
	}

	response := aggregatedResponse.ToBifrostListModelsResponse(key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)

	if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
		response.ExtraFields.RawRequest = rawRequests
	}

	if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
		response.ExtraFields.RawResponse = rawResponses
	}

	return response, nil
}

// ListModels performs a list models request to Vertex's API.
// Requests are made concurrently for improved performance.
func (provider *VertexProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	finalResponse, bifrostErr := providerUtils.HandleMultipleListModelsRequests(
		ctx,
		keys,
		request,
		provider.listModelsByKey,
	)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	return finalResponse, nil
}

// TextCompletion is not supported by the Vertex provider.

// resolveURLSource turns one URL source into inline bytes, or reports that the reference
// should be forwarded to the converter untouched.
//
// forward=true means "leave this block exactly as it is" - the reference travels to the
// provider untouched. Otherwise the returned base64 payload replaces it; mediaType is
// best-effort and may be empty (the GCS path never reports one), so callers must keep
// their fallbacks to the declared file type or a sniffed value.
func (provider *VertexProvider) resolveURLSource(ctx *schemas.BifrostContext, key schemas.Key, rawURL string, isAnthropicFamily bool) (mediaType string, encoded string, forward bool, err error) {
	switch classifyURLSource(rawURL, isAnthropicFamily) {
	case urlSourceFetchHTTP:
		mediaType, encoded, err = providerUtils.FetchAndEncodeURL(ctx, rawURL)
	case urlSourceFetchGCS:
		encoded, err = provider.fetchGCSObjectEncoded(ctx, key, rawURL)
	default:
		forward = true
	}
	return mediaType, encoded, forward, err
}

// fetchGCSObjectEncoded reads a gs:// object with the request key's Google
// credentials and returns it base64-encoded. Used for model families that cannot
// take a Cloud Storage URI -- Claude-on-Vertex, which is base64-only. Reuses the
// same authenticated GCS surface as the Files API support on this provider.
func (provider *VertexProvider) fetchGCSObjectEncoded(ctx *schemas.BifrostContext, key schemas.Key, rawURL string) (string, error) {
	bucket, objectKey, err := parseGCSURI(rawURL)
	if err != nil {
		return "", err
	}
	if bucket == "" || objectKey == "" {
		return "", fmt.Errorf("invalid GCS URI %q: expected gs://bucket/object", providerUtils.RedactURLForError(rawURL))
	}
	authHeader, err := gcsGetAuthHeader(key)
	if err != nil {
		return "", err
	}
	content, bifrostErr := provider.gcsDownloadObject(ctx, authHeader, bucket, objectKey)
	if bifrostErr != nil {
		// BifrostError is not an error value, so its message is carried across by hand.
		return "", fmt.Errorf("failed to read %q from Cloud Storage: %s", providerUtils.RedactURLForError(rawURL), bifrostErr.GetErrorString())
	}
	return base64.StdEncoding.EncodeToString(content), nil
}

// inlineRemoteURLSources rewrites document AND image content blocks carrying a URL
// source into whatever the target model family can actually accept, fetching bytes
// when the reference cannot be forwarded. Mutates the request in place; safe to call
// when no such blocks are present. The ctx is propagated to each fetch so request
// cancellation/deadlines abort in-flight downloads.
//
// classifyURLSource holds the per-scheme, per-family rules and cites the provider docs
// behind each one; the short version is that http(s) is always fetched, gs:// is forwarded
// to Gemini and fetched for Claude, and object-store URIs other than gs:// are resolvable
// by neither side.
func (provider *VertexProvider) inlineRemoteURLSources(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) error {
	if request == nil || request.Input == nil {
		return nil
	}
	// When the caller is bypassing the converter via a pre-built raw body,
	// the request struct isn't what gets sent — skip the fetch.
	if useRawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && useRawBody {
		return nil
	}
	isAnthropicFamily := schemas.IsAnthropicModelFamily(ctx, request.Model)
	for mi := range request.Input {
		msg := &request.Input[mi]
		if msg.Content == nil || msg.Content.ContentBlocks == nil {
			continue
		}
		for bi := range msg.Content.ContentBlocks {
			block := &msg.Content.ContentBlocks[bi]

			// Inline url-source documents.
			if block.File != nil && block.File.FileURL != nil && *block.File.FileURL != "" {
				mediaType, encoded, forward, err := provider.resolveURLSource(ctx, key, *block.File.FileURL, isAnthropicFamily)
				if err != nil {
					return err
				}
				if !forward {
					block.File.FileData = &encoded
					if mediaType != "" && block.File.FileType == nil {
						block.File.FileType = &mediaType
					}
					block.File.FileURL = nil
				}
			}

			// Inline url-source images to a base64 data URI; Anthropic-on-Vertex
			// accepts base64 image sources only.
			if img := block.ImageURLStruct; img != nil && img.URL != "" {
				mediaType, encoded, forward, err := provider.resolveURLSource(ctx, key, img.URL, isAnthropicFamily)
				if err != nil {
					return err
				}
				if !forward {
					if mediaType != "" {
						img.URL = "data:" + mediaType + ";base64," + encoded
					} else {
						// No Content-Type to go on (absent header, or a GCS read, which
						// does not surface one); sniff the media type from the fetched
						// bytes so we never emit a malformed "data:;base64,..." URI,
						// which Anthropic-on-Vertex rejects.
						sanitized, sErr := schemas.SanitizeImageURL(encoded)
						if sErr != nil {
							return sErr
						}
						img.URL = sanitized
					}
				}
			}
		}
	}
	return nil
}

// inlineDocumentURLsResponses is the Responses-API analogue of inlineRemoteURLSources.
// File blocks live on ResponsesMessageContentBlock.ResponsesInputMessageContentBlockFile
// rather than the chat ContentBlock.File, so this walks the responses-shape input.
// Same per-scheme, per-family rules -- see classifyURLSource.
func (provider *VertexProvider) inlineDocumentURLsResponses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) error {
	if request == nil || request.Input == nil {
		return nil
	}
	if useRawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && useRawBody {
		return nil
	}
	isAnthropicFamily := schemas.IsAnthropicModelFamily(ctx, request.Model)
	for mi := range request.Input {
		msg := &request.Input[mi]
		if msg.Content == nil || msg.Content.ContentBlocks == nil {
			continue
		}
		for bi := range msg.Content.ContentBlocks {
			block := &msg.Content.ContentBlocks[bi]

			// Inline url-source files.
			if f := block.ResponsesInputMessageContentBlockFile; f != nil && f.FileURL != nil && *f.FileURL != "" {
				mediaType, encoded, forward, err := provider.resolveURLSource(ctx, key, *f.FileURL, isAnthropicFamily)
				if err != nil {
					return err
				}
				if !forward {
					f.FileData = &encoded
					if mediaType != "" && f.FileType == nil {
						f.FileType = &mediaType
					}
					f.FileURL = nil
				}
			}

			// Inline url-source images to a base64 data URI; Anthropic-on-Vertex
			// accepts base64 image sources only.
			if img := block.ResponsesInputMessageContentBlockImage; img != nil && img.ImageURL != nil && *img.ImageURL != "" {
				mediaType, encoded, forward, err := provider.resolveURLSource(ctx, key, *img.ImageURL, isAnthropicFamily)
				if err != nil {
					return err
				}
				if !forward {
					if mediaType != "" {
						dataURI := "data:" + mediaType + ";base64," + encoded
						img.ImageURL = &dataURI
					} else {
						// No Content-Type to go on (absent header, or a GCS read, which
						// does not surface one); sniff the media type from the fetched
						// bytes so we never emit a malformed "data:;base64,..." URI,
						// which Anthropic-on-Vertex rejects.
						sanitized, sErr := schemas.SanitizeImageURL(encoded)
						if sErr != nil {
							return sErr
						}
						img.ImageURL = &sanitized
					}
				}
			}
		}
	}
	return nil
}

// ChatCompletion performs a chat completion request to the Vertex API.
// It supports both text and image content in messages.
// Returns a BifrostResponse containing the completion results or an error if the request fails.
func (provider *VertexProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	var jsonBody []byte
	var bifrostErr *schemas.BifrostError
	// Resolve URL sources the target model family cannot read for itself: http(s) is
	// downloaded for both families (Vertex's own crawler rejects forwarded URLs), while a
	// gs:// URI is forwarded to Gemini as fileData.fileUri and read from Cloud Storage for
	// Claude, which accepts base64 sources only. See classifyURLSource.
	if err := provider.inlineRemoteURLSources(ctx, key, request); err != nil {
		return nil, providerUtils.NewBifrostOperationError("failed to inline remote URL sources for vertex", err)
	}
	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		jsonBody, bifrostErr = anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Vertex,
			Model:                     request.Model,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ProviderExtraHeaders:      provider.networkConfig.ExtraHeaders,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
	} else {
		jsonBody, bifrostErr = providerUtils.CheckContextAndGetRequestBody(
			ctx,
			request,
			func() (providerUtils.RequestBodyWithExtraParams, error) {
				// Format messages for Vertex API, preserving key order for prompt caching
				var rawBody []byte
				var extraParams map[string]interface{}
				var err error

				if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
					reqBody, err := gemini.ToGeminiChatCompletionRequestWithImageURLSchemes(ctx, request, geminiImageURLSchemes...)
					if err != nil {
						return nil, err
					}
					if reqBody == nil {
						return nil, fmt.Errorf("chat completion input is not provided")
					}
					extraParams = reqBody.GetExtraParams()
					// Strip unsupported fields for Vertex Gemini
					stripVertexGeminiUnsupportedFields(reqBody)
					// Marshal to JSON bytes
					rawBody, err = providerUtils.MarshalSorted(reqBody)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal request body: %w", err)
					}
				} else {
					// Use centralized OpenAI converter for non-Claude models
					reqBody := openai.ToOpenAIChatRequest(ctx, request)
					if reqBody == nil {
						return nil, fmt.Errorf("chat completion input is not provided")
					}
					extraParams = reqBody.GetExtraParams()
					// Marshal to JSON bytes
					rawBody, err = providerUtils.MarshalSorted(reqBody)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal request body: %w", err)
					}
				}
				// Remove region field if present
				rawBody, err = providerUtils.DeleteJSONField(rawBody, "region")
				if err != nil {
					return nil, fmt.Errorf("failed to delete region field: %w", err)
				}
				return &VertexRawRequestBody{RawBody: rawBody, ExtraParams: extraParams}, nil
			},
		)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}
	if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		if rawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && rawBody {
			jsonBody = gemini.NormalizeRawGenerateContentRequestForCompatibility(jsonBody)
		}
		jsonBody = stripVertexGeminiUnsupportedFieldsRaw(jsonBody)
	}

	projectID := resolveVertexProjectID(ctx, key)
	if projectID == "" {
		return nil, providerUtils.NewConfigurationError("project ID is not set")
	}

	region := resolveVertexRegion(ctx, key)
	if region == "" {
		return nil, providerUtils.NewConfigurationError("region is not set in key config")
	}

	// Remap unsupported tool versions for Vertex (handles raw passthrough bodies)
	if schemas.IsAnthropicModelFamily(ctx, request.Model) && jsonBody != nil {
		capModel := schemas.ResolveCanonicalModel(ctx, request.Model)
		remappedBody, remapErr := anthropic.RemapRawToolVersionsForProvider(jsonBody, schemas.Vertex, capModel)
		if remapErr != nil {
			return nil, providerUtils.NewBifrostOperationError(remapErr.Error(), nil)
		}
		jsonBody = remappedBody

		// Strip unsupported body fields for Vertex — covers both structured and raw passthrough paths.
		var stripErr error
		jsonBody, stripErr = anthropic.StripUnsupportedFieldsFromRawBody(jsonBody, schemas.Vertex, capModel)
		if stripErr != nil {
			return nil, providerUtils.NewBifrostOperationError(stripErr.Error(), nil)
		}
	}

	// Auth query is used for fine-tuned models to pass the API key in the query string
	authQuery := ""
	// Determine the URL based on model type
	var completeURL string
	if schemas.IsAllDigitsASCII(request.Model) {
		// Custom Fine-tuned models use OpenAPI endpoint
		projectNumber := resolveVertexProjectNumber(ctx, key)
		if projectNumber == "" {
			return nil, providerUtils.NewConfigurationError("project number is not set for fine-tuned models")
		}
		if key.Value.GetValue() != "" {
			authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
		}
		completeURL = getVertexEndpointURL(region, "v1beta1", projectNumber, request.Model, ":generateContent")
	} else if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		// Claude models use Anthropic publisher — model-aware host for multi-region support
		completeURL = getVertexModelAwarePublisherModelURL(region, "v1", projectID, "anthropic", request.Model, ":rawPredict", resolveVertexForceSingleRegion(ctx, key), provider.logger)
	} else if schemas.IsMistralModelFamily(ctx, request.Model) {
		// Mistral models use mistralai publisher with rawPredict
		completeURL = getVertexPublisherModelURL(region, "v1", projectID, "mistralai", request.Model, ":rawPredict")
	} else if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		// Gemini models support api key
		if key.Value.GetValue() != "" {
			authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
		}
		completeURL = getVertexPublisherModelURL(region, "v1", projectID, "google", gemini.NormalizeModelName(request.Model), ":generateContent")
	} else {
		completeURL = getVertexEndpointURL(region, "v1beta1", projectID, "openapi/chat/completions", "")
	}

	// Create HTTP request for streaming
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	respOwned := true
	defer func() {
		if respOwned {
			fasthttp.ReleaseResponse(resp)
		}
	}()

	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	if (schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model)) &&
		request.Params != nil && request.Params.ServiceTier != nil {
		if v := vertexServiceTierHeaderValue(region, request.Model, *request.Params.ServiceTier); v != "" {
			req.Header.Set(VertexServiceTierHeader, v)
		}
	}
	// Skip anthropic-beta from context headers — Anthropic models on Vertex use the
	// anthropic_beta body field instead, and other model families don't use it.
	providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, []string{anthropic.AnthropicBetaHeader})

	// If auth query is set, add it to the URL
	// Otherwise, get the oauth2 token and set the Authorization header
	if authQuery != "" {
		completeURL = fmt.Sprintf("%s?%s", completeURL, authQuery)
	} else {
		// Getting oauth2 token
		tokenSource, err := getAuthTokenSource(key)
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
		}
		token, err := tokenSource.Token()
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error getting token", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}

	req.SetRequestURI(completeURL)
	usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBody(ctx, req)
	if !usedLargePayloadBody {
		req.SetBody(jsonBody)
	}

	// Make the request with optional large response streaming
	activeClient := providerUtils.PrepareResponseStreaming(ctx, provider.client, resp)
	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
	defer wait()
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if usedLargePayloadBody {
		providerUtils.DrainLargePayloadRemainder(ctx)
	}
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

	if resp.StatusCode() != fasthttp.StatusOK {
		providerUtils.MaterializeStreamErrorBody(ctx, resp)
		// Remove client from pool for authentication/authorization errors
		if resp.StatusCode() == fasthttp.StatusUnauthorized || resp.StatusCode() == fasthttp.StatusForbidden {
			removeVertexClient(key.VertexKeyConfig.AuthCredentials.GetValue())
		}
		return nil, providerUtils.EnrichError(ctx, parseVertexError(resp), jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}

	responseBody, isLargeResp, decodeErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
	if decodeErr != nil {
		return nil, providerUtils.EnrichError(ctx, decodeErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
	}
	if isLargeResp {
		respOwned = false
		return &schemas.BifrostChatResponse{
			Model: request.Model,
			ExtraFields: schemas.BifrostResponseExtraFields{
				Latency:                 latency.Milliseconds(),
				ProviderResponseHeaders: providerUtils.ExtractProviderResponseHeaders(resp),
			},
		}, nil
	}

	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		// Create response object from pool
		anthropicResponse := anthropic.AcquireAnthropicMessageResponse()
		defer anthropic.ReleaseAnthropicMessageResponse(anthropicResponse)

		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, anthropicResponse, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		// Create final response
		response := anthropicResponse.ToBifrostChatResponse(ctx)

		response.ExtraFields = schemas.BifrostResponseExtraFields{
			Latency:                 latency.Milliseconds(),
			ProviderResponseHeaders: providerUtils.ExtractProviderResponseHeaders(resp),
		}

		// Set raw request if enabled
		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			response.ExtraFields.RawRequest = rawRequest
		}

		// Set raw response if enabled
		if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
			response.ExtraFields.RawResponse = rawResponse
		}

		return response, nil
	} else if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		geminiResponse := gemini.GenerateContentResponse{}

		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, &geminiResponse, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		response := geminiResponse.ToBifrostChatResponse()
		response.ExtraFields.Latency = latency.Milliseconds()
		response.ExtraFields.ProviderResponseHeaders = providerUtils.ExtractProviderResponseHeaders(resp)

		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			response.ExtraFields.RawRequest = rawRequest
		}

		if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
			response.ExtraFields.RawResponse = rawResponse
		}

		return response, nil
	} else {
		response := &schemas.BifrostChatResponse{}

		// Use enhanced response handler with pre-allocated response
		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, response, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		response.ExtraFields.Latency = latency.Milliseconds()
		response.ExtraFields.ProviderResponseHeaders = providerUtils.ExtractProviderResponseHeaders(resp)

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
}

// ChatCompletionStream performs a streaming chat completion request to the Vertex API.
// It supports both OpenAI-style streaming (for non-Claude models) and Anthropic-style streaming (for Claude models).
// Returns a channel of BifrostStreamChunk objects for streaming results or an error if the request fails.
func (provider *VertexProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	providerName := provider.GetProviderKey()
	projectID := resolveVertexProjectID(ctx, key)
	if projectID == "" {
		return nil, providerUtils.NewConfigurationError("project ID is not set")
	}

	region := resolveVertexRegion(ctx, key)
	if region == "" {
		return nil, providerUtils.NewConfigurationError("region is not set in key config")
	}

	// Resolve URL sources the target model family cannot read for itself: http(s) is
	// downloaded for both families (Vertex's own crawler rejects forwarded URLs), while a
	// gs:// URI is forwarded to Gemini as fileData.fileUri and read from Cloud Storage for
	// Claude, which accepts base64 sources only. See classifyURLSource.
	if err := provider.inlineRemoteURLSources(ctx, key, request); err != nil {
		return nil, providerUtils.NewBifrostOperationError("failed to inline remote URL sources for vertex", err)
	}
	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		// Use Anthropic-style streaming for Claude models.
		// Anthropic-on-Vertex doesn't accept URL-source document or image blocks; inline first.
		jsonData, bifrostErr := anthropic.BuildAnthropicChatRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Vertex,
			Model:                     request.Model,
			IsStreaming:               true,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ProviderExtraHeaders:      provider.networkConfig.ExtraHeaders,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		// Remap unsupported tool versions for Vertex streaming (handles raw passthrough bodies)
		if jsonData != nil {
			capModel := schemas.ResolveCanonicalModel(ctx, request.Model)
			var remapErr error
			jsonData, remapErr = anthropic.RemapRawToolVersionsForProvider(jsonData, schemas.Vertex, capModel)
			if remapErr != nil {
				return nil, providerUtils.NewBifrostOperationError(remapErr.Error(), nil)
			}

			// Strip unsupported body fields for Vertex — covers both structured and raw passthrough paths.
			var stripErr error
			jsonData, stripErr = anthropic.StripUnsupportedFieldsFromRawBody(jsonData, schemas.Vertex, capModel)
			if stripErr != nil {
				return nil, providerUtils.NewBifrostOperationError(stripErr.Error(), nil)
			}
		}

		completeURL := getVertexModelAwarePublisherModelURL(region, "v1", projectID, "anthropic", request.Model, ":streamRawPredict", resolveVertexForceSingleRegion(ctx, key), provider.logger)

		// Prepare headers for Vertex Anthropic
		headers := map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
		}

		// Adding authorization header
		tokenSource, err := getAuthTokenSource(key)
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
		}
		token, err := tokenSource.Token()
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error getting token", err)
		}
		headers["Authorization"] = "Bearer " + token.AccessToken

		// Use shared Anthropic streaming logic
		return anthropic.HandleAnthropicChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			completeURL,
			jsonData,
			headers,
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			provider.networkConfig.BetaHeaderOverrides,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			providerName,
			postHookRunner,
			nil,
			nil,
			provider.logger,
			postHookSpanFinalizer,
		)
	} else if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		// Use Gemini-style streaming for Gemini models
		jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
			ctx,
			request,
			func() (providerUtils.RequestBodyWithExtraParams, error) {
				reqBody, err := gemini.ToGeminiChatCompletionRequestWithImageURLSchemes(ctx, request, geminiImageURLSchemes...)
				if err != nil {
					return nil, err
				}
				if reqBody == nil {
					return nil, fmt.Errorf("chat completion input is not provided")
				}
				// Strip unsupported fields for Vertex Gemini
				stripVertexGeminiUnsupportedFields(reqBody)
				return reqBody, nil
			},
		)
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		// Auth query is used to pass the API key in the query string
		authQuery := ""
		if key.Value.GetValue() != "" {
			authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
		}

		// For custom/fine-tuned models, validate projectNumber is set
		projectNumber := resolveVertexProjectNumber(ctx, key)
		if schemas.IsAllDigitsASCII(request.Model) && projectNumber == "" {
			return nil, providerUtils.NewConfigurationError("project number is not set for fine-tuned models")
		}

		// Construct the URL for Gemini streaming
		completeURL := getCompleteURLForGeminiEndpoint(request.Model, region, projectID, projectNumber, ":streamGenerateContent")

		// Add alt=sse parameter
		if authQuery != "" {
			completeURL = fmt.Sprintf("%s?alt=sse&%s", completeURL, authQuery)
		} else {
			completeURL = fmt.Sprintf("%s?alt=sse", completeURL)
		}

		// Prepare headers for Vertex Gemini
		headers := map[string]string{
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
		}

		if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) {
			if _, overridden := provider.networkConfig.ExtraHeaders[VertexServiceTierHeader]; !overridden {
				if request.Params != nil && request.Params.ServiceTier != nil {
					if v := vertexServiceTierHeaderValue(region, request.Model, *request.Params.ServiceTier); v != "" {
						headers[VertexServiceTierHeader] = v
					}
				}
			}
		}

		// If no auth query, use OAuth2 token
		if authQuery == "" {
			tokenSource, err := getAuthTokenSource(key)
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
			}
			token, err := tokenSource.Token()
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error getting token", err)
			}
			headers["Authorization"] = "Bearer " + token.AccessToken
		}

		// Use shared streaming logic from Gemini
		return gemini.HandleGeminiChatCompletionStream(
			ctx,
			provider.streamingClient,
			completeURL,
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
	} else {
		var authHeader map[string]string
		// Auth query is used for fine-tuned models to pass the API key in the query string
		authQuery := ""
		// Determine the URL based on model type
		var completeURL string
		if schemas.IsMistralModelFamily(ctx, request.Model) {
			// Mistral models use mistralai publisher with streamRawPredict
			completeURL = getVertexPublisherModelURL(region, "v1", projectID, "mistralai", request.Model, ":streamRawPredict")
		} else {
			// Other models use OpenAPI endpoint for gemini models
			if key.Value.GetValue() != "" {
				authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
			}
			completeURL = getVertexEndpointURL(region, "v1beta1", projectID, "openapi/chat/completions", "")
		}

		if authQuery != "" {
			completeURL = fmt.Sprintf("%s?%s", completeURL, authQuery)
		} else {
			// Getting oauth2 token
			tokenSource, err := getAuthTokenSource(key)
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
			}
			token, err := tokenSource.Token()
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error getting token", err)
			}
			authHeader = map[string]string{
				"Authorization": "Bearer " + token.AccessToken,
			}
		}

		// Use shared OpenAI streaming logic
		return openai.HandleOpenAIChatCompletionStreaming(
			ctx,
			provider.streamingClient,
			completeURL,
			request,
			authHeader,
			provider.networkConfig.ExtraHeaders,
			provider.networkConfig.StreamIdleTimeoutInSeconds,
			providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest),
			providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse),
			providerName,
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

// Responses performs a responses request to the Vertex API.
func (provider *VertexProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	// Resolve URL sources the target model family cannot read for itself: http(s) is
	// downloaded for both families (Vertex's own crawler rejects forwarded URLs), while a
	// gs:// URI is forwarded to Gemini as fileData.fileUri and read from Cloud Storage for
	// Claude, which accepts base64 sources only. See classifyURLSource.
	if err := provider.inlineDocumentURLsResponses(ctx, key, request); err != nil {
		return nil, providerUtils.NewBifrostOperationError("failed to inline document URLs for vertex", err)
	}
	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		jsonBody, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Vertex,
			Model:                     request.Model,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ProviderExtraHeaders:      provider.networkConfig.ExtraHeaders,
			ValidateTools:             true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		projectID := resolveVertexProjectID(ctx, key)
		if projectID == "" {
			return nil, providerUtils.NewConfigurationError("project ID is not set")
		}

		region := resolveVertexRegion(ctx, key)
		if region == "" {
			return nil, providerUtils.NewConfigurationError("region is not set in key config")
		}

		// Claude models use Anthropic publisher — model-aware host for multi-region support
		url := getVertexModelAwarePublisherModelURL(region, "v1beta1", projectID, "anthropic", request.Model, ":rawPredict", resolveVertexForceSingleRegion(ctx, key), provider.logger)

		// Create HTTP request for streaming
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		respOwned := true
		defer func() {
			if respOwned {
				fasthttp.ReleaseResponse(resp)
			}
		}()

		req.Header.SetMethod(http.MethodPost)
		req.Header.SetContentType("application/json")
		providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, []string{anthropic.AnthropicBetaHeader})

		if betaHeaders := anthropic.FilterBetaHeadersForProvider(anthropic.MergeBetaHeaders(ctx, provider.networkConfig.ExtraHeaders), schemas.Vertex, provider.networkConfig.BetaHeaderOverrides); len(betaHeaders) > 0 {
			req.Header.Set(anthropic.AnthropicBetaHeader, strings.Join(betaHeaders, ","))
		} else {
			req.Header.Del(anthropic.AnthropicBetaHeader)
		}

		// Getting oauth2 token
		tokenSource, err := getAuthTokenSource(key)
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
		}
		token, err := tokenSource.Token()
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error getting token", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)

		req.SetRequestURI(url)
		usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBody(ctx, req)
		if !usedLargePayloadBody {
			req.SetBody(jsonBody)
		}

		// Make the request with optional large response streaming
		activeClient := providerUtils.PrepareResponseStreaming(ctx, provider.client, resp)
		latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
		defer wait()
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}
		if usedLargePayloadBody {
			providerUtils.DrainLargePayloadRemainder(ctx)
		}
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

		if resp.StatusCode() != fasthttp.StatusOK {
			providerUtils.MaterializeStreamErrorBody(ctx, resp)
			// Remove client from pool for authentication/authorization errors
			if resp.StatusCode() == fasthttp.StatusUnauthorized || resp.StatusCode() == fasthttp.StatusForbidden {
				removeVertexClient(key.VertexKeyConfig.AuthCredentials.GetValue())
			}
			return nil, providerUtils.EnrichError(ctx, parseVertexError(resp), jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		responseBody, isLargeResp, decodeErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
		if decodeErr != nil {
			return nil, providerUtils.EnrichError(ctx, decodeErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}
		if isLargeResp {
			respOwned = false
			return &schemas.BifrostResponsesResponse{
				ExtraFields: schemas.BifrostResponseExtraFields{
					Latency:                 latency.Milliseconds(),
					ProviderResponseHeaders: providerUtils.ExtractProviderResponseHeaders(resp),
				},
			}, nil
		}

		// Create response object from pool
		anthropicResponse := anthropic.AcquireAnthropicMessageResponse()
		defer anthropic.ReleaseAnthropicMessageResponse(anthropicResponse)

		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, anthropicResponse, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		// Create final response
		response := anthropicResponse.ToBifrostResponsesResponse(ctx)

		response.ExtraFields = schemas.BifrostResponseExtraFields{
			Latency: latency.Milliseconds(),
		}

		response.ExtraFields.ProviderResponseHeaders = providerUtils.ExtractProviderResponseHeaders(resp)
		// Set raw request if enabled
		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			response.ExtraFields.RawRequest = rawRequest
		}

		// Set raw response if enabled
		if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
			response.ExtraFields.RawResponse = rawResponse
		}

		return response, nil
	} else if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		jsonBody, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
			ctx,
			request,
			func() (providerUtils.RequestBodyWithExtraParams, error) {
				reqBody, err := gemini.ToGeminiResponsesRequestWithImageURLSchemes(ctx, request, geminiImageURLSchemes...)
				if err != nil {
					return nil, err
				}
				if reqBody == nil {
					return nil, fmt.Errorf("responses input is not provided")
				}
				// Strip unsupported fields for Vertex Gemini
				stripVertexGeminiUnsupportedFields(reqBody)
				return reqBody, nil
			},
		)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		if rawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && rawBody {
			jsonBody = gemini.NormalizeRawGenerateContentRequestForCompatibility(jsonBody)
		}
		jsonBody = stripVertexGeminiUnsupportedFieldsRaw(jsonBody)

		projectID := resolveVertexProjectID(ctx, key)
		if projectID == "" {
			return nil, providerUtils.NewConfigurationError("project ID is not set")
		}

		region := resolveVertexRegion(ctx, key)
		if region == "" {
			return nil, providerUtils.NewConfigurationError("region is not set in key config")
		}

		authQuery := ""
		if key.Value.GetValue() != "" {
			authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
		}

		// For custom/fine-tuned models, validate projectNumber is set
		projectNumber := resolveVertexProjectNumber(ctx, key)
		if schemas.IsAllDigitsASCII(request.Model) && projectNumber == "" {
			return nil, providerUtils.NewConfigurationError("project number is not set for fine-tuned models")
		}

		url := getCompleteURLForGeminiEndpoint(request.Model, region, projectID, projectNumber, ":generateContent")

		// Create HTTP request for streaming
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		respOwned := true
		defer func() {
			if respOwned {
				fasthttp.ReleaseResponse(resp)
			}
		}()

		req.Header.SetMethod(http.MethodPost)
		req.Header.SetContentType("application/json")
		if (schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model)) &&
			request.Params != nil && request.Params.ServiceTier != nil {
			if v := vertexServiceTierHeaderValue(region, request.Model, *request.Params.ServiceTier); v != "" {
				req.Header.Set(VertexServiceTierHeader, v)
			}
		}

		providerUtils.SetExtraHeaders(ctx, req, provider.networkConfig.ExtraHeaders, nil)

		// If auth query is set, add it to the URL
		// Otherwise, get the oauth2 token and set the Authorization header
		if authQuery != "" {
			url = fmt.Sprintf("%s?%s", url, authQuery)
		} else {
			// Getting oauth2 token
			tokenSource, err := getAuthTokenSource(key)
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
			}
			token, err := tokenSource.Token()
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error getting token", err)
			}
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}

		req.SetRequestURI(url)
		usedLargePayloadBody := providerUtils.ApplyLargePayloadRequestBody(ctx, req)
		if !usedLargePayloadBody {
			req.SetBody(jsonBody)
		}

		// Make the request with optional large response streaming
		activeClient := providerUtils.PrepareResponseStreaming(ctx, provider.client, resp)
		latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, activeClient, req, resp)
		defer wait()
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}
		if usedLargePayloadBody {
			providerUtils.DrainLargePayloadRemainder(ctx)
		}
		ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerUtils.ExtractProviderResponseHeaders(resp))

		if resp.StatusCode() != fasthttp.StatusOK {
			providerUtils.MaterializeStreamErrorBody(ctx, resp)
			// Remove client from pool for authentication/authorization errors
			if resp.StatusCode() == fasthttp.StatusUnauthorized || resp.StatusCode() == fasthttp.StatusForbidden {
				removeVertexClient(key.VertexKeyConfig.AuthCredentials.GetValue())
			}
			return nil, providerUtils.EnrichError(ctx, parseVertexError(resp), jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		responseBody, isLargeResp, decodeErr := providerUtils.FinalizeResponseWithLargeDetection(ctx, resp, provider.logger)
		if decodeErr != nil {
			return nil, providerUtils.EnrichError(ctx, decodeErr, jsonBody, nil, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}
		if isLargeResp {
			respOwned = false
			return &schemas.BifrostResponsesResponse{
				ExtraFields: schemas.BifrostResponseExtraFields{
					Latency:                 latency.Milliseconds(),
					ProviderResponseHeaders: providerUtils.ExtractProviderResponseHeaders(resp),
				},
			}, nil
		}

		geminiResponse := &gemini.GenerateContentResponse{}

		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, geminiResponse, jsonBody, providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest), providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse))
		if bifrostErr != nil {
			return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonBody, responseBody, provider.sendBackRawRequest, provider.sendBackRawResponse, latency)
		}

		response := geminiResponse.ToResponsesBifrostResponsesResponse()
		response.ExtraFields.Latency = latency.Milliseconds()
		response.ExtraFields.ProviderResponseHeaders = providerUtils.ExtractProviderResponseHeaders(resp)

		// Set raw response if enabled
		if providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse) {
			response.ExtraFields.RawResponse = rawResponse
		}

		if providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest) {
			response.ExtraFields.RawRequest = rawRequest
		}

		return response, nil
	} else {
		chatResponse, err := provider.ChatCompletion(ctx, key, request.ToChatRequest())
		if err != nil {
			return nil, err
		}

		response := chatResponse.ToBifrostResponsesResponse()
		return response, nil
	}
}

// ResponsesStream performs a streaming responses request to the Vertex API.
func (provider *VertexProvider) ResponsesStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	// Resolve URL sources the target model family cannot read for itself: http(s) is
	// downloaded for both families (Vertex's own crawler rejects forwarded URLs), while a
	// gs:// URI is forwarded to Gemini as fileData.fileUri and read from Cloud Storage for
	// Claude, which accepts base64 sources only. See classifyURLSource.
	if err := provider.inlineDocumentURLsResponses(ctx, key, request); err != nil {
		return nil, providerUtils.NewBifrostOperationError("failed to inline document URLs for vertex", err)
	}
	if schemas.IsAnthropicModelFamily(ctx, request.Model) {
		region := resolveVertexRegion(ctx, key)
		if region == "" {
			return nil, providerUtils.NewConfigurationError("region is not set in key config")
		}

		projectID := resolveVertexProjectID(ctx, key)
		if projectID == "" {
			return nil, providerUtils.NewConfigurationError("project ID is not set")
		}

		jsonBody, bifrostErr := anthropic.BuildAnthropicResponsesRequestBody(ctx, request, anthropic.AnthropicRequestBuildConfig{
			Provider:                  schemas.Vertex,
			Model:                     request.Model,
			IsStreaming:               true,
			BetaHeaderOverrides:       provider.networkConfig.BetaHeaderOverrides,
			ProviderExtraHeaders:      provider.networkConfig.ExtraHeaders,
			ValidateTools:             true,
			ShouldSendBackRawRequest:  provider.sendBackRawRequest,
			ShouldSendBackRawResponse: provider.sendBackRawResponse,
		})
		if bifrostErr != nil {
			return nil, bifrostErr
		}

		url := getVertexModelAwarePublisherModelURL(region, "v1", projectID, "anthropic", request.Model, ":streamRawPredict", resolveVertexForceSingleRegion(ctx, key), provider.logger)

		// Prepare headers for Vertex Anthropic
		headers := map[string]string{
			"Content-Type":  "application/json",
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
		}

		// Adding authorization header
		tokenSource, err := getAuthTokenSource(key)
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
		}
		token, err := tokenSource.Token()
		if err != nil {
			return nil, providerUtils.NewBifrostOperationError("error getting token", err)
		}
		headers["Authorization"] = "Bearer " + token.AccessToken

		// Use shared streaming logic from Anthropic
		return anthropic.HandleAnthropicResponsesStream(
			ctx,
			provider.streamingClient,
			url,
			jsonBody,
			headers,
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
	} else if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) || schemas.IsGemmaModelFamily(ctx, request.Model) {
		region := resolveVertexRegion(ctx, key)
		if region == "" {
			return nil, providerUtils.NewConfigurationError("region is not set in key config")
		}

		projectID := resolveVertexProjectID(ctx, key)
		if projectID == "" {
			return nil, providerUtils.NewConfigurationError("project ID is not set")
		}

		// Use Gemini-style streaming for Gemini models
		jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
			ctx,
			request,
			func() (providerUtils.RequestBodyWithExtraParams, error) {
				reqBody, err := gemini.ToGeminiResponsesRequestWithImageURLSchemes(ctx, request, geminiImageURLSchemes...)
				if err != nil {
					return nil, err
				}
				if reqBody == nil {
					return nil, fmt.Errorf("responses input is not provided")
				}
				// Strip unsupported fields for Vertex Gemini
				stripVertexGeminiUnsupportedFields(reqBody)
				return reqBody, nil
			},
		)
		if bifrostErr != nil {
			return nil, bifrostErr
		}
		if rawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && rawBody {
			jsonData = gemini.NormalizeRawGenerateContentRequestForCompatibility(jsonData)
		}
		jsonData = stripVertexGeminiUnsupportedFieldsRaw(jsonData)

		// Auth query is used to pass the API key in the query string
		authQuery := ""
		if key.Value.GetValue() != "" {
			authQuery = fmt.Sprintf("key=%s", url.QueryEscape(key.Value.GetValue()))
		}

		// For custom/fine-tuned models, validate projectNumber is set
		projectNumber := resolveVertexProjectNumber(ctx, key)
		if schemas.IsAllDigitsASCII(request.Model) && projectNumber == "" {
			return nil, providerUtils.NewConfigurationError("project number is not set for fine-tuned models")
		}

		// Construct the URL for Gemini streaming
		completeURL := getCompleteURLForGeminiEndpoint(request.Model, region, projectID, projectNumber, ":streamGenerateContent")
		// Add alt=sse parameter
		if authQuery != "" {
			completeURL = fmt.Sprintf("%s?alt=sse&%s", completeURL, authQuery)
		} else {
			completeURL = fmt.Sprintf("%s?alt=sse", completeURL)
		}

		// Prepare headers for Vertex Gemini
		headers := map[string]string{
			"Accept":        "text/event-stream",
			"Cache-Control": "no-cache",
		}

		if schemas.IsGeminiModelFamily(ctx, request.Model) || schemas.IsAllDigitsASCII(request.Model) {
			if _, overridden := provider.networkConfig.ExtraHeaders[VertexServiceTierHeader]; !overridden {
				if request.Params != nil && request.Params.ServiceTier != nil {
					if v := vertexServiceTierHeaderValue(region, request.Model, *request.Params.ServiceTier); v != "" {
						headers[VertexServiceTierHeader] = v
					}
				}
			}
		}

		// If no auth query, use OAuth2 token
		if authQuery == "" {
			tokenSource, err := getAuthTokenSource(key)
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error creating auth token source", err)
			}
			token, err := tokenSource.Token()
			if err != nil {
				return nil, providerUtils.NewBifrostOperationError("error getting token", err)
			}
			headers["Authorization"] = "Bearer " + token.AccessToken
		}

		// Use shared streaming logic from Gemini
		return gemini.HandleGeminiResponsesStream(
			ctx,
			provider.streamingClient,
			completeURL,
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
	} else {
		ctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
		return provider.ChatCompletionStream(
			ctx,
			postHookRunner,
			postHookSpanFinalizer,
			key,
			request.ToChatRequest(),
		)
	}
}


// stripVertexGeminiUnsupportedFields removes fields that are not supported by Vertex AI's Gemini API.
// Specifically, it removes the "id" field from function_call and function_response objects in contents.
func stripVertexGeminiUnsupportedFields(requestBody *gemini.GeminiGenerationRequest) {
	// Strip service tier — Vertex uses HTTP headers for this, not the request body.
	requestBody.ServiceTier = ""
	for _, content := range requestBody.Contents {
		for _, part := range content.Parts {
			// Remove id from function_call
			if part.FunctionCall != nil {
				part.FunctionCall.ID = ""
			}
			// Remove id from function_response
			if part.FunctionResponse != nil {
				part.FunctionResponse.ID = ""
			}
		}
	}
}

func stripVertexGeminiUnsupportedFieldsRaw(jsonBody []byte) []byte {
	if len(jsonBody) == 0 {
		return jsonBody
	}

	contents := gjson.GetBytes(jsonBody, "contents")
	if !contents.IsArray() {
		return jsonBody
	}

	out := jsonBody
	contentIndex := 0
	contents.ForEach(func(_, content gjson.Result) bool {
		parts := content.Get("parts")
		if !parts.IsArray() {
			contentIndex++
			return true
		}
		partIndex := 0
		parts.ForEach(func(_, part gjson.Result) bool {
			if part.Get("functionCall.id").Exists() {
				if updated, err := providerUtils.DeleteJSONField(out, fmt.Sprintf("contents.%d.parts.%d.functionCall.id", contentIndex, partIndex)); err == nil {
					out = updated
				}
			}
			if part.Get("functionResponse.id").Exists() {
				if updated, err := providerUtils.DeleteJSONField(out, fmt.Sprintf("contents.%d.parts.%d.functionResponse.id", contentIndex, partIndex)); err == nil {
					out = updated
				}
			}
			partIndex++
			return true
		})
		contentIndex++
		return true
	})

	// Strip top-level serviceTier — Vertex uses HTTP headers for this, not the request body.
	if providerUtils.JSONFieldExists(out, "serviceTier") {
		if updated, err := providerUtils.DeleteJSONField(out, "serviceTier"); err == nil {
			out = updated
		}
	}

	return out
}


func parseGCSURI(uri string) (bucket, objectKey string, err error) {
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", fmt.Errorf("invalid GCS URI %q: must start with gs://", uri)
	}
	rest := strings.TrimPrefix(uri, "gs://")
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return rest, "", nil
	}
	return rest[:idx], rest[idx+1:], nil
}


func gcsGetAuthHeader(key schemas.Key) (string, error) {
	tokenSrc, err := getAuthTokenSource(key)
	if err != nil {
		return "", fmt.Errorf("failed to get GCS auth token source: %w", err)
	}
	tok, err := tokenSrc.Token()
	if err != nil {
		removeVertexClient(key.VertexKeyConfig.AuthCredentials.GetValue())
		return "", fmt.Errorf("failed to acquire GCS access token: %w", err)
	}
	return "Bearer " + tok.AccessToken, nil
}

func (provider *VertexProvider) gcsDownloadObject(ctx *schemas.BifrostContext, authHeader, bucket, objectKey string) ([]byte, *schemas.BifrostError) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	downloadURL := fmt.Sprintf("https://storage.googleapis.com/storage/v1/b/%s/o/%s?alt=media", url.PathEscape(bucket), url.PathEscape(objectKey))
	req.SetRequestURI(downloadURL)
	req.Header.SetMethod(http.MethodGet)
	req.Header.Set("Authorization", authHeader)

	if err := provider.client.Do(req, resp); err != nil {
		return nil, providerUtils.NewBifrostOperationError(schemas.ErrProviderDoRequest, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, &schemas.BifrostError{
			IsBifrostError: false,
			Error: &schemas.ErrorField{
				Message: fmt.Sprintf("failed to download GCS object: status code %d", resp.StatusCode()),
				Code:    schemas.Ptr(strconv.Itoa(resp.StatusCode())),
			},
		}
	}

	body := resp.Body()
	res := make([]byte, len(body))
	copy(res, body)
	return res, nil
}


