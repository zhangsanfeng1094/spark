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
	baseURL := strings.TrimSpace(profile.OpenAIBaseURL)
	if config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) && strings.TrimSpace(profile.AnthropicBaseURL) != "" {
		baseURL = strings.TrimSpace(profile.AnthropicBaseURL)
	}
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	apiKey := strings.TrimSpace(profile.OpenAIAPIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(profile.AnthropicAuthToken)
	}
	modelListURL := strings.TrimSpace(profile.ModelListURL)
	if config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) {
		if modelListURL != "" {
			return fetchAnthropicModelsFromURLWithClient(modelListURL, apiKey, client)
		}
		return fetchAnthropicModelsWithClient(baseURL, apiKey, client)
	}
	if modelListURL != "" {
		return fetchOpenAIModelsFromURLWithClient(modelListURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
	}
	return fetchOpenAIModelsWithClient(baseURL, apiKey, strings.TrimSpace(profile.OpenAIOrg), strings.TrimSpace(profile.OpenAIProject), client)
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
