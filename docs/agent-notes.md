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
