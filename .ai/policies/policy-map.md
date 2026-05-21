---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - docs/internal-architecture.md
  - go list ./...
  - skills/ai-native-project-skeleton/runtime/policy-matcher.md
---

# Policy Map

Match policies by path first, then by task intent.

| Path or task cue | Policy |
| --- | --- |
| `internal/app/**`, Cobra commands, launch/config/profile/MCP/skill commands | `cli-command.policy.md` |
| `internal/config/**`, profiles, MCP config, Codex TOML, Claude JSON, migrations | `config-persistence.policy.md` |
| `internal/integrations/**`, agent runners/editors, API probe, local proxy runtime | `integration-adapter.policy.md` |
| `internal/compat/**`, protocol translation, IR, gateway, policy, streaming, reasoning, tools | `compat-translation.policy.md` |
| `internal/tui/**`, Bubble Tea models, dashboard, profile/MCP/skill views | `tui-view.policy.md` |
| `internal/skills/**`, `skills/**`, skill registry/install/catalog/peer sync | `skill-system.policy.md` |
| `internal/usage/**`, token usage records, usage summaries | `usage-store.policy.md` |
| `npm/**`, `.goreleaser.yml`, release workflows, package publishing | `release-packaging.policy.md` |

Conflict rule:
- Nearest `.agent.md` and current code win over this map.
