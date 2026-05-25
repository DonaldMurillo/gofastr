# GoFastr Project Architecture Review

Date: 2026-05-08 (updated)

A full review of the current tree (218 Go files). Each finding was re-verified against the source. Fixed findings from prior reviews have been removed; stale findings are preserved with updated line references.

## Architecture Summary

GoFastr is a Go fullstack framework built in four layers:

- **`framework/`** — public application/entity layer. Registers entities, auto-generates CRUD routes, OpenAPI output, hooks, events, plugins, migrations, debug endpoints, and app startup. PayloadCMS-inspired declarative API.
- **`core/`** — 12 low-level reusable primitives: router, handler binding, query builders, middleware, OpenAPI builder, static serving, uploads, streaming, MCP, schema validation, and migrations. Standalone-usable.
- **`core-ui/`** — server-rendered UI system: app/router/layout model, DI container (`di/`), HTML primitives (`html/`), composed patterns (`patterns/`), reactive signals, islands/SSE, dev server, runtime JavaScript, styling/theming, and a `.ui.go` linter (`check/`).
- **`battery/`** — optional pluggable backends: auth (JWT, sessions, password hashing), cache (in-memory, Redis), email (SMTP, log), queue (in-memory, Redis), storage (local, S3, in-memory).
- **`cmd/gofastr/`** — CLI for `init`, `dev`, `build`, `generate`, `migrate`, `test`.

### What's Working Well

- Clean package boundaries with minimal external dependencies (stdlib-first).
- The entity declarative API is elegant and the CRUD route generation is well-structured.
- Signal system (`core-ui/signal/`) has a clean reactive model with computed values and effects.
- Island architecture (`core-ui/island/`) separates server rendering from live updates cleanly.
- The router correctly uses Go 1.22+ `ServeMux` pattern syntax with `{id}` parameters.
- Tests pass cleanly under `go test ./...` and `go vet ./...` is clean.
- The DI container is simple and functional.
- Component/action model with server-side Go handlers and compiled client JS is a nice design.

---

## P1 Findings — Data Loss / Security / Crash

### Finding 1 — Generated init project does not compile

**Files:** `cmd/gofastr/init.go:81-113`, `cmd/gofastr/init.go:145-154`, `cmd/gofastr/init.go:182-240`

Three compilation-breaking bugs in the scaffolded project:

1. **Missing `context` import.** Generated `main.go` calls `migrator.Up(context.Background())` but the template doesn't import `"context"`.
2. **Type mismatch on `CRUD` field.** The entity config emits `CRUD: true` (bool literal), but `EntityConfig.CRUD` is `*bool`. The app logic checks `config.CRUD != nil` then `*config.CRUD`, so a bare `true` won't compile.
3. **Double `go mod init`.** The scaffold writes a `go.mod` file, then runs `go mod init`, which fails because `go.mod` already exists.

**Impact:** The first-run experience (`gofastr init myapp && cd myapp && go build`) is completely broken.

**Fix:**
1. Add `"context"` to the generated import block.
2. Change `CRUD: true` → `CRUD: boolPtr(true)` and add a `boolPtr` helper.
3. Remove the manual `go.mod` write or skip `go mod init` when it exists.

---

### Finding 2 — Default SQLite scaffold is incompatible with the migration runner

**Files:** `cmd/gofastr/init.go:87-96`, `core/migrate/runner.go:24-31`, `core/migrate/runner.go:110-114`

The scaffold defaults to `sqlite3`, but:

1. `CreateMigrationsTable` uses `DEFAULT NOW()` — PostgreSQL syntax. SQLite doesn't recognize `NOW()`.
2. `runMigrationUp` records migrations with `$1` placeholders — PostgreSQL parameter style. SQLite uses `?`.
3. `appliedVersions` reads `applied_at` as a `time.Time` via `Scan`, but SQLite stores timestamps as strings without a driver-level conversion guarantee.

**Impact:** A generated app is steered into a migration system that fails on its default database.

**Fix:** Either (a) switch the default to PostgreSQL, or (b) make the migration runner dialect-aware with a `Dialect` option (`"postgres"` | `"sqlite"`) that selects the correct placeholder style and timestamp functions.

