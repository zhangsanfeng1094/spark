# Codex Model Catalog Integration

## Overview

Spark now supports reading Codex model catalog directly from the `models_cache.json` file, eliminating the need to run `codex debug prompt-input` subprocess for fetching model metadata and system prompts.

## Model Catalog File Location

Codex stores the model catalog in:
- Default: `~/.codex/models_cache.json`
- Custom: Set via `CODEX_HOME` environment variable (e.g., `$CODEX_HOME/models_cache.json`)

## File Format

The `models_cache.json` file contains:

```json
{
  "fetched_at": "2024-01-15T10:30:00Z",
  "etag": "abc123...",
  "client_version": "1.0.0",
  "models": [
    {
      "slug": "gpt-5.2",
      "display_name": "GPT-5.2",
      "description": "Latest GPT model",
      "base_instructions": "You are a helpful coding assistant...",
      "context_window": 272000,
      "max_context_window": 272000,
      "shell_type": "shell_command",
      "truncation_policy": {
        "mode": "bytes",
        "limit": 10000
      },
      "supported_reasoning_levels": [],
      "supports_parallel_tool_calls": false
    }
  ]
}
```

## API Functions

### `fetchCodexModelCatalog() ([]CodexModelInfo, error)`

Reads and parses the entire model catalog from `models_cache.json`.

**Returns:**
- Array of `CodexModelInfo` objects containing all available models
- Error if the file doesn't exist or is invalid

**Example:**
```go
models, err := fetchCodexModelCatalog()
if err != nil {
    log.Fatal(err)
}

for _, model := range models {
    fmt.Printf("Model: %s - %s\n", model.Slug, model.DisplayName)
    fmt.Printf("Instructions: %s\n", model.BaseInstructions)
}
```

### `fetchCodexModelInstructions(modelSlug string) (string, error)`

Fetches the `base_instructions` (system prompt) for a specific model.

**Parameters:**
- `modelSlug`: The model identifier (e.g., "gpt-5.2", "gpt-4o")
  - If empty, returns the first model's instructions
  - Supports prefix matching (e.g., "gpt-5" matches "gpt-5.2")

**Returns:**
- The `base_instructions` string for the matched model
- Error if model not found or catalog unavailable

**Example:**
```go
// Get specific model instructions
instructions, err := fetchCodexModelInstructions("gpt-5.2")
if err != nil {
    log.Fatal(err)
}

// Get default model instructions
defaultInstructions, err := fetchCodexModelInstructions("")
```

### `fetchCodexDefaultPrompt() (string, error)`

Fetches the default model's system prompt with automatic fallback.

**Behavior:**
1. First tries to read from `models_cache.json` (fast, no subprocess)
2. Falls back to `codex debug prompt-input` if cache unavailable (legacy method)

**Returns:**
- The system prompt string
- Error if both methods fail

**Example:**
```go
prompt, err := fetchCodexDefaultPrompt()
if err != nil {
    log.Fatal(err)
}
fmt.Println("Default prompt:", prompt)
```

### `getCodexHome() (string, error)`

Returns the Codex home directory path.

**Returns:**
- `$CODEX_HOME` if set
- `~/.codex` otherwise

## Integration with Prompt Manager

The model catalog functions are used in the prompt injection system:

- **Append Mode**: When a prompt preset uses `mode: append`, Spark fetches the original Codex system prompt and appends custom content
- **Replace Mode**: Custom prompt completely replaces the model's base instructions

See `resolveCodexAppendPrompt()` in `internal/integrations/codex.go` for implementation details.

## Custom Model Catalogs

You can specify a custom model catalog file in your Spark configuration:

```json
{
  "integrations": {
    "codex": {
      "model_catalog_json": "/path/to/custom_models.json"
    }
  }
}
```

This is passed to Codex at launch as:
```bash
codex -c model_catalog_json="/path/to/custom_models.json"
```

## Performance Benefits

**Before (Legacy Method):**
- Spawns `codex debug prompt-input` subprocess
- Parses full prompt JSON output
- ~100-500ms overhead per call

**After (File-Based Method):**
- Direct file read from `~/.codex/models_cache.json`
- Cached in memory after first read
- ~1-5ms overhead per call
- **50-100x faster** than subprocess method

## Troubleshooting

### Cache File Not Found

If `~/.codex/models_cache.json` doesn't exist:
1. Run Codex at least once to generate the cache
2. Or the system falls back to `codex debug prompt-input`
3. Check `CODEX_HOME` environment variable is set correctly

### Invalid Cache Format

If the cache file is corrupted:
```bash
# Regenerate by running Codex
codex

# Or manually delete and let Codex recreate it
rm ~/.codex/models_cache.json
```

### Custom Model Not Found

If your custom model isn't recognized:
1. Ensure it's in the `models_cache.json` or custom catalog
2. Check the `slug` field matches your query
3. Use prefix matching (e.g., "custom-" for "custom-model-v1")

## Testing

Run the test suite:
```bash
go test ./internal/integrations -v -run "TestFetchCodex"
```

Key test cases:
- Reading valid cache files
- Handling missing files
- Invalid JSON parsing
- Model lookup (exact and prefix match)
- Environment variable override
