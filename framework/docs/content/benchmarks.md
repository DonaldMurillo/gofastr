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
| 5    | TechEmpower-style endpoints (Plaintext, JSON, SingleQuery, MultiQuery, Fortunes-like, Updates) | Both |
| 6    | Latency percentiles + concurrency (p50/p90/p99/p999 at parallelism 1/8/64) | Both |
| 7    | Stdlib baselines (`net/http` + `database/sql` paired with framework equivalents) | Both |
| 8    | Operational (cold start, sustained heap, goroutine leak check) | SQLite |
| 9    | UI runtime: streaming list at real volume, SSE EventStream end-to-end, island RPC swap, UI host page render | Both |

## Running

```bash
make bench               # everything, BENCHTIME=1s BENCH_COUNT=3
make bench-tier1         # claims-defending end-to-end
make bench-tier2         # hot-path microbenchmarks
make bench-tier3         # concurrency & background
make bench-tier4         # startup & infra
make bench-tier5         # TechEmpower-style endpoints
make bench-tier6         # latency percentiles + concurrency
make bench-tier7         # stdlib baselines
make bench-tier8         # operational (cold start, heap, goroutines)
make bench-tier9         # UI runtime (streaming, SSE, islands, page render)

make bench-techempower   # alias for tier 5
make bench-overhead      # alias for tier 7 (framework vs hand-rolled)

make bench-sqlite        # everything, BENCH_SKIP_PG=1
make bench-pg            # only the /postgres/ sub-benchmarks
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

Use `make bench-pg-evidence` for CI or release notes. It runs the
Postgres-sensitive witnesses (`StreamingVsBuffered_RealVolume`,
`SchemaDiff`, and idempotent `AutoMigrate`), writes
`dist/bench/postgres-evidence.txt`, and fails if no `/postgres/`
sub-benchmark actually ran.

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
- **SSE backpressure** — a bounded subscriber buffer paired with a slow
  consumer should drop the oldest surplus in default mode; the
  `drop_rate` metric records this. `?slow=block` is the opt-in
  stronger-delivery mode and intentionally trades emitter latency for
  delivery.
- **Cron tick** — scanning N jobs at minute boundaries. ~175µs for N=1000
  baseline; budget for in-process schedulers running many jobs.

## Tier 4 — what to look for

- **AutoMigrate re-run** — the "safe to run on every boot" claim. Should
  stay under ~10ms even at 50 entities. Postgres is much slower than
  SQLite because the live-schema read (one bulk
  `information_schema.columns` query feeding both existence and
  column-add detection) is a network round-trip.
- **Schema diff** — same shape, but pays for the full information_schema
  lookup. Acceptable as a one-shot CLI command, not as a hot path.
- **In-memory search** — confirms O(corpus) scan. 10k docs ≈ 3ms per
  query is fine for tests/demos; production needs a real backend.

## Tier 5 — TechEmpower-style endpoints

The six canonical comparable workloads. Numbers here can be cross-
referenced with the published TechEmpower Framework Benchmarks
(techempower.com/benchmarks) to see roughly where GoFastr sits next to
Gin/Echo/Fiber/Actix/etc.

| Bench               | Workload                                            |
|---------------------|-----------------------------------------------------|
| `Plaintext`         | Return `Hello, World!`. Pure routing + write cost. |
| `Plaintext_WithDefaults` | Same, through default middleware chain.       |
| `JSON`              | Encode `{"message":"Hello, World!"}`.              |
| `SingleQuery`       | `GET /worlds/{id}` — one row by PK.                |
| `MultiQuery`        | Same, N times per request (1/5/10/20).             |
| `FortunesLike`      | List a small full table — closest API analogue.    |
| `Updates`           | GET-modify-PUT N rows (1/5/10/20).                 |

Throughput in req/s = `1e9 / ns_per_op`. Compare with caution: the
TechEmpower harness uses real network listeners + multiple client
connections, while these run through `httptest`'s in-memory `ResponseRecorder`,
which is faster than the wire. The relative shape (Plaintext > JSON >
SingleQuery > MultiQuery > Updates) holds; absolute numbers don't
translate one-to-one.

## Tier 6 — latency percentiles + concurrency

`BenchmarkT6_*` benchmarks record per-operation latencies and emit
`p50_ns`, `p90_ns`, `p99_ns`, `p999_ns`, and `max_ns` via
`b.ReportMetric`. Mean ns/op hides the tail; the percentiles surface it.

Parallelism is exercised via `b.RunParallel` + `b.SetParallelism(N)`,
where N is the multiplier over `GOMAXPROCS`. So `parallelism=8` on an
8-core machine gives 64 worker goroutines.

What to look for:

- **p99 ÷ p50 ratio** — under load, anything above 3× means significant
  tail growth. SQLite write-heavy workloads will hit this immediately
  because writes serialise.
- **List concurrency slope** — read-only list endpoints should scale
  near-linearly on Postgres (no write lock) but stay flat (or get worse)
  on SQLite (single connection).
- **Mixed RW reads vs writes ratio** — should match the 9:1 mix the
  benchmark drives. If reads drastically outnumber that, writes are
  starving on the lock.

## Tier 7 — stdlib baselines (the overhead tax)

Hand-rolled `net/http` + `database/sql` implementations of plaintext,
JSON, single-query, and filtered list endpoints — paired with the same
endpoints expressed through the framework. The delta is what the
declare-once surface actually costs.

What to look for:

- **Plaintext + JSON deltas should be in the 5-15% range.** These paths
  go through the router and not much else; bigger overhead suggests
  router work that should be inlined.
- **SingleQuery delta exposes the entity + CRUD machinery.** Expect 20-
  30% overhead from filter parsing, case conversion, projection logic.
- **FilteredList delta is the worst case** (allocations dominate).
  This is the hottest optimization target.

## Tier 9 — UI runtime: streams, islands, full SSR

The framework's value proposition isn't just JSON CRUD — it's the
SSR-with-hydration runtime, the SSE/island plumbing, and the UI host.
These benchmarks measure those paths.

| Bench | Workload |
|-------|----------|
| `StreamingListRealVolume` | Calls `serveStreamingList` directly with 1k / 5k / 10k row limits — bypasses parsePagination's ≤100 cap. The honest streaming-throughput measurement. |
| `StreamingVsBuffered_RealVolume` | Buffered (50 paginated requests × 100 rows = 5000) vs streaming (one request × 5000). Apples-to-apples comparison at real volume. |
| `SSEEventStream` | Subscribes to `/posts/_events` via the real handler, fires 500 events, reports `events_delivered`, `events_dropped`, `delivery_ratio`, `bytes_received`. |
| `IslandRPC` | One island RPC swap end-to-end: GET against an island endpoint that renders ~10 row fragments. Measures the canonical "click → fetch → swap" pattern. |
| `IslandRPC_Concurrency` | Same handler under `b.RunParallel` at parallelism 1/8/64, with p50/p90/p99 reporting. |
| `UIHostPageRender` | Full SSR through `uihost.Mount`: layout + screen + runtime injection. Two screens (simple + 50-item list). |

### What to look for

- **Streaming wins over buffered-paginated** at any volume that pays
  network round-trips. The first SQLite smoke run showed 14ms vs 15ms
  (small win); Postgres showed 12ms vs 46ms — a **3.9× win** because
  one query beats 50 round-trips.
- **SSE delivery_ratio** ≪ 1.0 under bursty emit. Default subscribers
  drop oldest events on overflow; this is documented behaviour. A
  regression means the ratio drops further at a fixed emit rate.
- **Island RPC** should stay sub-100µs for a small (~10-row) fragment.
  This is the response-time floor for "click → see new content."
- **UIHostPageRender** vs `BenchmarkT7_JSON_GoFastr` (~500ns) tells
  you what SSR + hydration shell adds over a bare framework JSON
  response — expect 50-100µs at minimum because of the HTML tree
  build and runtime script injection.

### Caveats

- `httptest.ResponseRecorder` removes wire latency and flushing
  semantics. SSE numbers are **lower bounds on encoding cost**, not
  RPS over a real network.
- Real island deployments include client-side hydration time that
  these benchmarks don't measure (Go can't time JS). Wire that in via
  Playwright if it matters.

## Resource benchmarks (separate from the Tier 1-9 Go bench suite)

Resource numbers — binary size, peak RAM during `go build`, idle and
under-load RAM of each running binary — are produced by a separate
runner under `cmd/bench-resources/` rather than `go test`. They aren't
expressed as Go benchmarks because the unit of measurement is "one
fully-built and warmed binary", not "one operation".

```bash
make bench-resources             # default LOAD=200 requests per app
LOAD=500 make bench-resources    # override
```

Three bench apps under `benchmarks/apps/<name>/`:

| App       | Surface                                                              |
|-----------|----------------------------------------------------------------------|
| `minimal` | `NewApp` + one plaintext route. No DB, no entities. Establishes the floor. |
| `crud`    | One entity, SQLite + auto-migrate + CRUD routes.                     |
| `full`    | Upper bound — every supported framework surface wired on: three related entities with relations, audit log, cron, MCP, UI host + one screen, file storage, in-memory search backend, RolePolicy + RequirePermission, multi-tenant scope, custom endpoints, plugin, OpenAPI + Swagger UI, lifecycle hooks. |

Plus the two cmd binaries (`gofastr`, `kiln`) for build-only comparison.

Output is Markdown to stdout and `dist/bench/resources.md`:

```
| App         | Bin size | Build wall | Build peak RAM | Idle RAM | Loaded RAM | Load reqs/dur |
| minimal     |  8.8 MB  |   5.4s     | 311.3 MB       | 11.0 MB  | 12.5 MB    | 200 / 12ms    |
| crud        | 12.9 MB  |  16.7s     | 759.8 MB       | 13.7 MB  | 14.8 MB    | 200 / 16ms    |
| full        | 13.5 MB  |  16.9s     | 752.8 MB       | 14.8 MB  | 16.2 MB    | 200 / 17ms    |
```

### What to look for

- **Bin size delta `crud` − `minimal`** is the cost of the SQLite cgo
  driver. Switching to a pure-Go driver (`modernc.org/sqlite`) would
  cut this in half but slow the binary a few %.
- **Bin size delta `full` − `crud`** is what every other framework
  surface costs at once: UI host, file storage, search backend, audit,
  cron, MCP, access control, multi-tenant, OpenAPI/Swagger, plugins.
  About **+0.6 MB** total — they're code paths inside the framework
  and its sibling packages, not separate binaries' worth of code.
- **Build peak RAM** is mostly the cgo toolchain. CI machines need to
  budget ~1 GB headroom for the compile step.
- **Idle vs Loaded RAM** should be roughly equal after a warmup. A
  significant climb under load means GC pressure, retained slices, or
  goroutine leaks — pair with Tier 8's `HeapAfterLoad`.

### Dev-server RAM

`scripts/dev-watch.sh` rebuilds the compiled binary on change and
re-runs it. Its long-running RAM is the same as the running binary
(see Idle RAM column for `full`), with brief spikes to the build peak
(~750 MB on cgo builds) during rebuilds. There is no separate
dev-server overhead worth measuring — it's a thin shell loop around
`go build` + `exec`.

## Tier 8 — operational

- **`ColdStart_*`** — time from `NewApp` through first request served.
  Reported as ns/op; with `benchtime=1x` that's effectively the cold-
  start latency for a single binary instance.
- **`HeapAfterLoad`** — drives 5000 list requests, then reads
  `runtime.MemStats`. Reports `heap_alloc_bytes` (live heap),
  `heap_objects`, `mallocs`, `frees`, `gc_pause_total_ms`, `gc_cycles`.
  A regression in live heap means we're retaining state across
  requests; a regression in pause time means GC pressure grew.
- **`GoroutinesAfterLoad`** — sanity check for goroutine leaks.
  `gor_delta` should always be 0 (or very small).

The Tier 8 benchmarks deliberately use `-benchtime=1x` because they're
property assertions, not iteration timings. Reading per-run values is
the point.

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

## Improvement opportunities

The prioritized list of optimizations these benchmarks surfaced —
together with the specific bench that exposed each one and a verify
step — the verification results live in [`perf-results.md`](perf-results.md).
Update it whenever a new hotspot appears or a fix lands.
