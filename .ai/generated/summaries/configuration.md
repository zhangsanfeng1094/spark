---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/config
  - docs/internal-architecture.md
---

# Configuration Summary

Purpose:
`internal/config` owns Spark config loading, saving, normalization, migration, profiles, integrations, MCP servers, and peer-agent config formats.

Main files:
- `config.go`: root config, profiles, integrations, model history, load/save/normalize.
- `mcp.go`: MCP server add/remove/enable/disable/merge behavior.
- `toml.go`: Codex TOML support.
- `claude_json.go`: Claude JSON support.
- `files.go`: file write helpers.
- `migrate.go`: legacy config migration.

Collaborators:
- `internal/app` calls config workflows.
- `internal/integrations` consumes profiles and integration config.
- `internal/tui` presents config-backed managers.

Tests:
Existing config tests cover config, TOML, and Claude JSON behavior. Schema or merge changes should add targeted tests.

Unclear or inferred:
Backup semantics should be verified in code before relying on them in a migration.
