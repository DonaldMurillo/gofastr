# Performance opportunities

A prioritized list of improvements the benchmark suite has surfaced.
Every item names the benchmark that exposed it so the win can be
verified after a fix, and the priority reflects "ratio of impact to
scope" — not just raw speedup.

Snapshot baseline: darwin/arm64, M4 Pro, Go 1.26, SQLite in-memory and
Postgres-16-alpine via testcontainers. Numbers cited are from the
stable runs in commits `89a297c`..`48b9bda`.

---

## P0 — biggest win per unit of work

### 1. Default middleware chain is **200× the no-defaults cost**

`BenchmarkMiddleware_DefaultChain` reports 268µs with the default
chain and 1.3µs without. The `Logging()` middleware writes a
structured line to stdout per request — that's the bulk of the gap.

**Where:** `core/middleware/logging.go`, wired in `framework/app.go`'s
`applyDefaultMiddleware`.

**Options:**
- Add a `WithLoggingWriter(io.Writer)` option and discard by default
  in tests.
- Switch to a sampled/buffered logger when `GOFASTR_ENV != "dev"`.
- Disable logging in the default chain entirely; require explicit
  opt-in. This is the most aggressive change but matches the
  framework's "no implicit cost" philosophy.

**Verify:** `make bench-tier2` — the with/without delta should
collapse to ≤10×.

---

### 2. `parsePagination` clamps `?limit=` to ≤100, hiding the streaming win

`BenchmarkT9_StreamingVsBuffered_RealVolume/postgres` shows streaming
beats buffered-paginated **4× at 5000 rows** (12.6ms vs 50ms). But
clients can't reach that workload through the HTTP surface because
`framework/crud.go:parsePagination` caps `?limit=` at 100, and the
`streamListThreshold = 1000` auto-trigger is therefore unreachable.

**Where:** `framework/crud.go:516` (the `n <= 100` guard).

**Options:**
- Raise the cap and let `streamListThreshold` auto-route large pages
  through the streaming path. Make the cap configurable per-entity.
- Add an explicit `?stream=true` that bypasses the cap and uses the
  caller-supplied limit (the streaming handler already accepts a
  separate `limit` param).

**Verify:** `make bench-tier9` — the
`StreamingVsBuffered_RealVolume/postgres/buffered-paginated-5000`
case should disappear (one request, not 50).

---

### 3. FilteredList is **+127% slower vs hand-rolled** `net/http`

`BenchmarkT7_FilteredList_GoFastr/sqlite` is 161µs / 3187 allocs;
the hand-rolled `BenchmarkT7_FilteredList_NetHTTP/sqlite` is 71µs /
1881 allocs. Same SQL, same JSON output. The framework's list
handler does include parsing, filter parsing, soft-delete check,
tenant scope check, projection resolution, JSON casing — all on
every request.

**Where:** `framework/crud.go` `List()`, plus all the
`apply*` and `parse*` helpers it calls.

**Options:**
- Precompute per-entity at registration time: the column list, the
  fixed-attribute fields, the visible-field set, the convert-key
  function. Reuse across requests instead of recomputing.
- Skip `parseIncludeTree`, `parseFilters`, `parseNestedFilters`,
  `parseSort`, `applySoftDeleteFilter`, `applyTenantScope` when the
  entity hasn't enabled the corresponding feature. Right now every
  list call goes through every parser even when none could fire.
- Pool the `[]map[string]any` row slice via `sync.Pool`.

**Target:** halve the gap (-127% → -60%).

**Verify:** `make bench-tier7` — `FilteredList_GoFastr` vs
`FilteredList_NetHTTP` delta.

---

### 4. JSON case conversion: **26 allocations per row, twice on write paths**

`BenchmarkJSONCasing/snake→camel` reports 19µs / 1048 B / 26 allocs
for a 10-key row. `camel→snake` 7µs / 19 allocs. List endpoints run
`snake→camel` once per row × rows-per-page; write endpoints run
both. That's the single biggest per-request allocation pressure.

