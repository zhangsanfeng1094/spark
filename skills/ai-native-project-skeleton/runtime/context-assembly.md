# Context Assembly

Purpose: build a minimal task prompt from resolved repository intelligence.

## Assembly Order

1. User task and acceptance criteria.
2. Current files or changed files.
3. Nearest `.agent.md` anchor.
4. Matching policies.
5. Boundary rules.
6. Relevant generated summaries.
7. Local slices from generated indexes.
8. Global rules, only when needed.

## Size Discipline

Use the smallest context that can safely guide the task.

Prefer summaries, selected constraints, and local index slices over full documents. Do not inject the full repository tree, all policies, all boundary notes, full graphs, or raw generated artifacts.

## Required Metadata Labels

Generated artifacts should identify their origin:

```yaml
source: ai
confidence: medium
updated_at: YYYY-MM-DD
derived_from:
  - path/or/command
```

Generated graph edges should also identify evidence:

```yaml
evidence_type: source-import|source-reference|documented|inferred
policy_status: observed|unreviewed|allowed|violation
```

Use `allowed` only when backed by human-written dependency rules or explicit reviewed policy. Use `observed` or `unreviewed` for AI-derived edges.

Human-written rules should use:

```yaml
source: human
confidence: high
```

## Final Context Checklist

- Does this context explain the current boundary?
- Does it include the policies for the file role?
- Does it omit unrelated boundaries?
- Does it distinguish human rules from generated inference?
- Does it include only local graph/index slices?
- Does it flag stale or conflicting metadata?