---

### Finding 3 — Multitenancy is declarative but not enforced by CRUD routes

**Files:** `framework/tenant.go:38-52`, `framework/crud.go:110-143`, `framework/crud.go:210-275`, `framework/crud.go:279-347`

`WithMultiTenant` sets `Entity.Config.MultiTenant = true`. `TenantMiddleware` correctly extracts the tenant ID from headers and stores it in context. `ApplyTenantFilter` and `InjectTenantID` are available as helpers.

However, `CrudHandler.List/Get/Create/Update/Delete` never:
- Read tenant context from the request
- Call `ApplyTenantFilter` on queries
- Call `InjectTenantID` before inserts
- Scope updates/deletes to the current tenant

**Impact:** An entity marked multitenant can list, read, update, and delete records across all tenants. This is a data isolation failure.

**Fix:** In each CRUD handler, after extracting the request context:
```go
if ch.Entity.Config.MultiTenant {
    tenantID := GetTenantID(r.Context())
    if tenantID != "" {
        qb.Where("tenant_id = $1", tenantID)  // or ApplyTenantFilter(qb, tenantID)
    }
}
```
And in `Create`, inject `tenantID` into the data map before insert.

---

### Finding 4 — Soft-delete records remain visible through normal CRUD reads

**Files:** `framework/crud.go:110-143` (List), `framework/crud.go:175-205` (Get), `framework/softdelete.go`

`ApplySoftDeleteFilter` is defined and works correctly, but `CrudHandler.List()` and `CrudHandler.Get()` never call it. Records with `deleted_at` set remain fully visible.

Additionally, the delete path (crud.go:360) writes `"NOW()"` as a parameter value through `qb.Set("deleted_at", "NOW()")`. The query builder treats this as a literal string parameter, not a SQL function. Depending on the driver, this stores the literal string `"NOW()"` rather than a timestamp.

**Impact:** Soft-deleted records are not actually hidden, and the deleted_at value may be incorrect.

**Fix:**
1. In `List()` and `Get()`, after building the query:
   ```go
   if ch.Entity.Config.SoftDelete {
       showTrashed := r.URL.Query().Get("trashed") == "true"
       if !showTrashed {
           qb.Where("deleted_at IS NULL")
       }
   }
   ```
2. For the delete path, use a raw SQL expression or pass `time.Now().UTC()` as a parameter value instead of the string `"NOW()"`.

---

### Finding 5 — Timeout middleware races the response writer

**File:** `core/middleware/timeout.go:18-28`

Confirmed by `go test -race ./core/middleware`:
```
DATA RACE: concurrent writes to http.ResponseWriter
```

`Timeout` invokes `next.ServeHTTP(w, r)` in a goroutine using the original `http.ResponseWriter`. When the deadline expires, the parent goroutine writes a 504 via `http.Error(w, ...)`. Both goroutines can write headers/body concurrently.

**Impact:** Race condition on every timed-out request. Corrupted response, potential panic.

**Fix:** Use a `responseWriterWrapper` that guards writes with a sync mechanism, or use `http.NewResponseController` with a separate writer for the timeout branch. Common pattern:

```go
type safeWriter struct {
    http.ResponseWriter
    mu     sync.Mutex
    timedOut bool
}
```

---

### Finding 6 — Dynamic route params mutate shared screen/component state — RESOLVED

**Files:** `core-ui/app/app.go`, `core-ui/app/screen.go`

Earlier versions stored route params on a shared `Screen.routeParams` field that concurrent requests could overwrite. Resolved by per-request component instancing (see `screen.newInstance()` — shallow-copies the registered template, applies `SetParams` only to the request-local instance). The `Screen.routeParams` field and `Screen.RouteParams()` method have been removed.

---

### Finding 7 — Overlay rendering contract is split between server and runtime

**Files:** `core-ui/app/screen.go:154-167`, `core-ui/app/app.go:144-168`

`Screen.Render()` for drawer/sheet/dialog returns raw component content (no ARIA wrapping). Comments say the runtime handles structural wrapping. But:

