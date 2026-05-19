---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/tui
  - scripts/tui-state-navigator.js
  - scripts/tui-state-map.json
---

# TUI Rules

Purpose:
Owns terminal interaction models and views.

Stable constraints:
- Maintain predictable model/update/view behavior.
- Preserve scripted navigation where `scripts/tui-state-map.json` covers a flow.
- Keep view logic separate from config format details and protocol translation.

Dependency constraints:
- May depend on config, integrations, and skills for displayed data and actions.
- Should not own CLI command parsing.

Vocabulary:
- `dashboard`: top-level interactive menu.
- `manager`: interactive flow for profiles, MCP servers, skills, or usage views.
- `state map`: scripted navigation map for TUI automation.

Open questions:
- Confirm desired scripted navigation updates with humans when UI order changes.
