---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/config
  - docs/internal-architecture.md
---

# Configuration Rules

Purpose:
Owns Spark and peer-agent configuration persistence.

Stable constraints:
- Profile, integration, model history, and MCP behavior should round-trip through load/save tests.
- Codex TOML and Claude JSON should remain format-aware.
- Backups should be preserved where existing helpers provide them.

Dependency constraints:
- Should not depend on CLI, TUI, integrations, or compatibility gateway packages.

Vocabulary:
- `RootConfig`: Spark's persisted configuration root.
- `Profile`: gateway endpoint/API key/model grouping.
- `McpServerConfig`: MCP server entry managed by Spark and peer sync.

Open questions:
- Confirm any schema migration strategy before removing legacy support.
