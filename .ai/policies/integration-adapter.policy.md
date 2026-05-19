---
source: ai
confidence: medium
role: integration-adapter
status: draft
updated_at: 2026-05-18
derived_from:
  - internal/integrations
  - docs/internal-architecture.md
---

# Integration Adapter Policy

Role:
Files that configure or launch external coding agents and wire local compatibility proxy runtime.

Detected from:
- `internal/integrations/types.go` Runner/Editor interfaces.
- Agent-specific integration files and registry.
- `compatproxy/` and `proxyutil/` helpers.

Rules:
- Keep agent launch/config behavior separate from protocol translation internals.
- Preserve environment, profile, model, and pass-through arg handling.
- Register new integrations consistently through the existing registry.
- Keep probe and proxy logging useful for diagnosing upstream API behavior.

Non-rules:
- Do not put reusable protocol mapping rules here when `internal/compat` can own them.

Matching:
- `internal/integrations/**`
- Tasks mentioning agent launch, runner/editor, API probe, local proxy runtime.

Review notes:
- Test launch-route and proxy behavior when changing gateway detection or fallback rules.
