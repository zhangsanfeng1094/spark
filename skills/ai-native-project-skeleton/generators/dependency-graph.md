# Dependency Graph Generator

Generate `.ai/generated/dependency-graph.index.json` from imports, package references, build files, or language-specific dependency tools.

The index is a compact routing artifact. It should contain high-signal boundaries and direct relationships only. If a full graph is useful, write it under `.ai/generated/raw/dependency-graph.full.json` and keep it tool-query-only.

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

## Edge Fields

- `from`
- `to`
- `type`
- `evidence_type`
- `evidence`
- `policy_status`
- `notes`

## Evidence Types

- `source-import`: verified from source imports or language dependency tooling.
- `source-reference`: verified from source references that are not imports.
- `documented`: described by project docs, not directly verified in code.
- `inferred`: AI inference from naming, structure, or repeated patterns.

Do not label an edge `source-import` if the evidence is only documentation.

## Policy Status

- `observed`: exists in current code or generated evidence.
- `unreviewed`: plausible but not confirmed.
- `allowed`: explicitly permitted by human-written rules or reviewed policy.
- `violation`: conflicts with human-written rules.

Default to `observed` for source-backed edges and `unreviewed` for inferred or documented edges. Never use `allowed` only because the edge exists.

## Rules

- keep graph generation deterministic where possible
- mark uncertain dynamic dependencies as low confidence
- do not encode policy decisions unless backed by `.ai/rules/dependency-rules.md`
- keep the index small enough for selective lookup
- keep full raw graphs out of default context assembly
