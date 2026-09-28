# Codex Model Catalog Integration

## Overview

Spark supports passing a custom Codex model catalog to Codex at launch. The catalog
path is stored in the Spark configuration (`integrations.codex.model_catalog_json`)
and passed as `-c model_catalog_json=...`, letting Codex load custom model metadata
without relying on its built-in `models_cache.json`.

## Configuration

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

The field can also be edited from the TUI (Manage settings → Codex model catalog JSON).
An empty value is simply omitted, so Codex keeps its default catalog behavior.

## Launch Args

`argsWithIntegration()` in `internal/integrations/codex.go` appends the catalog
override only when `ModelCatalogJSON` is set:

```go
if integration != nil && strings.TrimSpace(integration.ModelCatalogJSON) != "" {
    cmdArgs = append(cmdArgs, "-c", fmt.Sprintf("model_catalog_json=%q", strings.TrimSpace(integration.ModelCatalogJSON)))
}
```

## Catalog File Format

Codex model catalogs follow the `models_cache.json` shape:

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
      "context_window": 272000
    }
  ]
}
```

## Testing

Run the test suite:
```bash
go test ./internal/integrations -v -run "TestCodexModelCatalogArgs"
```
