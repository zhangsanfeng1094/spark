# Module Bootstrap Prompt

Use when creating a new architecture boundary or adding repository intelligence to an existing area of code.

## Instructions

1. Inspect existing modules and naming conventions.
2. Infer whether the area is a stable architecture boundary, feature slice, layer, package, application, tool, or other project-specific unit.
3. Create an anchor only when the boundary has stable purpose, constraints, ownership, or vocabulary.
4. Create `.ai/boundaries/<inferred-name>/rules.md` only for meaningful rules supported by code or human input.
5. Generate policy candidates from repeated project roles instead of copying generic policies.
6. Record assumptions and open questions instead of inventing business rules.

## Output

- boundary name
- anchor path
- boundary rules path
- inferred role and policy mapping
- assumptions needing human review
