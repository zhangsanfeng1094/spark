# Semantic Summary Prompt

Use when generating or refreshing `.ai/generated/summaries/*`.

## Instructions

Summarize code behavior, not intentions.

Include:

- module purpose inferred from code
- public entry points
- important dependencies
- recurring local patterns
- notable side effects
- test coverage signals
- confidence and source metadata
- evidence type for important claims

Exclude:

- invented business rules
- broad architecture claims not supported by code
- full file listings
- implementation trivia
- full graph data

## Output Header

```yaml
source: ai
confidence: low|medium|high
updated_at: YYYY-MM-DD
derived_from:
  - <path>
```

## Claim Discipline

Use direct language for source-backed facts. Use "appears to" or "likely" for inference. If a claim comes from docs rather than code, say so.
