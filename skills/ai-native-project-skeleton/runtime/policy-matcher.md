# Policy Matcher

Purpose: discover the project's own file roles and select semantic policies without loading unrelated rules.

## Role Discovery

Infer roles from repository evidence:

- file names and directory names
- imports and dependency direction
- framework conventions already present
- test placement and naming
- build configuration
- repeated local patterns
- existing documentation, if it agrees with code

Do not use a built-in role taxonomy. Name roles only after seeing repeated evidence in the target project.

## Matching Rules

1. Prefer explicit project-local policy files in `.ai/policies/`.
2. If no policy exists, derive a candidate role and record it in `.ai/policies/policy-map.md`.
3. Create a new `<project-role>.policy.md` only when the role is stable and repeated.
4. Match multiple policies when a file has multiple roles.
5. Do not load every policy by default.
6. If no policy matches, proceed with local code patterns and mark the role as unresolved.

## Conflict Rules

- Local `.ai/policies/*` beats bundled defaults.
- Nearest `.agent.md` constraints beat global rules.
- Code reality beats all metadata.
- AI-generated summaries never beat human-written rules.