1. Direct server-side rendering of overlay paths (no client JS) produces unwrapped, inaccessible markup.
2. `RenderPage` for overlays skips layout and uses `screen.Render()`, returning bare content without `role="dialog"`, `aria-modal="true"`, or backdrop.
3. Tests that exercise server-side overlay rendering verify raw content, not accessible markup.

**Impact:** Overlays accessed via direct URL or server rendering are not accessible. Screen readers cannot identify them as dialogs/drawers.

**Fix:** Move ARIA wrapping into `Screen.Render()` for all overlay types. The runtime can still add backdrop behavior, but the server output should be self-contained and accessible:
```go
case ScreenDialog:
    return render.Tag("div", map[string]string{
        "role": "dialog", "aria-modal": "true", "aria-label": s.Title,
    }, content)
```

---

### Finding 8 — Redis queue ack/nack semantics are unsafe

**Files:** `battery/queue/redis.go:58-91` (Dequeue), `battery/queue/redis.go:99-102` (Ack/Nack)

1. **Ack deletes the entire processing key** (`Del(ctx, q.processingQueue)`) instead of removing one job from the hash. This loses tracking for all other in-flight jobs.
2. **Nack is a no-op.** Failed jobs are never retried or moved to the dead letter queue.
3. **Visibility timeout recovery is not implemented.** Abandoned jobs stay in the processing hash forever.
4. **Dequeue ignores type filters.** `types...` parameter is accepted but never checked, unlike `MemoryQueue.Dequeue` which correctly filters.
5. **Enqueue doesn't apply defaults.** Unlike `MemoryQueue.Enqueue` which generates IDs, timestamps, and sets `MaxAttempts`, `RedisQueue.Enqueue` marshals the job as-is. Jobs without an ID get stored under an empty hash field.
6. **The `RedisClient` interface lacks `HDel`.** The comment on line 102 acknowledges this.

**Impact:** The Redis queue cannot safely process more than one concurrent job. Retries and dead-letter behavior are nonfunctional.

**Fix:** Add `HDel` to the `RedisClient` interface. Rewrite `Ack` to use `HDel(ctx, q.processingQueue, jobID)`. Implement `Nack` with retry logic. Add type filtering in `Dequeue`. Apply defaults in `Enqueue`.

---

## P2 Findings — Incorrect Behavior

### Finding 9 — Create accepts read-only and hidden fields

**File:** `framework/crud.go:241-254`

`Create` iterates all entity fields and inserts any value present in the request body unless the field is auto-generated. Fields marked `ReadOnly` or `Hidden` (but not auto-generated) can still be supplied by clients. This includes server-owned fields like `deleted_at`, `tenant_id`, or internal status fields.

**Impact:** Clients can set fields they shouldn't be able to write.

**Fix:** In `Create`'s field iteration, skip fields where `f.ReadOnly` or `f.Hidden`:
```go
for _, f := range ch.Entity.GetFields() {
    if f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
        continue
    }
    // ... accept field from body
}
```

---

### Finding 10 — Hook registry is not connected to CRUD lifecycle

**Files:** `framework/app.go:177-185` (HookRegistry), `framework/crud.go:210-347` (CRUD handlers)

The app exposes per-entity hook registries via `App.HookRegistry(entityName)`, and `HookRegistry.ExecuteHooks` works correctly. However, `CrudHandler` has no reference to `App` or the hook registries. None of the CRUD handlers call `ExecuteHooks` for any lifecycle event (BeforeCreate, AfterCreate, etc.).

Tests verify the registry as a standalone helper, not the actual lifecycle integration.

**Impact:** Hooks declared via the API are silently ignored. Users expect `BeforeCreate` to run but nothing happens.

**Fix:** Pass the `App` (or a `HookProvider` interface) to `CrudHandler`. In each CRUD handler, look up the hook registry for the entity name and execute the appropriate hooks.

---

### Finding 11 — OpenAPI filter parameters don't match parser behavior

**Files:** `framework/entity_openapi.go:86-99`, `framework/filter.go:49-107`

