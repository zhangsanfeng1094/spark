# Architecture Review Prompt

Use when reviewing a repository or change for architectural consistency.

## Review Focus

- boundary violations
- hidden coupling
- duplicated abstractions
- dependency direction problems
- metadata that conflicts with code
- stale generated summaries
- missing tests around boundary behavior

## Method

1. Resolve nearest anchors for changed files.
2. Load matching policies.
3. Compare imports, call paths, ownership, and side effects with documented constraints.
4. Treat code as authoritative when metadata disagrees.
5. Report issues with file paths and concrete risk.

## Output

List findings first, ordered by severity. Include stale metadata separately from code defects.
