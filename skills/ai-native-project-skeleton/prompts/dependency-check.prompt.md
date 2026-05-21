# Dependency Check Prompt

Use when validating dependency direction and architectural drift.

## Instructions

1. Extract imports or package dependencies for the changed files.
2. Compare them with nearest anchor constraints and `.ai/rules/dependency-rules.md`.
3. Identify new cross-boundary edges.
4. Classify each edge as observed, unreviewed, allowed, or violation.
5. Recommend the smallest correction that matches existing project patterns.

Use `allowed` only when backed by human-written dependency rules or reviewed policy. Existing code by itself means `observed`, not allowed.

## Output

- dependency edge
- source file
- target module
- classification
- evidence type
- reason
- suggested fix
