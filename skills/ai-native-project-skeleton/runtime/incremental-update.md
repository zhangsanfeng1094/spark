# Incremental Update

Purpose: keep repository intelligence synchronized with code without rebuilding or rewriting everything.

## Change Detection

Inspect:

- git diff and changed files
- new or removed modules
- dependency changes
- schema or migration changes
- PR descriptions and review comments, when available

## Update Targets

Refresh only affected artifacts:

- `.ai/generated/module-graph.index.json`
- `.ai/generated/dependency-graph.index.json`
- `.ai/generated/summaries/<module>.md`
- `.ai/generated/raw/*`, only when the project uses raw query artifacts
- `.ai/history/architecture-evolution.md`
- `.ai/history/recurring-patterns.md`
- `.ai/history/anti-patterns.md`

## Drift Detection

Mark context as stale when:

- documented dependencies no longer match imports
- anchors describe deleted or renamed modules
- boundary rules conflict with current behavior
- generated summaries mention removed symbols
- deprecated patterns still appear as recommended examples
- graph edges use `allowed` without support from human-written rules
- edge evidence says `source-import` but points only to prose documentation

## Human Review Boundary

Require human confirmation before turning major AI inference into authoritative rules, especially:

- new architecture boundaries
- cross-boundary dependency permissions
- security or compliance constraints
- business-critical boundary rules

Generated updates may summarize and flag; they should not silently redefine architecture.