**Where:** `framework/casing.go`.

**Options:**
- Pre-compute the camel↔snake mapping for every entity field at
  `Define()` time and use the cached table on every conversion
  (eliminates per-call `strings.Split` + `unicode.ToUpper`).
- Pool a `strings.Builder` via `sync.Pool` for the rare miss.
- Reuse `mapToCamelCase`'s output map across requests (pool of
  `map[string]any`, cleared on return).

**Verify:** `make bench-tier2` — `JSONCasing` snake→camel should
drop below 10 allocs.

---

## P1 — solid wins, contained scope

### 5. SchemaDiff at **59ms for 50 entities on Postgres**

`BenchmarkSchemaDiff/postgres/N=50` does N round-trips to
`information_schema.columns` because the diff inspects one entity at
a time. One `WHERE table_name IN (...)` query would do it.

**Where:** `framework/schema_diff.go:DiffSchema`.

**Options:**
- Single bulk query: `SELECT table_name, column_name, data_type FROM
  information_schema.columns WHERE table_name IN ($1, $2, …)`.
- Cache per table on AutoMigrate path (already paid for it once).

**Target:** 5-10× faster.

**Verify:** `make bench-tier4` — `SchemaDiff/postgres/N=50` should
drop to ~10ms.

---

### 6. AutoMigrate idempotent re-run at **7.5ms for 50 entities (Postgres)**

`BenchmarkAutoMigrate_Idempotent/postgres/N=50` — same root cause as
#5: one existence check per entity. The "safe to run on every boot"
claim survives at 50 entities, breaks down at 500.

**Where:** `framework/migrate.go:AutoMigrate`.

**Options:**
- Single bulk `pg_tables` query to know which tables exist before
  the per-entity loop, then skip the existence probe.

**Target:** sub-1ms regardless of entity count.

**Verify:** `make bench-tier4`.

---

### 7. CronTick allocates **1471 times per minute for 1000 jobs**

`BenchmarkCronTick/N=1000` reports 175µs / 213KB / 1471 allocs per
tick. The snapshot copy + per-job match check allocates fresh slices.

**Where:** `framework/cron.go:runOnce`.

**Options:**
- Replace `jobs := make([]scheduledJob, len(s.jobs)); copy(jobs, s.jobs)`
  with a read-locked iteration if mutations during tick are rare.
- Pre-sort jobs by next-fire time so the tick can break early after
  the first non-match.

**Target:** ≤1 alloc per tick regardless of N.

**Verify:** `make bench-tier3`.

---

### 8. DSL parser allocates on every call

`BenchmarkDSLParse/complex` is 6µs / 7 allocs. Agents often issue
the same query template repeatedly; parsed `DSLQuery` could be
cached by input string.

**Where:** `framework/dsl.go:ParseDSL`.

**Options:**
- Add a `sync.Map[string]DSLQuery` LRU keyed by the raw input.
- Bound the cache to a reasonable size so adversarial inputs can't
  exhaust memory.

**Target:** ~50ns / 0 allocs on cache hit.

**Verify:** `make bench-tier2`.

---

## P2 — broader scope, real benefit

### 9. SSE backpressure drops half the burst

`BenchmarkSSE_BackpressureDropRate`: drop_rate **0.99** at 5000
events through a 32-buffer + slow consumer. `BenchmarkT9_SSEEventStream`:
delivery_ratio **0.48** at 500 events end-to-end. Documented
behaviour, but the buffer cap is hardcoded.

**Where:** `framework/crud_events.go:EventStream` (`buf := make(chan
Event, 32)`).

**Options:**
- Per-subscriber buffer size via a query param or header
  (`?buffer=128`).
- Per-entity default via `EntityConfig.EventBuffer int`.
- Add an alternative `?slow=block` mode for clients willing to
  trade latency for delivery.

**Verify:** `make bench-tier9` — delivery_ratio should improve at a
higher buffer setting.

---

### 10. UI host page render is **15× a bare JSON encode**

