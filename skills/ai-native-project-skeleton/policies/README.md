# Policies

This skill does not ship universal role policies as defaults.

Policies should be generated from the target repository's own architecture vocabulary. Use `policy-archetype.template.md` to create project-local files under `.ai/policies/` only after discovering stable repeated roles in the codebase.

Do not start from a fixed taxonomy. First inspect the repository, then name roles using the language already present in code and tests.
