# 041 — CLI: `gofastr test`

**Phase:** 4 (CLI & DX) | **Depends on:** 035, 042

## Goal
Run tests with framework integration. Test DB setup, coverage, watch mode.

## Deliverables
- [ ] `gofastr test` — run all tests with framework harness
- [ ] Auto-detect `*_test.go` files
- [ ] Test database setup/teardown (temp database per test run)
- [ ] Coverage reporting: `--cover`
- [ ] Watch mode: `--watch` rerun on file changes
- [ ] Verbose output: `--verbose` with test names
- [ ] `--json` output for CI integration
- [ ] Benchmark support: `--bench`

## Acceptance Criteria
- Runs all Go tests in project
- Test DB created before tests, dropped after
- Coverage report shows percentage
- Watch mode reruns on .go file change
