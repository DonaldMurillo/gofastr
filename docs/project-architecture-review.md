# GoFastr Project Architecture Review

Date: 2026-05-07

## Round 1 - Whole Project Findings

### Finding 1 - [P1] Overlay rendering contract is broken

File: `core-ui/app/screen.go:154-167`

`Screen.Render` now returns raw drawer/sheet/dialog content while tests and direct server rendering expect ARIA/structural wrappers. Runtime fetches may wrap overlays client-side, but direct visits and partial rendering can produce unwrapped, inaccessible overlay markup, and the package test is currently red.

### Finding 2 - [P1] Multitenancy flag does not protect CRUD routes

File: `framework/tenant.go:41-52`

`WithMultiTenant` only sets `Entity.Config.MultiTenant`, but auto CRUD handlers never read tenant context, apply tenant filters, or inject `tenant_id` on writes. Any entity marked multitenant can still list, read, update, or delete across tenants.

### Finding 3 - [P1] Generated init project does not compile

File: `cmd/gofastr/init.go:79-130`

The scaffolded `main.go` calls `context.Background` without importing `context`, sets `CRUD: true` even though `EntityConfig.CRUD` is `*bool`, and writes `go.mod` before running `go mod init`, causing `go mod init` to fail in the generated project.

### Finding 4 - [P2] QueryBuilder Build mutates arguments

File: `core/query/query.go:187-197`

`Build` appends limit and offset values onto `qb.args` every time it is called, so repeated `Build` calls produce different args and placeholder indexes. Builders are easy to accidentally reuse in tests, logging, instrumentation, and pagination helpers.

### Finding 5 - [P2] Cursor pagination can duplicate placeholder numbers

File: `core/query/query.go:112-123`

`Cursor` manually appends its arg but stores nil `whereClause` args, so `Build` does not advance `paramIdx` for that clause. A `Where` added after `Cursor` can be renumbered to the same placeholder as the cursor condition.

### Finding 6 - [P2] HTTP cache middleware loses response metadata

File: `battery/cache/middleware.go:30-55`

Cache hits replay only status and body, not headers such as `Content-Type`. On misses, `X-Cache` is set after the wrapped handler has already written headers, so clients will often never receive `X-Cache: MISS`.

### Finding 7 - [P2] Multiple CORS origins are emitted as an invalid header

File: `core/middleware/cors.go:26-43`

`Access-Control-Allow-Origin` cannot contain a comma-separated list of origins. Browsers require one origin echoed back or `*`, usually with `Vary: Origin` when selecting dynamically.

## Round 2 - API Focus

### Finding 8 - [P1] Migration layer is PostgreSQL-only while the default scaffold is SQLite

File: `core/migrate/runner.go:24-31`, `core/migrate/diff.go:25-56`, `cmd/gofastr/init.go:44-47`

The generated project defaults to SQLite, but the migration runner and schema diff generator emit PostgreSQL-specific SQL: `DEFAULT NOW()`, `BIGSERIAL`, `UUID`, `JSONB`, `DOUBLE PRECISION`, `information_schema`, and `$1` placeholders. A default `gofastr init` app will therefore be steered into APIs that do not match its default database driver.

### Finding 9 - [P2] OpenAPI filter parameters do not match implemented API filters

File: `framework/entity_openapi.go:86-90`, `framework/filter.go:37-47`

The OpenAPI generator documents filters as `filter_<field>` query parameters, but the implemented CRUD parser accepts `<field>`, `<field>_gt`, `<field>_gte`, `<field>_lt`, `<field>_lte`, `<field>_like`, and `<field>_in`. Clients generated from the spec will send filter names the server ignores.

### Finding 10 - [P2] File-field storage ignores request cancellation and deadlines

File: `framework/filefield.go:63-66`, `framework/filefield.go:88-89`

`ProcessFileField` and `DeleteFileField` use `context.Background()` instead of a caller-supplied request context. Upload/save/delete operations can continue after the client disconnects and cannot honor request deadlines or cancellation.

### Finding 11 - [P2] Static-file dev serving can escape staticDir

File: `core-ui/devserver/devserver.go:289-296`

The dev server joins `staticDir` with `r.URL.Path` directly. On Unix, `filepath.Join(base, "/absolute")` discards `base`, and `..` segments are not checked before `os.Stat`/`http.ServeFile`. A request path can escape the intended static directory when `WithStaticDir` is enabled.

## Round 3 - Core UI Focus

### Finding 12 - [P1] Server actions never invoke registered Go handlers

File: `core-ui/devserver/devserver.go:488-529`, `core-ui/component/actions.go:3-22`

`ActionDef` stores server-side handlers, but `handleServerAction` only decodes the action name and returns a canned `"Server action processed"` response. It never looks up the component action registry or invokes `ActionDef.Handler`, so actions marked for server execution do not perform application work.

### Finding 13 - [P1] Dynamic route params are stored on shared Screen instances

File: `core-ui/app/router.go:73-91`, `core-ui/app/app.go:126-135`

The app router resolves dynamic routes by mutating `dr.screen.routeParams` on the registered screen. Because that screen and its component instance are shared across requests, concurrent requests to `/products/a` and `/products/b` can overwrite each other's params before `SetParams` and render run.

### Finding 14 - [P2] Action extraction uses package-global mutable state

File: `core-ui/component/actions.go:79-125`

`ExtractActions` relies on the package-level `currentRegistry` while calling user component `Actions()`. Concurrent calls can interleave and register actions into the wrong registry because there is no mutex or context-local registry.

### Finding 15 - [P2] Overlay runtime builds structural HTML with innerHTML from fetched fragments

File: `core-ui/runtime/runtime.js:579-607`

`openOverlay` fetches arbitrary route HTML and interpolates it into template strings assigned to `innerHTML`. Server-rendered HTML may be safe today when built through `render.Text`, but any component returning `render.Raw` or user-provided HTML becomes scriptable in the overlay path. This makes overlays a higher-risk injection sink than normal server rendering.

### Finding 16 - [P2] DI injection silently skips failures and may render nil dependencies

File: `core-ui/app/app.go:133-135`, `core-ui/app/di.go:103-140`

`RenderPage` and `RenderPartial` call `a.Inject(screen.Component)` and ignore its error. The container also silently skips missing providers. Screens can render with nil services, making missing dependencies surface as render panics or incorrect UI instead of route-level errors.
