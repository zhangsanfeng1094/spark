# Example Extractor

Generate examples from existing code to help future agents follow local patterns.

## Selection Criteria

Choose examples that are:

- current
- tested
- representative
- small enough to understand quickly
- aligned with human-written rules

Avoid examples that are:

- deprecated
- unusually complex
- known workarounds
- untested unless explicitly labeled

## Output

Write concise notes under `.ai/boundaries/<boundary>/examples/` or `.ai/generated/summaries/`.

Each example should include:

- path
- pattern demonstrated
- when to reuse it
- confidence
