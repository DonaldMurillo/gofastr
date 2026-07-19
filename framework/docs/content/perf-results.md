# Performance results — 2026-07-18 (v0.26.0)

Current measured results for the framework's hot paths. Each section
names the benchmark that produced the numbers, so a future change can
re-run exactly the same witness. Methodology + full catalog of what each
benchmark defends: see [`benchmarks.md`](benchmarks.md).

Environment: darwin/arm64, Apple M4 Pro, Go (project default), SQLite
in-memory, and Postgres 16 via testcontainers when noted.

Run command:

```
go test ./framework/ -run=^$ -bench=<pattern> \
    -benchmem -benchtime=100ms -count=1 -timeout=600s
```

---

## Results

### Default middleware chain
- Benchmark: `BenchmarkMiddleware_DefaultChain`
- with-default-chain 3.9 µs/op, 47 allocs; without-default-chain 0.19 µs/op,
  5 allocs; raw-router 0.17 µs/op, 4 allocs.
- The full default chain costs ~23× a raw route. (The historic 200×
  regression — 268 µs — stays gone.)

### Streaming vs buffered list at volume
- Benchmark: `BenchmarkT9_StreamingVsBuffered_RealVolume`
- Postgres, 5000 rows: buffered-paginated 46.7 ms/op; streaming-single
  11.5 ms/op — a **4.1× streaming win**. SQLite is too fast to show the
  gap clearly (15.7 vs 13.9 ms).

### FilteredList vs hand-rolled net/http — overhead reduction (2026-07-18)
- Benchmarks: `BenchmarkT7_FilteredList_GoFastr` vs
  `BenchmarkT7_FilteredList_NetHTTP`.
- **Fixture-fix caveat (commit-window):** generated CRUD required a
  session by default since commit `4758c4a0`, but the
  `setupBlogDomain` fixture had no `OwnerField`/`Access`/`Config.Public`
  — anonymous bench requests 401'd before reaching the List body. The
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
  registration) — gated on a sanitize-still-rejects regression test.

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
  not just table existence — one bulk `information_schema.columns` query
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
  95 allocs/op — p99 below the 10 ms target.

---

## Reading these numbers

- SQLite in-memory tiers measure framework overhead; Postgres tiers
  measure the realistic end-to-end path (network + real planner). A
  claim about Postgres behavior needs Postgres evidence — don't
  extrapolate from the SQLite tier.
- `-benchtime=100ms -count=1` is a smoke-grade capture for tracking
  order-of-magnitude and alloc counts, not publication-grade statistics.
  Re-run with a larger benchtime and `-count=10` + `benchstat` before
  claiming a regression or a win.
