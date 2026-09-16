package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// SupportedProviders lists the standard providers initialized by default.
var SupportedProviders = []schemas.ModelProvider{
	schemas.OpenAI,
	schemas.Anthropic,
	schemas.Gemini,
	schemas.DeepSeek,
	schemas.Ollama,
	schemas.OpenRouter,
	schemas.Azure,
	schemas.Bedrock,
	schemas.Groq,
	schemas.Mistral,
	schemas.XAI,
	schemas.Cerebras,
	schemas.Cohere,
	schemas.Perplexity,
	schemas.VLLM,
	schemas.SGL,
}

// SparkAccount implements schemas.Account for Spark profile and dynamic provider routing.
type SparkAccount struct {
	mu        sync.RWMutex
	providers []schemas.ModelProvider
	configs   map[schemas.ModelProvider]*schemas.ProviderConfig
	keys      map[schemas.ModelProvider][]schemas.Key
}

// NewSparkAccount creates an initialized SparkAccount with default provider configs and dummy keys.
func NewSparkAccount() *SparkAccount {
	a := &SparkAccount{
		providers: make([]schemas.ModelProvider, len(SupportedProviders)),
		configs:   make(map[schemas.ModelProvider]*schemas.ProviderConfig, len(SupportedProviders)),
		keys:      make(map[schemas.ModelProvider][]schemas.Key, len(SupportedProviders)),
	}
	copy(a.providers, SupportedProviders)

	for _, p := range SupportedProviders {
		a.configs[p] = &schemas.ProviderConfig{
			NetworkConfig: schemas.NetworkConfig{
				DefaultRequestTimeoutInSeconds: 300,
				MaxRetries:                     3,
				RetryBackoffInitial:            500 * time.Millisecond,
				RetryBackoffMax:                5 * time.Second,
				AllowPrivateNetwork:            true,
			},
			ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{
				Concurrency: 16,
				BufferSize:  16,
			},
		}
		a.keys[p] = []schemas.Key{
			{
				ID:    string(p) + "-default",
				Name:  string(p) + "-default",
				Value: schemas.SecretVar{Val: "spark-key"},
			},
		}
	}
	return a
}

func (a *SparkAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := make([]schemas.ModelProvider, len(a.providers))
	copy(res, a.providers)
	return res, nil
}

func (a *SparkAccount) GetKeysForProvider(ctx context.Context, providerKey schemas.ModelProvider) ([]schemas.Key, error) {
	if ctx != nil {
		if k, ok := ctx.Value(schemas.BifrostContextKeyDirectKey).(schemas.Key); ok && k.Value.GetValue() != "" {
			return []schemas.Key{k}, nil
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if keys, ok := a.keys[providerKey]; ok && len(keys) > 0 {
		return keys, nil
	}
	return []schemas.Key{
		{
			ID:    string(providerKey) + "-default",
			Name:  string(providerKey) + "-default",
			Value: schemas.SecretVar{Val: "spark-key"},
		},
	}, nil
}

func (a *SparkAccount) GetConfigForProvider(providerKey schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cfg, ok := a.configs[providerKey]
	if !ok || cfg == nil {
		return &schemas.ProviderConfig{
			NetworkConfig: schemas.NetworkConfig{
				DefaultRequestTimeoutInSeconds: 300,
				MaxRetries:                     3,
				RetryBackoffInitial:            500 * time.Millisecond,
				RetryBackoffMax:                5 * time.Second,
				AllowPrivateNetwork:            true,
			},
			ConcurrencyAndBufferSize: schemas.ConcurrencyAndBufferSize{
				Concurrency: 16,
				BufferSize:  16,
			},
		}, nil
	}
	return cfg, nil
}

func (a *SparkAccount) SetProviderConfig(providerKey schemas.ModelProvider, cfg *schemas.ProviderConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.configs[providerKey] = cfg
}

func (a *SparkAccount) SetProviderKey(providerKey schemas.ModelProvider, apiKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(apiKey) == "" {
		apiKey = "spark-key"
	}
	a.keys[providerKey] = []schemas.Key{
		{
			ID:    string(providerKey) + "-active",
			Name:  string(providerKey) + "-active",
			Value: schemas.SecretVar{Val: apiKey},
		},
	}
}

// Engine wraps Bifrost client singleton and account configuration.
type Engine struct {
	client  *bifrost.Bifrost
	account *SparkAccount
	logger  schemas.Logger
}

// New creates and initializes a Bifrost Engine.
func New(ctx context.Context, logger schemas.Logger, plugins ...schemas.LLMPlugin) (*Engine, error) {
	if logger == nil {
		logger = bifrost.NewNoOpLogger()
	}
	account := NewSparkAccount()
	client, err := bifrost.Init(ctx, schemas.BifrostConfig{
		Account:         account,
		Logger:          logger,
		LLMPlugins:      plugins,
		InitialPoolSize: 16,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init bifrost core: %w", err)
	}
	return &Engine{
		client:  client,
		account: account,
		logger:  logger,
	}, nil
}

func (e *Engine) Client() *bifrost.Bifrost {
	return e.client
}

func (e *Engine) Account() *SparkAccount {
	return e.account
}

func (e *Engine) Logger() schemas.Logger {
	return e.logger
}

// ConfigureProvider sets the base URL and API key for a provider, then reloads it in Bifrost.
func (e *Engine) ConfigureProvider(providerKey schemas.ModelProvider, baseURL, apiKey string) error {
	return e.ConfigureProviderWithHeaders(providerKey, baseURL, apiKey, nil)
}

// ConfigureProviderWithHeaders sets the base URL, API key, and extra HTTP headers for a provider, then reloads it in Bifrost.
func (e *Engine) ConfigureProviderWithHeaders(providerKey schemas.ModelProvider, baseURL, apiKey string, extraHeaders map[string]string) error {
	cfg, err := e.account.GetConfigForProvider(providerKey)
	if err != nil {
		return err
	}
	newCfg := *cfg
	newCfg.NetworkConfig.BaseURL = baseURL
	newCfg.NetworkConfig.ExtraHeaders = extraHeaders
	e.account.SetProviderConfig(providerKey, &newCfg)
	e.account.SetProviderKey(providerKey, apiKey)
	return e.client.UpdateProvider(providerKey)
}
