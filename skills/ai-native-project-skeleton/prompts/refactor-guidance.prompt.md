# Refactor Guidance Prompt

Use when planning or performing a refactor that touches architecture boundaries.

## Instructions

1. Identify current boundary ownership.
2. Locate existing patterns before introducing abstractions.
3. Preserve public contracts unless explicitly changing them.
4. Move behavior toward the owning boundary instead of spreading shared logic blindly.
5. Update generated summaries after code changes.
6. Do not promote AI-inferred patterns into human rules without review.

## Output

- intended boundary changes
- files affected
- policy implications
- tests required
- context artifacts to refresh
