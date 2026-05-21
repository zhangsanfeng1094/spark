---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - internal/integrations
  - docs/internal-architecture.md
  - README.md
---

# Integrations Summary

Purpose:
`internal/integrations` owns agent-specific launch and configuration behavior for supported coding agents.

Main concepts:
- `Runner`: launches an agent with profile/model/extra args.
- `Editor`: writes target agent configuration and exposes available models.
- Registry: lists supported integrations for CLI/TUI selection.
- `compatproxy` and `proxyutil`: local proxy runtime support and shared proxy utilities.

Collaborators:
- `internal/config` supplies profiles and persisted integration settings.
- `internal/compat/gateway` handles shared protocol compatibility routes.
- `internal/app` orchestrates launch/config commands.

Tests:
Existing integration tests should be checked for registry, launch-route, probe, and agent-specific config changes.

Unclear or inferred:
Some compatibility proxy responsibilities may continue moving toward `internal/compat`; verify current code before refactoring.
