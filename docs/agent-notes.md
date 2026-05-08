# Agent Notes

## 2026-05-07 - architecture-review

- Scope: `testing`, `core-ui`, `framework`
- Symptom: `go test ./...` needs permission to bind local `httptest` ports, and the current real failure is `github.com/gofastr/gofastr/core-ui/app` overlay wrapper expectations.
- Evidence: `go test ./core-ui/app` fails `TestNewDrawer`, `TestNewSheet`, and `TestNewDialog`; `go test ./core/query ./framework ./core/middleware ./cmd/gofastr` passes.
- Next time: run focused package tests first, then escalate the full suite only when browser/httptest packages are required.

## 2026-05-07 - api-ui-review

- Scope: `api`, `core-ui`, `architecture`
- Symptom: Architecture reviews should separate declarative feature flags from enforcement paths; current risks cluster around CRUD scoping, DB dialect assumptions, and runtime/server UI contracts.
- Evidence: `docs/project-architecture-review.md` tracks round 2 API findings and round 3 core-ui findings with file references.
- Next time: check generated docs/specs against actual parser behavior, then check shared UI instances for request-scoped mutable state.

## 2026-05-08 - continuation-review

- Scope: `core/middleware`, `framework`, `core/router`, `core-ui/devserver`, `core-ui/island`
- Symptom: Normal `go test ./...` can pass while concurrency bugs and unconnected public APIs remain. Race-enabled checks found the timeout middleware writes to the same response recorder from two goroutines.
- Evidence: `docs/project-architecture-review.md` round 4 records findings 17-23. `go test -race ./core/middleware ./core-ui/island ./core-ui/devserver` fails on `core/middleware/timeout.go`.
- Next time: include `go test -race` for middleware and SSE/streaming packages during reviews, and verify public registries/hooks are invoked by the runtime path, not only unit-tested as standalone helpers.

## 2026-05-08 - fresh-architecture-review

- Scope: `framework`, `core`, `core-ui`, `battery`, `cmd`
- Symptom: Round-based review notes became hard to use after multiple fix passes. A clean consolidated review makes current risks easier to triage and avoids carrying fixed findings forward.
- Evidence: `docs/project-architecture-review.md` now starts from scratch with architecture summary, prioritized findings, test gaps, and verification. File-field context handling was rechecked and left out because it now uses caller-supplied context.
- Next time: when asked to "start from scratch," rewrite the review artifact instead of appending rounds, and revalidate each old finding against the current tree before preserving it.
