# Performance results

Each section names the benchmark that produced the numbers, so a later
change can re-run the same witness. Methodology and the full catalog of
what each benchmark defends: see [`benchmarks.md`](benchmarks.md).

## Current snapshot: 2026-10-18 (v0.90.0)

Environment: darwin/arm64, Apple M4 Pro, Go 1.27.2, SQLite in memory.
Postgres was not measured in this round. Medians of five runs:

```
BENCH_SKIP_PG=1 go test ./framework/ -run='^$' -bench=<pattern> \
    -benchmem -benchtime=1s -count=5
```

The fixtures run in the production posture (see
[benchmarks.md](benchmarks.md#benchmarks-measure-the-production-posture)),
and the framework and fixtures have changed since the v0.26.0 snapshot
below, so compare the two only by re-running a benchmark at both
versions.

### Framework against hand-rolled net/http

| Benchmark | net/http | GoFastr | Allocs (net/http / GoFastr) |
|---|---:|---:|---:|
| `T7_Plaintext` | 410 ns | 445 ns | 10 / 10 |
| `T7_JSON` | 494 ns | 542 ns | 10 / 10 |
| `T7_SingleQuery` | 5.8 µs | 7.6 µs | 54 / 81 |
| `T7_FilteredList` (50 rows) | 161 µs | 167 µs | 3,027 / 2,381 |

### Request path

| Benchmark | Result |
|---|---|
| `Middleware_DefaultChain` | with the default chain 4.4 µs / 47 allocs; without it 146 ns / 4 allocs; raw router 136 ns / 4 allocs |
| `T9_IslandRPC/pages=1` | 8.0 µs / 107 allocs |
| `T9_IslandRPC_Concurrency/workers=64` | p50 11 µs, p90 26 µs, p99 0.42 ms, p999 20 ms |
| `T9_UIHostPageRender` | `/` 142 µs / 1,133 allocs (15.3 KB); `/about` 156 µs / 1,245 allocs (16.1 KB) |
| `JSONCasing` | snake→camel and camel→snake 371 ns / 4 allocs per 10-key row; single word 5.9 ns / 0 allocs |
| `DSLParse` (cache hit) | 19–79 ns, 0–3 allocs |

### Data paths

| Benchmark | Result |
|---|---|
| `Tier1_IncludesVsN1`, limit=100 | eager include 1.9 ms / 28.5k allocs; naive N+1 14.3 ms / 47.7k allocs |
| `Tier1_PaginationDepth` | offset page 1 → 180: 142 → 218 µs; cursor page 1 → 180: 148 → 153 µs |
| `Tier1_BatchVsN`, 50 items | batch 1.5 ms; 50 single POSTs 2.3 ms |
| `T9_StreamingVsBuffered_RealVolume`, 5000 rows | buffered-paginated 13.4 ms; streaming-single 12.2 ms |

### Boot and background

| Benchmark | Result |
|---|---|
| `AutoMigrate_Idempotent`, N=50 | 469 µs / 7.5k allocs |
| `SchemaDiff`, N=50 | 414 µs / 7.4k allocs |
| `CronTick` | N=1 14 ns / 0 allocs; N=1000 185 µs / 1.3k allocs |
| `T8_ColdStart` | minimal app 0.26 ms; ten entities 0.62 ms (`-benchtime=1x`) |

---

## Historical snapshot: 2026-07-18 (v0.26.0)

> **Kept as a reference point.** These numbers were captured at
> **v0.26.0** on **2026-07-18**. Re-run the named benchmark before
> quoting any figure.

Environment: darwin/arm64, Apple M4 Pro, Go (project default), SQLite
in-memory, and Postgres 16 via testcontainers when noted.

Run command:

```
go test ./framework/ -run=^$ -bench=<pattern> \
    -benchmem -benchtime=100ms -count=1 -timeout=600s
```

---

### Default middleware chain
- Benchmark: `BenchmarkMiddleware_DefaultChain`
- with-default-chain 3.9 µs/op, 47 allocs; without-default-chain 0.19 µs/op,
  5 allocs; raw-router 0.17 µs/op, 4 allocs.
- The full default chain costs ~23× a raw route. (The historic 200×
  regression, 268 µs, stays gone.)

### Streaming vs buffered list at volume
- Benchmark: `BenchmarkT9_StreamingVsBuffered_RealVolume`
- Postgres, 5000 rows: buffered-paginated 46.7 ms/op; streaming-single
  11.5 ms/op: a **4.1× streaming win**. SQLite is too fast to show the
  gap clearly (15.7 vs 13.9 ms).

### FilteredList vs hand-rolled net/http: overhead reduction (2026-07-18)
- Benchmarks: `BenchmarkT7_FilteredList_GoFastr` vs
  `BenchmarkT7_FilteredList_NetHTTP`.
- **Fixture-fix caveat (commit-window):** generated CRUD required a
  session by default since commit `4758c4a0`, but the
  `setupBlogDomain` fixture had no `OwnerField`/`Access`/`Config.Public`,
  so anonymous bench requests 401'd before reaching the List body. The
  bench harness now injects a user via `handler.SetUser(req.Context(),
  …)`, mirroring production auth middleware, so the numbers below reflect
  the REAL List path. The pre-fix 176 µs / 2442 allocs figure measured
  the auth rejection, not the list.
- **Issue #100B optimizations (parse `r.URL.Query()` once per request,
  pool the per-row scan buffer, hoist the operator-suffix table, pre-cap
  QueryBuilder/CountBuilder `wheres`/`args`):**

  | Dialect  | Metric    | Before          | After           | Δ            |
  |----------|-----------|-----------------|-----------------|--------------|
  | SQLite   | sec/op    | 167.1 µs ± 3%   | 162.6 µs ± 3%   | **−2.69%** (p=0.003) |
  | SQLite   | B/op      | 96.29 Ki ± 0%   | 85.64 Ki ± 0%   | **−11.06%** (p=0.000) |
  | SQLite   | allocs/op | 2.447k ± 0%     | 2.353k ± 0%     | **−3.84%** (p=0.000) |
  | Postgres | sec/op    | 825.3 µs ± 7%   | 850.2 µs ± 7%   | ~ (p=0.247, n.s.) |
  | Postgres | B/op      | 77.20 Ki ± 0%   | 66.58 Ki ± 0%   | **−13.75%** (p=0.000) |
  | Postgres | allocs/op | 1.952k ± 0%     | 1.858k ± 0%     | **−4.82%** (p=0.000) |

  benchstat `-benchtime=1s -count=10`, geomean −5.38% sec/op, −12.42%
  B/op, −4.33% allocs/op. Postgres time is network-bound (the testcontainers
  round-trip dominates), so the Go-side allocation/bytes win doesn't show
  up in wall time on that tier; SQLite (in-memory, no network) shows the
  full stack improvement.
- The hand-rolled net/http baseline is unchanged (`BenchmarkT7_FilteredList_NetHTTP`);
  the gap to it shrank on bytes/allocs but persists on time because the
  declare-once surface still pays for case conversion, projection, and
  include-tree resolution that the hand-rolled handler skips. The next
  lever is item 5 of the brief (caching static per-entity work behind
  registration), gated on a sanitize-still-rejects regression test.

### JSON case conversion
- Benchmark: `BenchmarkJSONCasing`
- snake→camel / camel→snake: ~450 ns/op, 4 allocs per 10-key row;
  `casing.ToCamel`/`ToSnake` single-word 6.4 ns/op, 0 allocs (cached).

### Schema diff
- Benchmark: `BenchmarkSchemaDiff` (bulk `information_schema` reads via
  `ReadLiveColumnsBulk`)
- Postgres N=50: 3.4 ms / 5328 allocs; SQLite N=50: 443 µs / 6383 allocs.
  (Down from 59 ms on Postgres before the bulk-query rework.)

### AutoMigrate idempotent re-run
- Benchmark: `BenchmarkAutoMigrate_Idempotent`
- Postgres N=50: 3.7 ms / 5387 allocs; SQLite N=50: 472 µs / 6397 allocs.
- Context: boot auto-migrate converges columns (additive `ADD COLUMN`),
  not just table existence: one bulk `information_schema.columns` query
  plus an in-memory per-entity diff. That trade roughly doubled the
  re-run cost versus the existence-only path, and buys "add a field,
  reboot, it works" without a `migrate generate` + `migrate up` step.
  Still one round trip and comfortably inside a ~10 ms boot budget.

### Cron tick scan
- Benchmark: `BenchmarkCronTick`
- N=1: 7.6 ns / 0 allocs; N=1000: 292 µs / 1333 allocs.
- The per-tick scan itself is 0-alloc; the allocs at large N are one
  goroutine spawn per *matching* job (`go func(j CronJob)`), which is
  intentional dispatch cost, not scan overhead.

### DSL parser cache
- Benchmark: `BenchmarkDSLParse`
- All shapes (trivial/filter/complex/in-list): 15–16 ns/op, 0 allocs on
  cache hit.

### SSE backpressure semantics
- Benchmark: `BenchmarkSSE_BackpressureDropRate`
- Witness: `core/stream.SSEBroker`, `?buffer=128`, a deliberately slow
  subscriber, 5000 fast-published events → delivered 130, dropped 4870
  (drop rate 0.974).
- This is the **intended contract**, not a defect: bounded, non-blocking
  delivery with oldest-drop and latest-event retention for slow
  subscribers. `?slow=block` / `X-SSE-Slow: block` is the opt-in
  stronger-delivery path that backpressures publishers instead of
  dropping.

### UI host page render
- Benchmark: `BenchmarkT9_UIHostPageRender`
- `/` 3.7 ms / 59k allocs (response 14.8 KB); `/about` 3.9 ms / 59k
  allocs (response 15.6 KB).
- Interpret against the current response size: the witness pages are
  ~7× larger than the 2026-05 fixtures (2.2 KB then), so these numbers
  are a **new current-shape baseline**, not comparable to the old
  µs-scale figures. Compare future changes against today's bytes/op.

### Island RPC tail latency
- Benchmark: `BenchmarkT9_IslandRPC_Concurrency` (fixed worker counts)
- workers=64: p50 12 µs, p90 37 µs, p99 **5.2 ms**, p999 14 ms,
  95 allocs/op; p99 below the 10 ms target.

---

## Reading these numbers

- SQLite in-memory tiers measure framework overhead; Postgres tiers
  measure the realistic end-to-end path (network + real planner). A
  claim about Postgres behavior needs Postgres evidence; don't
  extrapolate from the SQLite tier.
- `-benchtime=100ms -count=1` is a smoke-grade capture for tracking
  order-of-magnitude and alloc counts, not publication-grade statistics.
  Re-run with a larger benchtime and `-count=10` + `benchstat` before
  claiming a regression or a win.
