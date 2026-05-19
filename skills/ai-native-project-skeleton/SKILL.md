---
name: ai-native-project-skeleton
description: Use when deriving project-specific high-level code abstractions and repository intelligence from an existing codebase, including sparse .agent.md anchors, detached .ai semantic context, inferred architecture boundaries, generated policy maps, drift checks, and progressive context disclosure.
---

# AI-Native Project Skeleton

Use this skill to derive and maintain repository intelligence: a lightweight, project-specific abstraction layer that helps humans and agents understand a codebase without turning metadata into a second source of truth.

## Core Contract

Code is authoritative. Metadata explains, constrains, summarizes, and routes context, but it must never overrule the codebase.

Prefer:

- sparse `.agent.md` files only at meaningful architecture boundaries
- detached semantic knowledge under `.ai/`
- project-derived rules and policies
- generated artifacts marked with source and confidence
- small index artifacts for default lookup, with large raw artifacts kept tool-only
- context assembled progressively from the current task

Avoid:

- metadata in every source directory
- giant architecture documents
- duplicating code truth in prose
- injecting full repository context into prompts
- injecting full graphs or raw generated artifacts into prompts
- treating AI inference as guaranteed truth

## Standard Workflow

1. Inspect the repository structure and existing conventions before creating files.
2. Infer the repository's own architecture vocabulary from paths, imports, build files, tests, and naming patterns.
3. Add `.agent.md` only at those boundaries, using `templates/agent-anchor.template.md`.
4. Create or update `.ai/` with derived abstractions, rules, policy maps, generated artifacts, and evolution notes.
5. Match policies by file role and task type using `runtime/policy-matcher.md`.
6. Generate small index artifacts first; create raw/full artifacts only when a tool can query them selectively.
7. Assemble only the minimum useful context using `runtime/context-assembly.md`.
8. For changes over time, use `runtime/incremental-update.md` to refresh generated artifacts and flag drift.

## Context Priority

When assisting on a code task, resolve context in this order:

1. Current task
2. Nearest `.agent.md`
3. Matching semantic policies
4. Boundary rules
5. Global architecture rules
6. Generated summaries and local graph indexes

Nearest context wins when guidance conflicts. If metadata conflicts with code, treat metadata as stale and report the mismatch.

## When To Load References

- For anchor discovery and context lookup, read `runtime/context-resolver.md`.
- For discovering file roles and policy matching, read `runtime/policy-matcher.md`.
- For final prompt/context construction, read `runtime/context-assembly.md`.
- For keeping `.ai/generated` synchronized with code, read `runtime/incremental-update.md`.
- For bootstrapping a new module or repo, read `prompts/module-bootstrap.prompt.md`.
- For architecture review, read `prompts/architecture-review.prompt.md`.
- For refactor planning, read `prompts/refactor-guidance.prompt.md`.
- For dependency validation, read `prompts/dependency-check.prompt.md`.
- For semantic summaries, read `prompts/semantic-summary.prompt.md`.

## Repository Intelligence Layout

Use this shape as a neutral semantic layer. Generate concrete names from the repository being analyzed.

```text
.ai/
  rules/
    architecture.md
    dependency-rules.md
    style-preferences.md
    ai-behavior.md
  policies/
    policy-map.md
    <project-role>.policy.md
  boundaries/
    <inferred-boundary>/
      rules.md
      decisions/
      examples/
      notes.md
  generated/
    architecture-map.md
    dependency-graph.index.json
    module-graph.index.json
    summaries/
    raw/
      dependency-graph.full.json
      module-graph.full.json
  history/
    architecture-evolution.md
    recurring-patterns.md
    anti-patterns.md
```

Do not impose a universal architecture model. Generate policies only from roles and boundaries that are visible in the target project.

Only `*.index.json`, `architecture-map.md`, and selected summaries are default context candidates. Files under `.ai/generated/raw/` are optional tool-query assets and must not be loaded into prompts by default.

## Output Expectations

When modifying a repository, provide:

- files created or changed
- boundaries, roles, layers, or other abstractions detected
- policy roles discovered or mapped
- generated artifacts refreshed or intentionally deferred
- any stale or conflicting metadata found

Keep generated architecture knowledge concise and labeled with `source` and `confidence`.
