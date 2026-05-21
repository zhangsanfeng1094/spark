# Boundary Signals

Use these checks while evaluating architecture-level refactors.

## Responsibility Map

Create a short table or notes covering:

- package/module name
- exported types/functions
- top-level callers
- internal imports
- external side effects
- tests and missing test paths

Useful commands:

```bash
rg --files <target>
rg -n "target/package|ExportedName|Start|Run|ServeHTTP|Set|Open|Write" .
go list -deps -f '{{.ImportPath}} => {{join .Imports ","}}' ./...
```

For large output, summarize with a script instead of reading raw logs.

## Split Signals

Split or extract when:

- lifecycle code is duplicated across variants
- transport/server setup is mixed with protocol conversion
- retry policy or fallback behavior is embedded inside a route handler
- a UI or CLI package imports a heavy integration package for one helper
- package names describe only one of several responsibilities
- adding one more client/provider would require copy-pasting a file set

Do not split solely because a file is long. Split when a boundary improves dependency direction, testability, or future extension.

## Target Boundary Patterns

Common target shapes:

- `server` or `proxy`: listener, mux, shutdown, logging, auth, client construction
- `client/<protocol>`: inbound request parsing and outbound response/SSE writing
- `target/<provider>`: upstream request/response mapping
- `gateway` or `pipeline`: route selection and orchestration
- `policy`: cross-protocol compatibility decisions
- `integration` or `runner`: product-specific launch/configuration only

Keep wrappers temporarily when external callers already depend on old names.

## Migration Slices

Prefer this order:

1. Extract shared infrastructure with no behavior change.
2. Point existing variants at the shared infrastructure.
3. Add one unified entry point if useful.
4. Move protocol-specific code behind adapters.
5. Remove compatibility wrappers only after callers migrate.

Each slice should have a clear rollback: revert that slice without changing user-facing behavior.
