---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - docs/internal-architecture.md
  - go list ./...
---

# Dependency Rules

Observed dependency direction:

```text
cmd/spark -> internal/app
internal/app -> internal/config, internal/integrations, internal/skills, internal/tui, internal/usage, internal/version
internal/tui -> internal/config, internal/integrations, internal/skills
internal/integrations -> internal/config, internal/compat/gateway
internal/compat/client/* -> internal/compat/ir, internal/compat/target/openai_chat
internal/compat/gateway -> internal/compat/client/*, internal/compat/target/openai_chat
internal/compat/target/openai_chat -> internal/compat/ir, internal/compat/policy
internal/compat/policy -> internal/compat/ir
```

Rules:
- Do not introduce imports from lower-level packages back into `internal/app`.
- Do not make `internal/config` depend on TUI, app commands, or integration runners.
- Do not make `internal/compat/ir` depend on client, gateway, target, or integration packages.
- Keep `internal/compat/policy` independent of concrete HTTP handlers.
- Keep npm packaging scripts outside Go package dependency decisions.

Review when:
- A change creates an import cycle.
- A compatibility change touches both `internal/integrations` and `internal/compat`.
- A config change reaches into peer-agent files directly outside `internal/config`.