The OpenAPI generator documents filter names in camelCase (e.g., `createdAt`, `createdAt_gt`). But `ParseFilters` accepts schema field names as-is (e.g., `created_at`, `created_at_gt`). For snake_case fields, the spec advertises parameters that the server doesn't recognize, and the actual working parameters aren't documented.

**Impact:** API consumers using the OpenAPI spec will send query parameters the server ignores.

**Fix:** Either (a) make `ParseFilters` accept camelCase and convert internally, or (b) have the OpenAPI generator document the actual field names the parser accepts.

---

### Finding 12 — Custom 405 handler is dead API

**File:** `core/router/router.go:125-148`

`MethodNotAllowed` stores a handler, but `ServeHTTP` only branches when `pattern == ""` (no route matched). Go 1.22's `ServeMux` handles method-not-allowed internally — it returns a 405 before reaching our custom check. The stored handler is never invoked.

**Impact:** Users who set a custom 405 handler get the default Go 405 response instead.

**Fix:** Remove the `MethodNotAllowed` API, or implement a custom method-matching layer before delegating to `ServeMux`.

---

### Finding 13 — Debug stats endpoint exposed by default without auth opt-in

**File:** `framework/app.go:191-210`, `framework/app.go:247-275`

`App.Start` always registers `/.debug/stats` returning PID, uptime, goroutine count, Go version, memory stats, entity count, and app metadata. There's no opt-in/opt-out setting and no auth hook.

**Impact:** Information disclosure in production. Attackers gain system-level metrics.

**Fix:** Add a `Config.DebugEndpoints bool` (default `false` in production). Only register when enabled. Alternatively, require an auth middleware for `/.debug/*`.

---

### Finding 14 — Auto-migration SQL defaults are not escaped

**File:** `framework/migrate.go:41-43`, `framework/migrate.go:95-111`

`sqlDefault` wraps string defaults in single quotes without escaping embedded quotes. A field with `Default: "Bob's post"` generates:
```sql
DEFAULT 'Bob's post'  -- invalid SQL
```

**Impact:** Migration fails for common default values containing apostrophes.

**Fix:** Escape single quotes by doubling them: `strings.ReplaceAll(v, "'", "''")`.

---

### Finding 15 — OpenAPI path conversion only supports colon params

**Files:** `core/openapi/spec.go:40-66`, `core/openapi/spec.go:129-150`

The core router uses Go 1.22 patterns (`/users/{id}`), but the OpenAPI builder auto-converts only colon-style params (`:id` → `{id}`). If custom users register router-native `{id}` paths with the OpenAPI builder, path parameters won't be extracted. Entity OpenAPI currently passes colon paths, so this works for the default case, but custom endpoint registration could break.

**Impact:** OpenAPI spec missing path parameters for custom endpoints using `{id}` syntax.

**Fix:** Support both `:id` and `{id}` conversion in the OpenAPI builder.

---

### Finding 16 — Static directory serving rejects normal files on Unix

**File:** `core-ui/devserver/devserver.go:293-305`

The traversal protection uses `filepath.Join(ds.staticDir, filepath.Clean("/"+path))`. Since `filepath.Clean("/"+path)` produces an absolute path on Unix, `filepath.Join` drops `ds.staticDir`. The resulting path fails the prefix check and falls through to page rendering.

Example: request `/safe.txt` → `filepath.Join("/static", "/safe.txt")` → `/safe.txt` → prefix check against `/static` fails → file not served.

**Impact:** Static file serving is completely broken on Unix when using `WithStaticDir`.

**Fix:** Use `filepath.Join(ds.staticDir, path)` (no `filepath.Clean("/"+...)`), or compute the relative join properly:
```go
filePath := filepath.Join(ds.staticDir, filepath.Clean(path))
```

---

### Finding 17 — Signal update endpoint ignores submitted signal values

**File:** `core-ui/devserver/devserver.go:463-490`

`handleSignalUpdate` decodes the body and extracts `signalID`, but the line `_ = signalID` discards it. The function never updates any signal registry or applies the submitted value. It only re-renders islands and returns `{"status":"ok"}`.

