---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/tui
  - scripts/tui-state-navigator.js
  - scripts/tui-state-map.json
  - docs/internal-architecture.md
---

# TUI Summary

Purpose:
`internal/tui` owns terminal views and interaction helpers for dashboard, profile management, MCP management, skill management, token usage, prompts, and model connection flows.

Collaborators:
- `internal/app` enters TUI flows.
- `internal/config`, `internal/integrations`, and `internal/skills` provide data and actions.
- `scripts/tui-state-navigator.js` and `scripts/tui-state-map.json` support scripted navigation checks.

Tests:
Existing TUI tests cover dashboard, managers, profile actions/views, token usage, MCP, and skill manager behavior.

Unclear or inferred:
Not every visual state was reviewed during this initialization; check nearest tests for changed views.
