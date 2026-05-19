# Module Graph Generator

Generate `.ai/generated/module-graph.index.json` as a compact map of project-specific boundaries and relationships.

The module graph is for routing context, not for documenting every package, file, or symbol.

## Required Fields

```json
{
  "source": "ai",
  "confidence": "medium",
  "updated_at": "YYYY-MM-DD",
  "artifact_role": "index",
  "nodes": [],
  "edges": []
}
```

## Node Fields

- `id`
- `paths`
- `summary`
- `confidence`

## Edge Fields

- `from`
- `to`
- `type`
- `evidence_type`
- `evidence`
- `policy_status`

## Rules

- Use the same `nodes` and `edges` shape as dependency indexes.
- Keep only stable boundaries and important direct relationships.
- Use project language for node IDs.
- Do not include every file or every package unless the repository is tiny.
- Put any exhaustive machine graph under `.ai/generated/raw/` and query it through tools only.