**Impact:** Signal updates from the client are silently ignored. Server-side state is unchanged. Islands re-render but with stale data.

**Fix:** Look up the signal by ID, apply the submitted value, then re-render islands.

---

### Finding 18 — Island update sends can panic during unsubscribe race

**File:** `core-ui/island/manager.go:115-137`

`Push` reads the stream channel under `RLock`, releases the lock, then sends to the channel. `Unsubscribe` can close that channel between the unlock and the send. `PushUpdate` has the same pattern.

**Impact:** Send on closed channel → panic during SSE disconnect/update races.

**Fix:** Use a done channel pattern or protect the close+delete atomically:
```go
// In Unsubscribe, don't close the channel. Instead, send a sentinel.
// Or protect the send with a per-session mutex.
```

---

### Finding 19 — Action extraction can deadlock after panic

**File:** `core-ui/component/actions.go:125-131`

`ExtractActions` locks `extractMu` but does not use `defer` for unlock. If `ic.Actions()` panics, the mutex remains locked. Subsequent calls to `ExtractActions` deadlock forever. Additionally, `currentRegistry` is not restored on panic, corrupting future extractions.

**Impact:** One panicking component action definition deadlocks the entire action compilation system.

**Fix:**
```go
func ExtractActions(c Component) *ActionRegistry {
    ic, ok := c.(InteractiveComponent)
    if !ok {
        return NewActionRegistry()
    }
    extractMu.Lock()
    defer extractMu.Unlock()
    reg := NewActionRegistry()
    prev := currentRegistry
    currentRegistry = reg
    defer func() {
        currentRegistry = prev
        recover() // or let it propagate after cleanup
    }()
    ic.Actions()
    return reg
}
```

---

### Finding 20 — DI failures are logged but rendering continues with nil dependencies

**Files:** `core-ui/app/app.go:134-137` (RenderPage), `core-ui/app/app.go:233-236` (RenderPartial), `core-ui/app/di.go:103-140`

`Inject` returns an error when a provider is missing, but `RenderPage`/`RenderPartial` log it and continue rendering. The container's `Inject` method also skips missing providers silently in some paths.

**Impact:** Screens render with nil struct fields. This can cause nil pointer panics in rendering code, producing partial or broken UI.

**Fix:** Return the DI error to the caller, or render a clear error page instead of proceeding with nil deps.

---

### Finding 21 — Overlay runtime uses innerHTML sink for fetched fragments

**File:** `core-ui/runtime/runtime.js:579-607`

The overlay runtime fetches route HTML and interpolates it via `innerHTML`. While the server renderer escapes `render.Text`, components returning `render.Raw` or untrusted HTML become scriptable through this path.

**Impact:** XSS risk if any component renders untrusted content via `render.Raw`.

**Fix:** Sanitize fetched fragments before innerHTML injection, or use DOMParser + selective node adoption. Document that `render.Raw` is for trusted content only.

---

### Finding 22 — S3 storage doesn't validate keys

**Files:** `battery/storage/s3.go:74-106`, `battery/storage/local.go:52-75`

Local storage validates keys for traversal and empty values. S3 storage only checks for empty key on `Save`. `Get`, `Delete`, `Exists`, and presigned URL helpers accept any key including `../` sequences or empty strings.

**Impact:** Inconsistent namespace safety across storage backends. S3 keys can be manipulated to access unintended paths.

**Fix:** Add a shared `ValidateKey(key string) error` function used by all backends.

---

### Finding 23 — Redis queue enqueue doesn't apply defaults

**File:** `battery/queue/redis.go:49-55`

Unlike `MemoryQueue.Enqueue` which generates IDs, timestamps, and default max attempts, `RedisQueue.Enqueue` marshals the job as-is. Jobs without an ID are stored under an empty hash field when dequeued, and retry metadata is missing.

**Fix:** Apply the same default-filling logic as `MemoryQueue.Enqueue`.

---

## P3 Findings — Polish / Tech Debt

### Finding 24 — Memory queue close can race with enqueue

**File:** `battery/queue/memory.go:80-103`, `battery/queue/memory.go:189-200`

