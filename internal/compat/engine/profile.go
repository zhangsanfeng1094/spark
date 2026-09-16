package engine

import (
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
	"spark/internal/config"
)

// CleanBaseURL trims trailing slashes and common `/v1` suffixes because Bifrost providers
// (OpenAI, Anthropic, etc.) append `/v1/...` to the configured BaseURL.
func CleanBaseURL(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	u = strings.TrimRight(u, "/")
	// If the user supplied "https://api.openai.com/v1" or "http://localhost:8080/v1",
	// strip "/v1" so provider appending "/v1/chat/completions" works properly.
	if strings.HasSuffix(u, "/v1") {
		u = strings.TrimSuffix(u, "/v1")
		u = strings.TrimRight(u, "/")
	}
	return u
}

// MapProfileToProvider determines the best Bifrost ModelProvider, cleaned BaseURL, and effective API key.
func MapProfileToProvider(profile *config.Profile) (schemas.ModelProvider, string, string) {
	return MapProfileAndModelToProvider(profile, "")
}

// MapProfileAndModelToProvider determines the best Bifrost ModelProvider, cleaned BaseURL, and effective API key,
// considering the requested model (e.g. Command Code routing Claude models via Anthropic Messages protocol).
func MapProfileAndModelToProvider(profile *config.Profile, modelName string) (schemas.ModelProvider, string, string) {
	if profile == nil {
		return schemas.OpenAI, "", ""
	}
	apiKey := profile.EffectiveAPIKey()
	baseURL := profile.OpenAIBaseURL
	if strings.TrimSpace(profile.AnthropicBaseURL) != "" &&
		(config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) ||
			strings.TrimSpace(baseURL) == "") {
		baseURL = profile.AnthropicBaseURL
	}

	lowerBase := strings.ToLower(baseURL)
	isCommandCode := strings.Contains(lowerBase, "commandcode") ||
		strings.Contains(lowerBase, "command-code") ||
		strings.Contains(lowerBase, ":3050") ||
		strings.EqualFold(profile.AuthProvider, "commandcode") ||
		strings.Contains(strings.ToLower(profile.EffectiveAuthRef()), "commandcode")

	// If Command Code, normalize endpoint to Command Code Cloud API
	if isCommandCode {
		if strings.TrimSpace(baseURL) == "" || strings.Contains(lowerBase, ":3050") {
			baseURL = "https://api.commandcode.ai/provider/v1"
		}
		cleanedBase := CleanBaseURL(baseURL)
		normModel := strings.ToLower(strings.TrimSpace(modelName))
		if normModel == "" {
			normModel = strings.ToLower(strings.TrimSpace(profile.DefaultModel))
		}
		// Command Code requires /provider/v1/messages (Anthropic format) for Claude models
		if strings.HasPrefix(normModel, "claude-") {
			return schemas.Anthropic, cleanedBase, apiKey
		}
		return schemas.OpenAI, cleanedBase, apiKey
	}

	cleanedBase := CleanBaseURL(baseURL)

	isCodexAuth := strings.EqualFold(profile.AuthProvider, "codex") ||
		strings.HasPrefix(strings.ToLower(profile.EffectiveAuthRef()), "codex") ||
		strings.Contains(lowerBase, "chatgpt.com")
	if isCodexAuth {
		if strings.TrimSpace(baseURL) == "" || baseURL == "https://api.openai.com/v1" {
			baseURL = "https://chatgpt.com/backend-api/codex"
			cleanedBase = CleanBaseURL(baseURL)
		}
		return schemas.OpenAI, cleanedBase, apiKey
	}

	isGrokAuth := strings.EqualFold(profile.AuthProvider, "grok") ||
		strings.HasPrefix(strings.ToLower(profile.EffectiveAuthRef()), "grok")
	if isGrokAuth {
		if strings.TrimSpace(baseURL) == "" || baseURL == "https://api.x.ai/v1" {
			baseURL = "https://cli-chat-proxy.grok.com/v1"
			cleanedBase = CleanBaseURL(baseURL)
		}
		return schemas.XAI, cleanedBase, apiKey
	}

	// Check provider by explicit API type or baseURL heuristics
	switch {
	case config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeAnthropicMessages) || strings.Contains(lowerBase, "anthropic.com"):
		return schemas.Anthropic, cleanedBase, apiKey
	case config.SupportsOpenAIAPIType(profile.OpenAIAPIType, config.OpenAIAPITypeGeminiGenerateContent) || strings.Contains(lowerBase, "googleapis.com"):
		return schemas.Gemini, cleanedBase, apiKey
	case strings.Contains(lowerBase, "deepseek.com"):
		return schemas.DeepSeek, cleanedBase, apiKey
	case strings.Contains(lowerBase, "openrouter.ai"):
		return schemas.OpenRouter, cleanedBase, apiKey
	case strings.Contains(lowerBase, "x.ai") || strings.Contains(lowerBase, "grok.com"):
		return schemas.XAI, cleanedBase, apiKey
	case strings.Contains(lowerBase, "groq.com"):
		return schemas.Groq, cleanedBase, apiKey
	case strings.Contains(lowerBase, "mistral.ai"):
		return schemas.Mistral, cleanedBase, apiKey
	case strings.Contains(lowerBase, "cerebras.ai"):
		return schemas.Cerebras, cleanedBase, apiKey
	case strings.Contains(lowerBase, "cohere.ai"):
		return schemas.Cohere, cleanedBase, apiKey
	case strings.Contains(lowerBase, "perplexity.ai"):
		return schemas.Perplexity, cleanedBase, apiKey
	case strings.Contains(lowerBase, "localhost:11434") || strings.Contains(lowerBase, "127.0.0.1:11434"):
		return schemas.Ollama, cleanedBase, apiKey
	default:
		return schemas.OpenAI, cleanedBase, apiKey
	}
}

