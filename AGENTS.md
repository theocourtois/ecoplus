# AGENTS.md

Rules for contributors and AI agents:

- No comments in Go code. `make nocomment` fails on any comment except `//go:` directives and files starting with `// Code generated`. `//nolint` is forbidden.
- No license headers. The license lives in LICENSE only.
- English everywhere.
- No package-level mutable state and no `init()`. Inject `cloud.Cloud`, `clock.Clock` and `Config` through constructors.
- `context.Context` is the first parameter of every function that does I/O.
- Wrap errors with `%w`. Domain errors are typed values in `internal/api` or `internal/fleet`.
- No `panic` outside `main`.
- Functions stay short. Files stay under 300 lines.
- `internal/fleet` imports only the standard library.
- Logs use `log/slog`, one event per state transition, with `server_id`, `from`, `to`, `gen` and `lease`.
- Tests are offline, deterministic and run with `-race -shuffle=on`. The full suite stays under 60 seconds.
- Run `make check` before sending a change.
