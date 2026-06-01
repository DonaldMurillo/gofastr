# Coverage notes — provably-unreachable & accepted-hard lines

This catalogues the statements that keep the audited packages below a literal
100% **by design**. Two deliberate policy calls (2026-06-01):

1. **Keep defensive guards, don't delete them.** Several `if err != nil`
   branches are unreachable today but are correct fail-closed handling that we
   keep as defense-in-depth (e.g. a future change from `Scan(*any)` to typed
   destinations would make the guard live). We do **not** rewrite them to
   `_ = f()` to chase the number.
2. **Accept CLI serve-loops / interactive entry points + a few OS-IO faults.**
   These can't be unit-tested without booting real listeners / stdin or
   fault-injecting kernel-level IO, and refactoring critical entry points to
   force coverage carries more risk than the number is worth.

Every other reachable branch IS covered. The full-suite (own-package + the
`framework` root + `examples/website` e2e) coverage after this pass:

| Package | Coverage | Residual |
|---|---|---|
| `core/migrate` | 100% | — |
| `framework/migrate` | 100% | — |
| `framework/tenant` | 100% | — |
| `framework/entity` | 100% | — |
| `core/schema` | 99.1% | 3 dead (below) |
| `framework/crud` | 99.2% | 11 dead + 8 hard |
| `framework` (root) | 99.1% | 5 dead + 5 hard |
| `cmd/gofastr` | 84% | serve-loops + IO-fault `osExit` |

## A real bug this pass found & fixed

`scanRowsWithKeys` (crud.go) and `scanRowsPooledWithKeys` (pool.go) — the
primary List/Get/cursor scan paths — looped `for rows.Next()` and returned
`results, nil` **without checking `rows.Err()`**. A mid-iteration DB fault
(dropped connection, read error) was silently swallowed, returning partial or
empty results as success. The eager loaders already guarded this; the primary
scanners did not. Fixed by adding `rows.Err()` checks (regression tests:
`TestList_ScanErr`, `TestListAll_ScanErr`, `TestCursor_ScanErr`,
`TestTypedFind_ScanErr`).

## core/schema (3 dead)

`validate.go` `validateDecimal` / `validateJSON`: `decimalRe` is the live form
guard and rejects NaN/Inf/overflow literals (asserted by
`TestValidateDecimal`). The downstream checks are unreachable given the regex
but kept fail-closed:
- `validate.go:179-181` — `math.IsNaN/IsInf(n)` after `ParseFloat`: the regex
  blocks non-finite literals and overflow returns a `ParseFloat` error above.
- `validate.go:189-193` — `big.Rat.SetString` `!ok`: any `decimalRe`-valid
  string parses as a `big.Rat`.
- `validate.go:314-316` — `!json.Valid(b)` after `json.Marshal`: `Marshal`
  never emits invalid JSON.

## framework/crud (11 dead + 8 hard)

**Dead (scan into `*any` can't fail / value already normalised):**
- `crud.go:845`, `eager.go:126,186,237,318`, `eager_filtered.go:137,240,320` —
  `rows.Scan(ptrs...)` where `ptrs[i] = &vals[i]` (`*interface{}`). Scanning a
  driver value into `*any` cannot fail.
- `crud.go:336` — `filter.ParseFilters` always returns a nil error.
- `crud_cursor.go:58` — `pagination.ParseCursorPagination` always normalises
  `direction` to `"forward"`/`"backward"` before `serveCursorList` (its only
  caller) sees it.
- `crud_stream.go:96` — `scanRowsOne` scans into `*any`; cannot fail.

**Hard (need kernel-level IO fault the in-memory test driver can't produce):**
- `eager.go:116,226,308`, `eager_filtered.go:128,230,311` — `rows.Columns()`
  error: only fails on an already-closed `Rows`, unreachable mid-flow.
- `crud_upload.go:90` — `strings.Contains(err, "request body too large")`: a
  legacy-Go fallback; modern Go reports `*http.MaxBytesError` (the typed path
  IS tested via `TestUpdate_413` / `crud.go:651`).
- `crud_upload.go:174` — `multipart.FileHeader.Open` failure: the in-process
  multipart reader keeps small parts in memory and never fails Open.

Reachable failure paths ARE tested, incl. fault-injected `Query`/`rows.Err`/
`Commit`/`Exec` errors (see `cov_faultdriver_test.go`) and failing-`ResponseWriter`
SSE/stream client-disconnects (`cov_failwriter_test.go`).

## framework (root) (5 dead + 5 hard)

**Dead:**
- `app.go:725,739,764` — `decl.Config()` error in `EntityFromFile` /
  `EntitiesFromDir` / `GroupEntitiesFromDir`: `entity.LoadEntityDeclaration[s]`
  already calls `Config()` and fails the load on error, so a loaded decl never
  re-fails here.
- `testharness.go:163` — `json.Unmarshal` of bytes just produced by
  `json.Marshal` into `any`: cannot fail.
- `typed_hooks.go:201` — same `json.Unmarshal`-of-own-`Marshal` pattern in
  `mergeStructIntoMap`.

**Hard:**
- `app.go:1022` — `srv.Shutdown(ctx)` error: needs a hung connection to exceed
  the deadline.
- `app.go:1080` — `isolation.Resolve(".")` / `.Addr(addr)` errors: depend on
  filesystem/env isolation state not reproducible in a unit test.
- `mcp_introspection.go:174,211` — `docs.List()` / `docs.SearchWithLimit()`
  errors: the docs FS is embedded at build time and always valid; only a
  corrupt embed would error.

The reachable `InTx` commit-error path IS tested (`TestCovInTxCommitError`,
fault driver).

## cmd/gofastr (serve-loops + IO-fault osExit)

The CLI reached 84% (from 48%). Remaining uncovered code is, by category:
- **Blocking serve loops / interactive entry points** — `main` (`os.Args`
  entry; its routing logic IS tested via the extracted `dispatch`),
  `dev.go runDev` (file-watch+rebuild loop), `harness.go runHarness/runSingle/
  runREPL` (interactive REPL needing a live provider + stdin),
  `harness_http.go startHTTPListener`, `harness_mcp.go runHarnessMCP` (stdio
  server), `generate_watch.go runGenerateWatch/runOnce` (poll loop + self-exec).
  Their pure helpers (isolation resolve, build/serve, scan-mod-times, hashers,
  `chatPage`, `streamOneTurn`, `deriveListenerSecret`) ARE tested.
- **Rare IO-fault `osExit(1)` branches** — e.g. a migrator's `Up` failing
  mid-transaction against a healthy SQLite file. Reachable error branches
  (missing dirs, bad drivers, malformed input, write failures) ARE covered; the
  flaky chmod/IO-fault residue is not fabricated.

Test seam added: `var osExit = os.Exit` (production-identical) lets tests
observe CLI exit codes without killing the test binary; `main()` is a one-line
wrapper over the testable `dispatch(args)`.
