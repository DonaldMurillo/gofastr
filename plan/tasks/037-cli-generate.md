# 037 — CLI: `gofastr generate`

**Phase:** 4 (CLI & DX) | **Depends on:** 035, 019

## Goal
Run code generation: entity configs → Go structs, DSL → query structs, template pre-compilation.

## Deliverables
- [ ] `gofastr generate` — run all code generation
- [ ] Entity config → Go model types, input/output types
- [ ] DSL queries → Go query struct code
- [ ] Template pre-compilation
- [ ] Output to `.gofastr/` directory (gitignored, regenerated)
- [ ] Dry-run: `--dry-run` shows what would be generated
- [ ] Watch mode: `--watch` regenerates on file changes (used by `gofastr dev`)
- [ ] `--json` output: list generated files

## Acceptance Criteria
- Generated Go code compiles
- Entity fields appear as struct fields with correct types
- DSL query generates valid query struct code
- .gofastr/ is idempotent (same input → same output)