// ResolveProfileModel maps a client-native model id onto a name the selected
// Spark profile actually serves. AGY Gemini mode rewrites Spark aliases such as
// gemini-3.7-flash-high into Google ids like gemini-3.7-flash; gateways that
// only list the alias then return "unknown provider for model ...".
func ResolveProfileModel(requested string, profile *config.Profile, preferred string) string {
	requested = strings.TrimPrefix(strings.TrimSpace(requested), "models/")
	preferred = strings.TrimSpace(preferred)

	candidates := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(config.NormalizeModel(name))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, name)
	}
	add(preferred)
	for _, name := range config.EffectiveProfileModels(profile) {
		add(name)
	}

	if requested == "" {
		if len(candidates) > 0 {
			return candidates[0]
		}
		return ""
	}

	reqNorm := strings.ToLower(config.NormalizeModel(requested))
	for _, candidate := range candidates {
		if strings.ToLower(config.NormalizeModel(candidate)) == reqNorm {
			return candidate
		}
	}

	reqFamily := modelFamily(reqNorm)
	if reqFamily != "" {
		for _, candidate := range candidates {
			if modelFamily(strings.ToLower(config.NormalizeModel(candidate))) == reqFamily {
				return candidate
			}
		}
	}
	return requested
}

var modelFamilySuffixes = []string{
	"-extra-low",
	"-non-reasoning",
	"-reasoning",
	"-thinking",
	"-preview",
	"-latest",
	"-agent",
	"-high",
	"-medium",
	"-low",
}

func modelFamily(model string) string {
	for {
		trimmed := false
		for _, suffix := range modelFamilySuffixes {
			if strings.HasSuffix(model, suffix) {
				model = strings.TrimSuffix(model, suffix)
				trimmed = true
				break
			}
		}
		if !trimmed {
			return model
		}
	}
}

// BindDirectKey attaches a direct schemas.Key to the BifrostContext if an API key is provided.
func BindDirectKey(ctx *schemas.BifrostContext, providerKey schemas.ModelProvider, apiKey string) {
	if ctx == nil || strings.TrimSpace(apiKey) == "" {
		return
	}
	ctx.SetValue(schemas.BifrostContextKeyDirectKey, schemas.Key{
		ID:    string(providerKey) + "-direct",
		Name:  string(providerKey) + "-direct",
		Value: schemas.SecretVar{Val: apiKey},
	})
}
