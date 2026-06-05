# Prompt Manager Implementation

## Overview

Prompt Manager manages reusable prompt preset files and binds them to integrations. At launch time, Spark resolves the active integration and model against the configured bindings and injects a prompt only when the global prompt switch and the matched binding are both enabled.

It also owns Codex prompt-adjacent launch metadata, including the optional Codex model catalog path used to load custom model metadata before prompt injection starts.

The current entry points are the interactive dashboard option `Manage prompts`, the `spark prompt` command, and the non-interactive debug renderer `spark debug snapshot prompt`.

## Data Model

Prompt configuration lives under `RootConfig.Prompts`:

- `enabled`: global prompt injection switch. Missing values default to `false`; users must explicitly enable prompt injection.
- `presets`: named prompt file definitions. Presets own the injection `mode`.
- `bindings`: integration/model bindings that point to a preset.

A binding has this meaning:

```text
integration + model -> preset + enabled
```

`PromptBinding.Mode` remains as a legacy override for older configs. New TUI edits no longer expose binding mode. Effective mode is resolved as `binding.mode` when present, otherwise `preset.mode`.

Codex custom model catalog configuration lives under `RootConfig.Integrations["codex"].ModelCatalogJSON` and is edited from the Prompt Manager Settings section. Spark passes it to Codex at launch as `-c model_catalog_json="..."`.

Minimal JSON shape:

```json
{
  "prompts": {
    "enabled": true,
    "presets": {
      "coding": {
        "name": "coding",
        "description": "Coding style",
        "file": "prompts/coding.md",
        "mode": "append"
      }
    },
    "bindings": [
      {
        "integration": "codex",
        "model": "*",
        "preset": "coding",
        "enabled": true
      }
    ]
  }
}
```

Minimal integration settings shape:

```json
{
  "integrations": {
    "codex": {
      "model_catalog_json": "/home/user/.codex/custom_models.json"
    }
  }
}
```

Codex expects the custom catalog file to deserialize as a `ModelsResponse`, whose top-level JSON object contains a `models` array. Each element is a Codex `ModelInfo` object using snake_case field names. This matches the upstream Codex `codex-rs/protocol/src/openai_models.rs` `ModelsResponse { models: Vec<ModelInfo> }` definition and the bundled catalog loader in `codex-rs/models-manager/src/lib.rs`.

Minimal catalog shape:

```json
{
  "models": [
    {
      "slug": "glm-5.1",
      "display_name": "GLM 5.1",
      "description": "Custom GLM model metadata",
      "supported_reasoning_levels": [],
      "shell_type": "shell_command",
      "visibility": "list",
      "supported_in_api": true,
      "priority": 10,
      "availability_nux": null,
      "upgrade": null,
      "base_instructions": "You are a helpful coding assistant.",
      "model_messages": null,
      "supports_reasoning_summaries": false,
      "default_reasoning_summary": "auto",
      "support_verbosity": false,
      "default_verbosity": null,
      "apply_patch_tool_type": null,
      "web_search_tool_type": "text",
      "truncation_policy": {
        "mode": "bytes",
        "limit": 10000
      },
      "supports_parallel_tool_calls": false,
      "supports_image_detail_original": false,
      "context_window": 272000,
      "max_context_window": 272000,
      "auto_compact_token_limit": null,
      "effective_context_window_percent": 95,
      "experimental_supported_tools": [],
      "input_modalities": ["text", "image"],
      "supports_search_tool": false,
      "use_responses_lite": false
    }
  ]
}
```

Supported integration values are `codex` and `claude`. Supported modes are `append` and `replace`. Missing binding `enabled` values normalize to `true`, but the global `enabled` switch is still the runtime gate.

## Binding Resolution

Bindings support exact model names and the wildcard model `*`. Resolution priority is fixed:

1. `integration + exact model`
2. `integration + *`

Wildcard bindings do not cross integration boundaries. Duplicate validation is by `integration + model`, so `codex/gpt-5` and `codex/*` can coexist, but two `codex/*` bindings cannot.

`ResolvePromptInjection(integration, model)` is the launch-time gate:

1. If the root config is nil or global prompts are disabled, return no injection.
2. Normalize the integration and requested model.
3. Resolve the best binding using exact-first, wildcard-second matching.
4. If the matched binding is disabled, return no injection without reading the preset file.
5. Read and validate the preset file.
6. Return a `PromptInjection` containing effective mode, resolved path, and file content.

## Validation Rules

Normalization and validation have different responsibilities:

- Normalization trims and canonicalizes preset names, integration names, binding models, prompt modes, and missing enabled flags.
- Old configs with binding-owned modes are migrated toward preset-owned modes. If all bindings using a preset have the same mode, that mode is promoted to the preset and matching overrides are cleared. Conflicts leave differing binding modes as legacy overrides and default the preset to `append`.
- `CheckPrompts()` returns structured issues with `Severity`, `Active`, and `Message`.
- `ValidatePrompts()` reports blocking errors only for active injection paths.
- `ValidatePromptConfigStrict()` reports inactive config issues too, for maintenance and tests.

Disabled global injection and disabled bindings do not block runtime validation for missing or empty prompt files. They still appear as inactive warnings in structured checks.

Prompt preset file paths are constrained:

- `~/...` expands against the user home directory and must remain under home after cleaning.
- Absolute paths must remain under the user home directory.
- Relative paths resolve under the Spark config directory, usually `~/.spark`, and must not escape it with `..`.
- Empty paths, home-external absolute paths, and cleaned relative escapes are rejected with an error that names the allowed ranges.

## TUI Behavior

The Prompt Manager TUI has three sections: Presets, Bindings, and Settings.

Preset rows show preset names. Binding rows show the effective launch target and state:

```text
integration · model · MODE · ON/OFF
```

Wildcard models display as `all models` in summaries. Binding details show `Effective Mode`; if a legacy binding mode override exists, details also show `Mode Override`.

Settings contains `Codex Catalog`, an optional path to a Codex custom model catalog JSON. The field is global for the Codex integration rather than per prompt preset or per provider profile.

The TUI edits an in-memory draft. These actions mark the draft dirty but do not write the config file immediately:

- `Space`: toggle global prompt injection on/off.
- `T`: toggle the selected binding when the Bindings section is selected.
- `A`: add in the current section.
- `E` or `Enter`: edit the current item.
- `D` or `X`: delete the current item after confirmation.
- Copy preset.

Use `S` or main-screen `F2` to save the draft. Dirty state appears in the header/status as `* Unsaved changes`. Editor `F2` applies the form into the draft; it does not save to disk. Quitting with unsaved changes asks whether to save, discard, or cancel.

Add/Edit Preset fields are `Name`, `Description`, `File`, and `Mode`. Add/Edit Binding fields are `Integration`, `Model`, `Preset`, and `Enabled`; `Model` may be `*`.

`V` validates and reports active errors separately from inactive warnings. Select fields show a `▼` marker and open a centered select modal with `Enter` or `Space`.

## Debug Snapshots

Prompt Manager snapshots are rendered with:

```sh
go run ./cmd/spark debug snapshot prompt --state <state> --width 120 --height 32
```

Supported prompt snapshot states include:

- `overview` or `presets`: default preset-focused view.
- `disabled`: global prompt injection disabled view.
- `bindings`: binding-focused view.
- `binding-disabled`: binding-focused view with the first binding disabled.
- `add-preset`: add preset editor.
- `add-binding`: add binding editor.
- `edit-current`: edit the current item.
- `error`: validation error display state.

## Testing

Recommended verification commands:

```sh
go test ./internal/config ./internal/tui
go test ./...
go run ./cmd/spark debug snapshot prompt --state bindings --width 120 --height 32
go run ./cmd/spark debug snapshot prompt --state add-binding --width 120 --height 32
go run ./cmd/spark debug snapshot prompt --state disabled --width 120 --height 32
```

Important coverage points:

- Missing global `prompts.enabled` defaults disabled.
- Binding `model="*"` matches any model for the same integration.
- Exact bindings win over wildcard bindings.
- Preset mode controls injection mode unless a legacy binding mode override exists.
- Path resolution rejects relative escapes and home-external absolute paths.
- Runtime validation skips inactive file issues while strict validation still reports them.
- Prompt Manager mutations are draft-only until `S` or main-screen `F2` saves.
