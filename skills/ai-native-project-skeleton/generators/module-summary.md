# Module Summary Generator

Generate `.ai/generated/summaries/<module>.md` from code inspection.

Summaries are default context candidates, so keep them short. Prefer roughly 0.5-1.5 KB per boundary unless the project justifies more.

## Extract

- module purpose from exported APIs and callers
- main files and public entry points
- internal collaborators
- external dependencies
- local neighbor edges from `.ai/generated/*.index.json`
- tests that exercise the module
- unclear or inferred behavior

## Rules

- summarize only what code supports
- label confidence
- prefer short sections
- link to anchors and boundary rules when present
- mark stale if referenced files disappear
- separate code-backed facts from AI inference
