package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"spark/internal/auth"
	"spark/internal/config"
)

func FetchOpenAIModels(profile *config.Profile) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	return fetchModelsWithClient(profile, client)
}

func fetchModelsWithClient(profile *config.Profile, client *http.Client) ([]string, error) {
	if profile == nil {
		return nil, fmt.Errorf("profile is nil")
	}
	baseURL := strings.TrimSpace(profile.EffectiveEndpoint())
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	mode := profile.EffectiveCredentialMode()
	apiKey := ""
	if mode != config.CredentialModeAuth {
		apiKey = strings.TrimSpace(profile.EffectiveAPIKey())
	}
	authRef := strings.TrimSpace(profile.EffectiveAuthRef())
	authProv, _ := auth.ParseAuthRef(authRef)
	if apiKey == "" && mode != config.CredentialModeAPIKey {
		if store, err := auth.DefaultStore(); err == nil && store != nil {
			if authRef != "" {
				if rec, err := store.GetByRef(authRef); err == nil && rec != nil && rec.AccessToken != "" {
					apiKey = rec.AccessToken
				}
			}
			if apiKey == "" {
				lowerBase := strings.ToLower(baseURL)
				if strings.Contains(lowerBase, "commandcode") || strings.Contains(lowerBase, ":3050") {
					authProv = auth.ProviderCommandCode
				} else if strings.Contains(lowerBase, "anthropic.com") {
					authProv = auth.ProviderClaude
				} else if strings.Contains(lowerBase, "openai.com") {
					authProv = auth.ProviderCodex
				} else if strings.Contains(lowerBase, "googleapis.com") {
					authProv = auth.ProviderGemini
				}
				if authProv != "" {
					if rec, err := store.Get(authProv); err == nil && rec != nil && rec.AccessToken != "" {
						apiKey = rec.AccessToken
					}
				}
			}
		}
	}

	modelListURL := strings.TrimSpace(profile.ModelListURL)
	proto := profile.EffectiveProtocol()
	if proto == config.ProtocolAnthropic || config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		if modelListURL != "" {
			return fetchAnthropicModelsFromURLWithClient(modelListURL, apiKey, client)
		}
		return fetchAnthropicModelsWithClient(baseURL, apiKey, client)
	}

	lowerBase := strings.ToLower(baseURL)
	isCommandCode := authProv == auth.ProviderCommandCode || strings.Contains(lowerBase, "commandcode") || strings.Contains(lowerBase, ":3050")

	if isCommandCode {
		if modelListURL != "" {
			return fetchOpenAIModelsFromURLWithClient(modelListURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
		}
		if strings.Contains(lowerBase, "commandcode.ai") {
			return fetchOpenAIModelsFromURLWithClient("https://api.commandcode.ai/provider/v1/models", apiKey, "", "", client)
		}
		// Try local proxy endpoint first (e.g. localhost:3050/v1/models)
		models, err := fetchOpenAIModelsWithClient(baseURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
		if err == nil && len(models) > 0 {
			return models, nil
		}
		// Fallback to upstream Command Code API if local proxy is not running or failed
		if apiKey != "" {
			if cloudModels, cloudErr := fetchOpenAIModelsFromURLWithClient("https://api.commandcode.ai/provider/v1/models", apiKey, "", "", client); cloudErr == nil && len(cloudModels) > 0 {
				return cloudModels, nil
			}
		}
		if cat, ok := auth.CatalogFor(auth.ProviderCommandCode); ok && len(cat.SuggestedModels) > 0 {
			return cat.SuggestedModels, nil
		}
		if err != nil {
			return nil, err
		}
		return models, nil
	}

	var models []string
	var err error
	if modelListURL != "" {
		models, err = fetchOpenAIModelsFromURLWithClient(modelListURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
	} else {
		models, err = fetchOpenAIModelsWithClient(baseURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
	}
	if (err != nil || len(models) == 0) && authProv != "" {
		if cat, ok := auth.CatalogFor(authProv); ok && len(cat.SuggestedModels) > 0 {
			return cat.SuggestedModels, nil
		}
	}
	return models, err
}

func fetchOpenAIModelsWithClient(baseURL, apiKey, org, project string, client *http.Client) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("empty base url")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	return fetchOpenAIModelsFromURLWithContext(ctx, base+"/models", apiKey, org, project, client)
}

func fetchOpenAIModelsFromURLWithClient(modelListURL, apiKey, org, project string, client *http.Client) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return fetchOpenAIModelsFromURLWithContext(ctx, modelListURL, apiKey, org, project, client)
}

func fetchOpenAIModelsFromURLWithContext(ctx context.Context, modelListURL, apiKey, org, project string, client *http.Client) ([]string, error) {
	if strings.TrimSpace(modelListURL) == "" {
		return nil, fmt.Errorf("empty models url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(modelListURL), nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if org != "" {
		req.Header.Set("OpenAI-Organization", org)
	}
	if project != "" {
		req.Header.Set("OpenAI-Project", project)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models request failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	models, err := parseOpenAIModelsResponse(body)
	if err != nil {
		return nil, err
	}
	sort.Strings(models)
	return models, nil
}

func fetchAnthropicModelsWithClient(baseURL, apiKey string, client *http.Client) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("empty base url")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	path := "/v1/models"
	if strings.HasSuffix(base, "/v1") {
		path = "/models"
	}
	return fetchAnthropicModelsFromURLWithContext(ctx, base+path, apiKey, client)
}

func fetchAnthropicModelsFromURLWithClient(modelListURL, apiKey string, client *http.Client) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return fetchAnthropicModelsFromURLWithContext(ctx, modelListURL, apiKey, client)
}

func fetchAnthropicModelsFromURLWithContext(ctx context.Context, modelListURL, apiKey string, client *http.Client) ([]string, error) {
	requestURL := strings.TrimSpace(modelListURL)
	if requestURL == "" {
		return nil, fmt.Errorf("empty models url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("API source does not support Anthropic model listing at %s; add model IDs manually or use a source that supports GET /v1/models", requestURL)
		}
		return nil, fmt.Errorf("models request failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	models, err := parseOpenAIModelsResponse(body)
	if err != nil {
		return nil, err
	}
	sort.Strings(models)
	return models, nil
}

func parseOpenAIModelsResponse(body []byte) ([]string, error) {
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	models := make([]string, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		models = append(models, id)
	}
	return config.NormalizeModels(models), nil
}
