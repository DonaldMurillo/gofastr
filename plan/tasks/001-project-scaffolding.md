# 001 — Project Scaffolding

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
Initialize the GoFastr project with proper Go module structure, folder layout, CI, and tooling.

## Deliverables
- [ ] `go.mod` initialized as `github.com/gofastr/gofastr`
- [ ] Folder structure:
  ```
  core/
    router/
    handler/
    middleware/
    query/
    schema/
    migrate/
    render/
    static/
    upload/
    stream/
    mcp/
    openapi/
  framework/
  battery/
    auth/
    storage/
    cache/
    email/
    queue/
  cmd/
    gofastr/
  plan/
  references/
  ```
- [ ] Each core package has a `doc.go` with package description
- [ ] `.gitignore` (binaries, .gofastr/, IDE files)
- [ ] `README.md` with project overview and status
- [ ] `Makefile` with targets: `build`, `test`, `lint`, `generate`, `dev`
- [ ] CI config (GitHub Actions): go test, go vet, golangci-lint
- [ ] `golangci.yml` lint config

## Acceptance Criteria
- `go build ./...` succeeds (even if packages are empty)
- `go test ./...` succeeds
- `make lint` passes
- CI pipeline runs green on push
