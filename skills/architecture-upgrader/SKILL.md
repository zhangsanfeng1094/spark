---
name: architecture-upgrader
description: Analyze and upgrade codebase architecture at package/module boundaries. Use when Codex is asked to assess or refactor architectural structure, responsibility drift, dependency direction, "does this package make sense?", duplicated service shells, protocol/adapter layering, or incremental migration plans that must preserve behavior while improving boundaries.
---

# Architecture Upgrader

Use this skill to turn architecture concerns into a small, verified refactor. Favor evidence from the codebase over naming intuition.

## Activation Criteria

Use this skill for structural work, not routine cleanup. It fits when a change requires at least one of:

- splitting a package or module that mixes integrations, adapters, lifecycle orchestration, observability, compatibility behavior, or persistence
- changing dependency direction while preserving public contracts
- replacing duplicated service shells with shared infrastructure
- planning a staged migration where rollback and verification matter
- reviewing boundary drift before implementing a structural upgrade

Do not assume mixed responsibilities are wrong. First prove harm such as dependency cycles, unclear ownership, copy-paste extension cost, fragile lifecycle coupling, or test friction.

## Workflow

1. Map current responsibilities.
   - List files, exported APIs, callers, imports, runtime entrypoints, side effects, config/logging hooks, and tests for the target area.
   - Identify what the package actually owns today, not what its name implies.
   - Search for docs or architecture notes, but treat them as context when code differs.

2. State the boundary problem plainly.
   - Separate stable responsibilities from drift.
   - Name mixed concerns such as lifecycle, protocol translation, persistence, CLI orchestration, probing, logging, retry policy, and domain logic.
   - Mark whether the issue is naming only, dependency direction, duplicated infrastructure, or behavior coupling.

3. Define the target shape.
   - Keep domain-specific adapters near their client/format.
   - Centralize shared lifecycle, routing, logging, auth, process execution, persistence, or transport mechanics.
   - Prefer interfaces at the point of variation, not around every helper.
   - Preserve public entry points when callers do not need to change.
   - State desired dependency direction and any temporary bridge allowed during migration.

4. Slice the migration.
   - Start with no-behavior-change extraction.
   - Move shared infrastructure before changing behavior.
   - Keep compatibility wrappers during the first pass.
   - Give each slice a rollback point and a behavior-preservation claim.
   - Add tests for lifecycle, error paths, dependency direction, adapter conformance, logging/probing checks, and one representative end-to-end path.

5. Verify before claiming success.
   - Run package tests and broader tests affected by import changes.
   - Inspect dependency direction after edits.
   - Check that user-owned or unrelated worktree changes were not reverted.
   - If coverage is low, say which risk remains.

## Review Heuristics

Use [boundary-signals.md](references/boundary-signals.md) when deciding whether a package should be split, renamed, or left alone.

Strong architecture upgrade candidates usually show at least two of:

- One package owns both orchestration and low-level mechanics.
- Multiple files repeat listener/server/client/logging/persistence lifecycle.
- A package imports a lower-level translator or gateway while also being named as a high-level integration.
- Tests mostly cover conversion helpers but not lifecycle or failure modes.
- Adding a new variant would require copying an existing service shell.
- Callers import a large package for one small utility.

## Output Shape

For analysis-only requests, answer with:

- current boundary
- concrete problems
- target boundary
- migration slices
- risks and temporary exceptions
- verification plan
- open questions

For implementation requests, edit in the smallest safe slice and finish with changed files plus tests run.
