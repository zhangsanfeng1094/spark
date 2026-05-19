---
source: ai
confidence: medium
updated_at: 2026-05-18
derived_from:
  - README.md
  - docs/internal-architecture.md
  - go list ./...
---

# Architecture Rules

Spark is organized around a CLI shell that coordinates config, integrations, TUI workflows, skill management, usage data, and protocol compatibility.

Stable boundaries:
- `cmd/spark` is only the executable entry point and delegates to `internal/app`.
- `internal/app` owns command construction and workflow orchestration.
- `internal/config` owns persisted Spark and peer-agent config formats.
- `internal/integrations` owns configuring and launching external agents.
- `internal/compat` owns protocol translation and compatibility gateway behavior.
- `internal/tui` owns interactive terminal presentation and state transitions.
- `internal/skills` owns skill discovery, installation, registry, and peer sync.
- `internal/usage` owns token usage storage and summaries.
- `npm` owns package wrapper and platform package metadata.

Architecture constraints:
- Keep protocol translation out of individual integration runners when a shared `internal/compat` adapter can own it.
- Keep config format reads/writes inside `internal/config` unless a boundary already has an established helper.
- Keep command and TUI orchestration thin enough that business rules remain testable in lower packages.
- Treat existing docs as context, not authority, when code differs.

Open questions:
- Some compatibility migration docs describe target architecture as well as current code. Confirm current package names before following older migration prose.
