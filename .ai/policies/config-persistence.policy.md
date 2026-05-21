---
source: ai
confidence: medium
role: config-persistence
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/config
  - docs/internal-architecture.md
---

# Config Persistence Policy

Role:
Files that load, normalize, migrate, merge, and save Spark or peer-agent configuration.

Detected from:
- `internal/config/config.go`, `mcp.go`, `toml.go`, `claude_json.go`, `files.go`, `migrate.go`.

Rules:
- Preserve Spark config compatibility unless the task explicitly changes the schema.
- Keep Codex TOML and Claude JSON logic format-aware.
- Use existing backup/write helpers where available.
- Pair schema or normalization changes with tests.

Non-rules:
- TUI display choices do not belong in config persistence.

Matching:
- `internal/config/**`
- Tasks mentioning profiles, MCP servers, Codex TOML, Claude JSON, migration, load/save.

Review notes:
- Watch for silent data loss when merging imported MCP servers or rewriting peer config files.
