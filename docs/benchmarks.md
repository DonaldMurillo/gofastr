# Benchmarks

GoFastr ships a tiered set of Go benchmarks. Each tier maps to a category
of claim or hot path; together they exercise the surfaces the README
advertises against both SQLite and Postgres.

## Tiers

| Tier | What it covers                                                  | DB? |
|------|------------------------------------------------------------------|-----|
| 1    | Claims-defending end-to-end (includes vs N+1, cursor depth, batch vs N, streaming) | Both |
| 2    | Hot-path microbenchmarks (router lookup, middleware chain, JSON casing, DSL parse) | No  |
| 3    | Concurrency & background (event bus fan-out, SSE drop rate, cron tick) | No  |
| 4    | Startup & infra (AutoMigrate idempotent, schema diff, in-memory search) | Both (search is in-memory) |

## Running

```bash
make bench           # everything, BENCHTIME=1s BENCH_COUNT=3
make bench-tier1     # just Tier 1 (the claims)
make bench-tier2     # just Tier 2
make bench-tier3     # just Tier 3
make bench-tier4     # just Tier 4

make bench-sqlite    # everything, skipping Postgres sub-benchmarks
make bench-pg        # only the /postgres/ sub-benchmarks
```

Tunable via env:

- `BENCHTIME` — `-benchtime` value (default `1s`).
- `BENCH_COUNT` — `-count` value (default `3`). Pair with `benchstat`.
- `BENCH_TIMEOUT` — `-timeout` value (default `30m`).
- `BENCH_PKGS` — which packages to scan (default `./framework/... ./core/router/... ./battery/search/...`).

Output captures land under `dist/bench/<scope>.txt` so they survive across
runs and can be compared with `benchstat dist/bench/old.txt
dist/bench/new.txt`.

## Postgres availability

Postgres benchmarks resolve a connection via the same path as the test
suite:

1. `TEST_POSTGRES_DSN` env var, or
2. testcontainers-go (requires Docker), or
3. **skip** — Postgres sub-benchmarks are silently skipped, not failed.

`make bench-pg` fails fast if neither path is available; `make bench` and
the tier targets will just skip the Postgres half.

## Reading the output

Standard Go benchmark line:

```
BenchmarkTier1_IncludesVsN1/sqlite3/eager-include/limit=100-14  607  1834105 ns/op  ...
                            ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
                            sub-benchmark path: dialect / variant / size
```

For benchmarks that report custom metrics (`b.ReportMetric`), look for
named columns like `drop_rate`, `delivered`, `dropped`, `response_bytes`.

## Tier 1 — what to look for

These exist to either back up the README or expose a hole.

### 1.1 Includes vs N+1
Eager (`?include=author,comments`) must outscale naive per-row fetches.
The win factor is `≈ N * round-trip cost`; on Postgres the gap should be
1–2 orders of magnitude.

### 1.2 Cursor pagination at depth
`offset/page=180` should degrade vs `offset/page=1` as table size grows.
`cursor/page=180` should be **flat**: same cost as page 1. If cursor
degrades, the keyset query lost its index or the cursor field is wrong.

### 1.3 Batch vs N
50-item batch should beat 50 individual POSTs by both per-tx overhead and
per-request middleware overhead. The gap is widest on Postgres where the
network round-trips dominate.

### 1.4 Streaming vs buffered
**Known caveat:** `parsePagination` clamps `?limit=` to ≤100, so the
streaming surface's intended workload (`limit ≥ streamListThreshold =
1000`) is not reachable from the HTTP surface. The benchmark therefore
exercises streaming at limit=100, which measures the per-row encode/write
overhead but does not show the bounded-memory advantage. Worth fixing.

## Tier 2 — what to look for

- **Router lookup** — should be near-flat across N=1..1000 routes
  (tree match, not linear scan). Compare against `http.ServeMux`.
- **Middleware chain** — measure the cost of `Recovery → RequestID →
  Logging → SecurityHeaders → Timeout`. The Logging middleware writes
  stdout; benchmarks that don't redirect it will see I/O overhead.
- **JSON casing** — every request runs `mapToCamelCase` once
  (write paths run `mapToSnakeCase` too). ~26 allocations per row is
  the current baseline; opportunities for buffer reuse exist.
- **DSL parse** — agents may invoke this per prompt. Should stay well
  under 100µs for a complex query.

## Tier 3 — what to look for

- **Event bus fan-out** — synchronous is dominated by handler slice
  copy + handler calls; per-emit allocations should scale linearly with
  N (the snapshot copy).
- **SSE backpressure** — a 32-event buffer paired with a slow consumer
  should drop the surplus; the `drop_rate` metric records this.
- **Cron tick** — scanning N jobs at minute boundaries. ~175µs for N=1000
  baseline; budget for in-process schedulers running many jobs.

## Tier 4 — what to look for

- **AutoMigrate re-run** — the "safe to run on every boot" claim. Should
  stay under ~10ms even at 50 entities. Postgres is much slower than
  SQLite because every existence check is a round-trip.
- **Schema diff** — same shape, but pays for the full information_schema
  lookup. Acceptable as a one-shot CLI command, not as a hot path.
- **In-memory search** — confirms O(corpus) scan. 10k docs ≈ 3ms per
  query is fine for tests/demos; production needs a real backend.

## Adding a benchmark

Co-locate with the code being measured: `bench_*_test.go` in the same
package. Use `forEachBenchDialect` if it needs a DB. Skip not fail when
external infrastructure is missing.

Every benchmark should:

1. Set up state outside the timed loop (`b.ResetTimer()` after).
2. Report allocations with `b.ReportAllocs()` when allocation cost is
   relevant.
3. Report custom dimensions with `b.ReportMetric(...)` when raw ns/op
   isn't the whole story (drop rates, body sizes, etc.).
4. Have stable, deterministic input so runs are comparable.

## Comparing runs

```bash
make bench BENCH_COUNT=10 BENCHTIME=1s
mv dist/bench/all.txt dist/bench/before.txt

# … make a change …

make bench BENCH_COUNT=10 BENCHTIME=1s
benchstat dist/bench/before.txt dist/bench/all.txt
```

`benchstat` (`go install golang.org/x/perf/cmd/benchstat@latest`) reports
geometric mean + p-value so noise doesn't show up as a regression.

## Common mistakes

- **Not skipping Postgres when it's unavailable.** Use
  `forEachBenchDialect` — it calls `b.Skip` correctly. Don't fail.
- **Allocating inside the timed loop.** Build payloads before
  `b.ResetTimer()`.
- **Benchmarking through stdout-writing middleware.** Some default
  middleware writes a request log per call; either redirect output or
  use `WithoutDefaultMiddleware()` for hot-path measurements.
- **Trusting a single run.** Use `BENCH_COUNT=10` + `benchstat` for any
  comparison that matters.
