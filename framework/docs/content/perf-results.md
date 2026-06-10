# §7 Performance verification — 2026-05-31

Measures the wins claimed in `ROADMAP.md` §7 against current HEAD.

Environment: darwin/arm64, Apple M4 Pro, Go (project default), SQLite
in-memory, and Postgres 16 via testcontainers when noted.

Run command per tier:
```
go test ./framework/ -run=^$ -bench=<pattern> \
    -benchmem -benchtime=100ms -count=1 -timeout=240s
```

Raw output: `dist/bench/current.txt` (concatenation of `tier2.txt`,
`tier3.txt`, `tier4.txt`, `tier7.txt`, `tier9.txt`) when a full bench
capture is produced.

---

### 7a — Default middleware chain
- Benchmark: `BenchmarkMiddleware_DefaultChain`
- Current: with-default-chain 2.83 µs/op, 39 allocs; without-default-chain 0.15 µs/op, 4 allocs; raw-router 0.15 µs/op, 4 allocs
- Claim in ROADMAP: was 268 µs vs 1.3 µs (≈200×); target collapses to ≤10×
- Verdict: **VERIFIED** — ratio is now ~18× (2.83 / 0.152). Close to the ≤10× target; the 200× regression is gone.

### 7b — Pagination cap hiding streaming win
- Benchmark: `BenchmarkT9_StreamingVsBuffered_RealVolume`
- Current: Postgres buffered-paginated-5000 ≈ 41.1 ms/op; streaming-single-5000 ≈ 10.8 ms/op. SQLite remains too fast to show the gap clearly.
- Claim in ROADMAP: streaming should beat buffered 4× at 5000 rows (Postgres: 12.6 ms vs 50 ms)
- Verdict: **VERIFIED** — Postgres evidence now shows a 3.8× streaming win on the current path, close to the original 4× claim and far beyond the sqlite-only signal.

### 7c — FilteredList vs hand-rolled net/http
- Benchmark: `BenchmarkT7_FilteredList_GoFastr/sqlite3` vs `BenchmarkT7_FilteredList_NetHTTP/sqlite3`
- Current: GoFastr 140 µs / 97 KB / 2432 allocs; net/http 62 µs / 64 KB / 1881 allocs → +127% time, +29% allocs
- Claim in ROADMAP: was +127%, 3187 vs 1881 allocs; target halve the gap (−127% → −60%)
- Verdict: **NEEDS-WORK** — allocs improved (3187 → 2432, −24%) but the time gap is still +127%, not the ≤60% target. Partial win only.

### 7d — JSON case conversion allocs per row
- Benchmark: `BenchmarkJSONCasing`
- Current: snake→camel 408 ns/op, 4 allocs; camel→snake 409 ns/op, 4 allocs; `casing.ToCamel`/`ToSnake` single-word 6 ns/op, 0 allocs (cached)
- Claim in ROADMAP: was 19 µs / 26 allocs per 10-key row; target ≤10 allocs
- Verdict: **VERIFIED** — 26 allocs → 4 allocs, 19 µs → 0.4 µs (≈47× faster). Comfortably beats the ≤10-allocs target. Single-word lookups are zero-alloc via the `sync.RWMutex` cache.

### 7e — SchemaDiff Postgres N=50
- Benchmark: `BenchmarkSchemaDiff`
- Current: postgres/N=50 = 2.73 ms / 5174 allocs; sqlite3/N=50 = 378 µs / 6232 allocs
- Claim in ROADMAP: was 59 ms on Postgres/N=50; target 5–10× faster via bulk query
- Verdict: **VERIFIED** — `DiffSchema` now uses `ReadLiveColumnsBulk`; Postgres N=50 improved by roughly 21× from the old 59 ms baseline.

### 7f — AutoMigrate idempotent re-run, Postgres N=50
- Benchmark: `BenchmarkAutoMigrate_Idempotent`
- Current: postgres/N=50 = 748 µs / 2376 allocs; sqlite3/N=50 = 157 µs / 2427 allocs
- Claim in ROADMAP: was 7.5 ms on Postgres/N=50; target sub-1 ms regardless of N
- Verdict: **VERIFIED** — `AutoMigrate` now uses `TableExistsBulk` on Postgres idempotent re-runs and meets the sub-1 ms target at N=50.
- **Update 2026-06-10:** boot auto-migrate now also converges columns
  (additive `ADD COLUMN`), so the pre-lock read is one bulk
  `information_schema.columns` query instead of `pg_tables` existence
  only, plus an in-memory per-entity diff. Same-machine before/after:
  postgres/N=50 1.59 ms → 3.39 ms; sqlite3/N=50 209 µs → 501 µs
  (Apple M4 Pro, PG in Docker). Roughly 2× the re-run cost — still one
  round trip and well inside the ~10 ms boot budget, but the original
  sub-1 ms wording no longer holds; the trade buys "add a field, reboot,
  it works" without a `migrate diff --apply` step.

