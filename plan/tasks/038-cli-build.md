# 038 — CLI: `gofastr build`

**Phase:** 4 (CLI & DX) | **Depends on:** 035, 037

## Goal
Codegen + go build in one step. Production-ready binary output.

## Deliverables
- [ ] `gofastr build` — runs generate then `go build`
- [ ] Output: single binary in `./bin/` or custom `-o` path
- [ ] Build flags: `-o`, `-ldflags` for version injection
- [ ] Cross-compilation: `GOOS`/`GOARCH` flags
- [ ] Build optimization: strip debug, trim path
- [ ] Error reporting: codegen errors AND go build errors, both actionable

## Acceptance Criteria
- `gofastr build` produces working binary
- Codegen errors reported before build attempt
- Build errors show file:line references
- Binary runs and serves the app
