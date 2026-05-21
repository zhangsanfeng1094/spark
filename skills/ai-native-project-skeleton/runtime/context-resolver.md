# Context Resolver

Purpose: find the smallest useful architecture context for a task.

## Inputs

- current task
- current file path, when available
- repository root
- changed files, when available

## Resolution Steps

1. Start from the current file directory.
2. Walk upward until the nearest `.agent.md` is found.
3. Parse the anchor for boundary name, purpose, constraints, key concepts, and `See:` references.
4. Infer the nearest semantic boundary from the anchor first, then from path segments and dependency evidence.
5. Load `.ai/boundaries/<boundary>/rules.md` only when that boundary exists and is relevant.
6. Load global `.ai/rules/*` only when the task affects architecture, dependencies, project style, or agent behavior.
7. Load `.ai/generated/summaries/*` only for the touched module or directly related dependencies.
8. Query `.ai/generated/*.index.json` only for the current boundary and direct neighbors.

## Generated Artifact Rules

- Default-load `architecture-map.md` only for orientation and only as needed.
- Default-query `dependency-graph.index.json` and `module-graph.index.json` by boundary, node, or touched path.
- Do not load `.ai/generated/raw/*` into prompt context. Use raw files only through tools that return a small filtered result.
- If an index is missing or stale, inspect code directly and mark the generated artifact stale.

## Anchor Rules

`.agent.md` files are context entry points, not full documentation.

They should be:

- short
- stable
- human-readable
- specific to a meaningful architecture boundary

Do not create anchors in every directory. Add one only when the boundary has independent purpose, constraints, dependencies, ownership, or vocabulary.

## Staleness Handling

If an anchor or boundary rule contradicts code:

- mark the metadata as stale in the response
- follow the code for behavior
- suggest a metadata update
- do not silently rewrite architectural rules unless the task explicitly includes synchronization