`Enqueue` checks `q.closed` under `RLock`, releases it, then sends to `jobChan`. `Close` can close `jobChan` between the check and the send, causing a panic on send to closed channel.

**Fix:** Send to `jobChan` while still holding the lock, or use a `sync.Once` for the close.

---

### Finding 25 — Static DirListing option is declared but not implemented

**File:** `core/static/static.go:40-42`, `core/static/static.go:118-126`

`Config.DirListing` is documented as enabling directory listing, but when `true` the code still returns false, eventually producing 404.

**Fix:** Implement or remove the option.

---

### Finding 26 — Generated entity config uses incorrect Max field type

**File:** `cmd/gofastr/init.go:145-154`

The generated entity config uses `ptrFloat(200)` for `schema.Field.Max`, but `Max` is `*float64`. The helper `ptrFloat` is correct, but the naming is misleading — it's a max character count, not a float. Consider using a `ptrInt` helper or documenting why `Max` is `*float64` (to support fractional constraints like `step`).

---

### Finding 27 — CRUD handlers use hardcoded `$1` placeholder style

**File:** `framework/crud.go` throughout

All CRUD handlers use PostgreSQL-style `$1` placeholders in `Where` clauses. This breaks with SQLite (uses `?`) and MySQL (uses `?`). Combined with Finding 2, the framework currently only works correctly with PostgreSQL.

**Fix:** Make the query builder dialect-aware, or document that only PostgreSQL is supported in v1.

---

## Cross-Cutting Test Gaps

| Gap | Risk |
|-----|------|
| Generated project compilation not tested | Finding 1 stays broken without CI catching it |
| CRUD tests don't cover tenant isolation | Finding 3 stays broken |
| CRUD tests don't cover soft-delete filtering | Finding 4 stays broken |
| CRUD tests don't verify read-only/hidden field rejection | Finding 9 stays broken |
| Hook lifecycle not tested end-to-end | Finding 10 stays broken |
| OpenAPI conformance tests don't round-trip documented params | Finding 11 stays broken |
| Race-enabled tests only run on middleware/island/devserver | Finding 6 (concurrent routing) not caught |
| No concurrent rendering tests for core-ui | Finding 6 stays undetected |
| Static file serving not tested after traversal hardening | Finding 16 stays broken |
| SSE unsubscribe/update race not tested | Finding 18 stays broken |
| No backend contract test shared between MemoryQueue and RedisQueue | Findings 8, 22, 23 stay inconsistent |
| No dialect-specific migration tests | Finding 2 stays broken |

## Verification

```sh
go test ./...
```
Result: **PASS** (all packages)

```sh
go vet ./...
```
Result: **CLEAN** (no issues)

```sh
go test -race ./core/middleware ./core-ui/island ./core-ui/devserver
```
Result: **FAIL** — race in `core/middleware/timeout.go` (concurrent response writer writes)

The full non-race suite passing should **not** be treated as proof that the issues above are safe. Most findings are contract gaps where tests assert helpers or happy paths rather than the production integration path.

## Summary Statistics

| Severity | Count | Category |
|----------|-------|----------|
| P1 | 8 | Data isolation, crashes, broken first-run, race conditions |
| P2 | 15 | Incorrect behavior, missing enforcement, unconnected APIs |
| P3 | 4 | Polish, tech debt, naming |
| **Total** | **27** | |

## Recommended Fix Priority

1. **P1 batch 1** (broken first-run): Findings 1, 2, 27 — fix `cmd/gofastr/init.go` and add dialect support
2. **P1 batch 2** (data isolation): Findings 3, 4 — wire multitenancy and soft-delete into CRUD handlers
3. **P1 batch 3** (runtime safety): Findings 5, 6, 18, 19 — fix timeout writer race, route param mutation, island panic, action deadlock
4. **P1 batch 4** (incomplete features): Findings 7, 8 — fix overlay accessibility, rewrite Redis queue
5. **P2 sweep**: Findings 9–23 — CRUD field guards, hook wiring, OpenAPI consistency, static serving, signal updates, DI errors
