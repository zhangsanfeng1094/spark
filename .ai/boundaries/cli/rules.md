---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/app
  - cmd/spark/main.go
  - docs/internal-architecture.md
---

# CLI Rules

Purpose:
Owns `spark` command construction and top-level workflows.

Stable constraints:
- `cmd/spark/main.go` should remain a thin executable entry.
- `internal/app` may orchestrate config, integrations, skills, TUI, usage, and version packages.
- Command parsing must preserve args after `--` for the selected integration.

Dependency constraints:
- Allowed to depend on internal app-facing packages.
- Should not own protocol translation, config file format details, or TUI rendering internals.

Vocabulary:
- `launch`: configure and run an integration.
- `config`: configure an integration without necessarily launching it.
- `profile`: OpenAI-compatible gateway profile.
- `MCP transfer`: import/export/sync MCP servers with peer agents.

Open questions:
- None for v1 metadata.