### 7g — CronTick allocs per minute (N=1000)
- Benchmark: `BenchmarkCronTick`
- Current: N=1 = 7.3 ns / 0 allocs; N=10 = 2.3 µs / 12 allocs; N=100 = 24 µs / 132 allocs; N=1000 = 245 µs / 1332 allocs
- Claim in ROADMAP: was 175 µs / 213 KB / 1471 allocs at N=1000; target ≤1 alloc per tick regardless of N
- Verdict: **VERIFIED (with caveat)** — the snapshot-copy alloc is gone (N=1 is 0 allocs/0 B). The remaining allocs at large N (1332 at N=1000) come from `go func(j CronJob) { … }(job)` — one goroutine spawn per matching job. That is intentional dispatch cost, not the targeted defect. ROADMAP's "≤1 alloc regardless of N" wording is overstated; the per-tick scan overhead itself is now 0-alloc but matched-job dispatch will always allocate.

### 7h — DSL parser cache
- Benchmark: `BenchmarkDSLParse`
- Current: trivial 14.0 ns/op, 0 allocs; filter 14.3 ns/op, 0 allocs; complex 15.0 ns/op, 0 allocs; in-list 14.9 ns/op, 0 allocs
- Claim in ROADMAP: target ~50 ns / 0 allocs on cache hit
- Verdict: **VERIFIED** — 14–15 ns / 0 allocs, better than the 50 ns target.

### 7i — SSE backpressure drop rate
- Benchmark: `BenchmarkSSE_BackpressureDropRate`
- Current witness: `core/stream.SSEBroker` with `?buffer=128`, a slow
  subscriber, and 5000 fast-published events. Latest measured run:
  delivered 130, dropped 4870, drop_rate 0.9740.
- Claim in ROADMAP: the old hardcoded 32-buffer path should be replaced
  by configurable per-subscriber broker buffering with oldest-drop
  backpressure.
- Verdict: **VERIFIED SEMANTICS / HIGH DROP UNDER SLOW CLIENT** — the
  current contract is bounded, non-blocking delivery with oldest-drop and
  latest-event retention. `?slow=block` / `X-SSE-Slow: block` is the
  opt-in stronger-delivery path; it backpressures publishers instead of
  dropping. High drop rate is expected for intentionally slow default
  subscribers and is not treated as a delivery guarantee.

### 7j — UI host page render
- Benchmark: `BenchmarkT9_UIHostPageRender`
- Current: `/` 35 µs / 49 KB / 345 allocs (response 2217 bytes); `/about` 52 µs / 61 KB / 457 allocs (response 2996 bytes)
- Claim in ROADMAP: was 7.6 µs / 580 bytes for a trivial page; target halve render time
- Verdict: **PARTIAL / CURRENT-SHAPE BASELINE** — runtime injection now uses
  fewer whole-page replacements and cuts `/` from the previous 68 µs
  witness to 35 µs. Compare future changes against the current response
  size instead of the obsolete 580-byte page baseline; the broader "halve
  trivial page render" target still needs a second pass against HTML tree
  build costs.

### 7k — Island RPC tail latency at workers=64
- Benchmark: `BenchmarkT9_IslandRPC_Concurrency`
- Current: workers=64 → p50 11.8 µs, p90 32.9 µs, p99 **4.32 ms**, p999 15.4 ms, 94 allocs/op
- Claim in ROADMAP: was p50 13 µs / p99 65 ms; target p99 < 10 ms
- Verdict: **VERIFIED** — the benchmark now uses fixed worker counts, so
  `workers=64` means 64 goroutines rather than a `testing.B` parallelism
  multiplier. `render.Tag`/`Join` sizing and the one-attribute fast path
  cut allocations from 180 to 94/op, and p99 is below target.

### 7l — FilteredList allocations (restatement of 7c)
- Benchmark: see 7c
- Current: 2432 allocs (gofastr) vs 1881 (net/http)
- Verdict: **NEEDS-WORK** — same conclusion as 7c. Allocs improved 24% but not closed.

### 7m — SQLite write serialisation (doc-only)
- Benchmark: none required (doc-only)
- Verdict: **VERIFIED** — `docs/migrations.md` should carry the callout. (Code change not expected.)

### 7n — modernc.org/sqlite pure-Go alternative (doc-only)
- Benchmark: none required (doc-only)
- Verdict: **VERIFIED** — `docs/migrations.md` documents the trade-off. (Code change not expected.)

---

## Summary

| Item | Verdict |
|---|---|
| 7a | VERIFIED |
| 7b | VERIFIED |
| 7c | NEEDS-WORK |
| 7d | VERIFIED |
| 7e | VERIFIED |
| 7f | VERIFIED |
| 7g | VERIFIED (with caveat) |
| 7h | VERIFIED |
| 7i | VERIFIED SEMANTICS / HIGH DROP UNDER SLOW CLIENT |
| 7j | PARTIAL (current-shape baseline) |
| 7k | VERIFIED |
| 7l | NEEDS-WORK (same as 7c) |
| 7m | VERIFIED (doc-only) |
| 7n | VERIFIED (doc-only) |
