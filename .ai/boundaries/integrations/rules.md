---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/integrations
  - docs/internal-architecture.md
---

# Integrations Rules

Purpose:
Owns agent-specific configuration and launch behavior.

Stable constraints:
- Runners launch agents; editors modify agent-specific configuration.
- Registry names must stay aligned with CLI/TUI selection.
- Gateway probing and local proxy startup must preserve diagnostic logging.

Dependency constraints:
- May use `internal/config` and `internal/compat/gateway`.
- Should not reimplement shared protocol translation outside `internal/compat`.

Vocabulary:
- `Runner`: integration that launches directly.
- `Editor`: integration that writes config before launch.
- `compatproxy`: local proxy runtime used when upstream APIs need translation.

Open questions:
- Keep checking whether compatibility runtime should stay in integrations or continue moving toward `internal/compat/gateway`.
