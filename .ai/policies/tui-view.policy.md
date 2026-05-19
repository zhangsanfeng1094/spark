---
source: ai
confidence: medium
role: tui-view
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/tui
  - scripts/tui-state-navigator.js
  - docs/internal-architecture.md
---

# TUI View Policy

Role:
Files that implement terminal UI models, views, updates, prompts, and interactive managers.

Detected from:
- `internal/tui/**`
- TUI tests and state navigator script.

Rules:
- Follow existing Bubble Tea model/update/view patterns.
- Keep layout and state transitions deterministic enough for tests and scripted navigation.
- Keep persistence behavior delegated to app/config/skills/integration packages unless an existing view helper owns it.

Non-rules:
- Do not use TUI tests as a substitute for config or compatibility protocol tests.

Matching:
- `internal/tui/**`
- Tasks mentioning dashboard, profile manager, MCP manager, skill manager, token usage view, prompts.

Review notes:
- Update `scripts/tui-state-map.json` only when scripted navigation expectations intentionally change.