`BenchmarkT9_UIHostPageRender/` is 7.6µs / 580 bytes for a trivial
page. `BenchmarkT7_JSON_GoFastr` is 500ns. The factor is the HTML
tree build + runtime script injection.

**Where:** `framework/uihost/uihost.go`, `core/render/html.go`.

**Options:**
- Pool the `strings.Builder` that backs `render.HTML`.
- Cache the runtime script tag string (currently rebuilt per
  request).
- Pre-flatten the layout shell so only the screen render varies per
  request.

**Target:** halve render time for trivial pages.

**Verify:** `make bench-tier9`.

---

### 11. Island RPC tail latency at parallelism=64

`BenchmarkT9_IslandRPC_Concurrency/parallelism=64`: p50 13µs, p99
**65ms**. The wide gap is contention through the recorder + render
allocations. Real production runs use a network connection per
request — the contention shifts but doesn't disappear.

**Where:** `core/render/render.go:RespondHTML`, per-handler
allocations.

**Options:**
- Pool the rendered output buffer at the handler entry.
- Pre-render static page chrome once at startup.

**Verify:** `make bench-tier9` — p99 at par=64 should drop below
10ms.

---

### 12. Filtered list overhead allocations

Re-stated from #3: 3187 allocs vs 1881 hand-rolled. The chain is:
include parse, filter parse, nested filter parse, sort parse,
projection resolve, count query, data query, scan rows, eager load,
JSON case convert, encode. Each step allocates fresh slices/maps.

**Options:** the precompute-at-register approach from #3, plus:
- Pool the response writer's byte buffer.
- Switch `[]map[string]any` to a typed result struct when the entity
  has a generated model (the `framework/typed_query.go` path
  already supports this — auto-detect and route to it).

---

## Doc-only fixes (no code change required)

### 13. SQLite write serialisation under load

`BenchmarkT6_CreateConcurrency/sqlite3/parallelism=64`: p99 climbs
to **112ms**. `BenchmarkT6_MixedRW/sqlite3/parallelism=64`: only 10
writes complete out of 5072 ops.

**Action:** Update `docs/migrations.md` and `docs/security.md` with
a "Concurrency model" callout for SQLite. The framework already
sets `MaxOpenConns(1)` in test helpers; users should know to do the
same in production or pick Postgres.

---

### 14. cgo SQLite costs 4MB binary + 440MB build RAM

Resource bench: `crud` is 12.9MB / 760MB build RAM; `minimal` is
8.8MB / 311MB. The delta is the cgo SQLite driver.

**Action:** Document `modernc.org/sqlite` as a pure-Go alternative
in `docs/migrations.md`. Trade-offs: pure-Go is a few % slower at
query time but saves ~4MB binary, ~440MB build RAM, and eliminates
the cgo toolchain dependency.

---

## How to track progress

For each item with a `verify` line:

```bash
# Capture before:
make bench-<tier> BENCH_COUNT=10 BENCHTIME=1s
mv dist/bench/<tier>.txt dist/bench/<tier>-before.txt

# Make the fix, then:
make bench-<tier> BENCH_COUNT=10 BENCHTIME=1s
benchstat dist/bench/<tier>-before.txt dist/bench/<tier>.txt
```

`benchstat` reports geometric mean + p-value so noise doesn't
register as a regression.

## What's NOT on this list

A few things the benchmarks measured well that don't need fixing
right now:

- **Router lookup** — gofastr ~180ns flat from N=1 to N=1000 vs
  ServeMux ~125ns. The tree match is fine; the 50ns delta isn't
  worth chasing.
- **Cursor pagination** — flat across pages (Tier 1.2). Working as
  designed.
- **Includes vs N+1** — 35× faster than naive on Postgres (Tier 1.1).
  The claim holds.
- **Heap retention under load** — Tier 8 shows 778KB live heap after
  5000 list requests. No leaks.
- **Goroutine leaks** — Tier 8 `gor_delta = 0` after 2000 requests.
  Clean.
