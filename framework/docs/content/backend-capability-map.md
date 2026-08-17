# Backend capability map

Start here when you know the *job* and need the primitive: "how do I scope
rows to a user", "where does auth come from", "how do I prove the API works".
This page is a routing table, not a tutorial — one row per job, the symbols to
compose, and a command that proves it. Read the linked topic only once a row
tells you which one you need.

The UI equivalent is [ui-capability-map.md](ui-capability-map.md).

## The 20-line app

Everything below assumes this shape. It is the whole spine.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app := framework.NewApp(
    framework.WithDB(db),              // any *sql.DB — sqlite3 or postgres
    framework.WithAPIPrefix("/api"),   // optional; routes AND openapi paths move
)
app.Entity("tickets", entity.EntityConfig{
    Table:      "tickets",
    Scope:      &framework.ScopeConfig{OwnerField: "user_id"}, // per-user scoping — see the table
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
    },
})
app.Start(":8080")
```

`framework.App.Entity` is the declaration. From that one call you get the table, REST
CRUD, `_batch`, `_events`, OpenAPI, and MCP tools. You do not wire them
separately, and there is no route file to keep in sync.

## Verify anything in three commands

These work against any GoFastr app and are the fastest way to answer "did
that actually do what I think".

```bash
gofastr verify                       # contract check: routing, permissions, security, data
curl localhost:8080/healthz          # is it up
curl localhost:8080/openapi.json | jq '.paths | keys'   # every mounted API path
```

Every `/api/…` path below assumes `framework.WithAPIPrefix("/api")` from the
snippet above. It is **optional**: without it, entities mount at the bare
`/tickets`, and every command here works with the `/api` dropped. Reach for
`curl localhost:8080/openapi.json | jq '.paths | keys'` when you are unsure —
under either setting, a documented path is the path you request.

**`/openapi.json` and `/api/llm.md` answer `401` by default** — the schema is
a disclosure, so both are behind the auth gate until you pass
`framework.WithPublicOpenAPI()`. That is not a bug to debug; the startup
banner says so next to each URL. `/metrics` is likewise **not mounted** until
`framework.WithMetrics()`, and returns `404` otherwise.

Read the startup banner before adding a debug print. It lists every mounted
route, marks which need auth, and names the option that ungates them.

## Jobs

| Job to be done | Compose | Prove it | Docs |
|---|---|---|---|
| CRUD over a table | `framework.App.Entity` with `entity.EntityConfig` | `curl localhost:8080/api/tickets` | [Entity declarations](entity-declarations.md) |
| Scope rows to the signed-in user | `EntityConfig.Scope.OwnerField` — **not** a hand-written filter | `gofastr verify data` fails when an entity with user data lacks it | [Entity declarations](entity-declarations.md), [Access control](access-control.md) |
| Login, signup, sessions, password reset | `battery/auth`: `auth.New`, `auth.SessionMiddleware` | `curl -i -X POST localhost:8080/auth/login -d '{...}'` → `Set-Cookie` | [Auth](auth.md) |
| Roles and permissions | `access.NewRolePolicy`, `access.Middleware`, `access.Wildcard` | `gofastr verify permissions` | [Access control](access-control.md) |
| Signed sessions across replicas | `framework.WithSecret` / `GOFASTR_SECRET`, `framework.WithSecretRotation` | restart the process; an existing cookie still authenticates | [Scaling](scaling.md), [Auth](auth.md) |
| Schema changes | auto-migration on `Start`, or `gofastr migrate` for explicit files | `gofastr migrate status` | [Migrations](migrations.md) |
| A REST client, typed | `gofastr generate sdk` | the generated client compiles against the live spec | [SDK](sdk.md) |
| Let an agent drive the app | entity MCP tools (automatic), `framework.WithMCPIntrospection` | `curl localhost:8080/api/llm.md` (401 without `framework.WithPublicOpenAPI`) | [Agent-ready](agent-ready.md) |
| Background work, retries, dead-letter | `battery/queue` | queue depth on `/metrics` (needs `framework.WithMetrics`) | [Queue](queue.md) |
| Scheduled work | `framework.Scheduler` (cron) | with `framework.WithMetrics`, `/metrics` shows the run counter advance | [Cron](cron.md) |
| React to a write | `battery/webhook` for outbound, `framework/hook` for in-transaction | a hook that returns an error rolls the write back | [Webhooks](webhooks.md), [Hooks and transactions](hooks-and-transactions.md) |
| Send mail | `battery/email` | the SMTP backend logs the send; swap in a fake for tests | [Email](email.md) |
| File and image uploads | `battery/storage`, `framework/imagefield` | `curl -F file=@x.png localhost:8080/api/posts` | [Storage](storage.md), [Uploads](uploads.md) |
| Full-text and vector search | `battery/search`, `battery/semantic` | `curl 'localhost:8080/api/posts?q=term'` | [Search](search.md), [Semantic search](semantic-search.md) |
| Multi-tenancy | `framework/tenant` — a declaration, not a WHERE clause | `gofastr verify data` | [Multi-tenant](multi-tenant.md) |
| Soft delete | `framework/softdelete` | `?trashed=true` returns the deleted rows | [Entity declarations](entity-declarations.md) |
| Rate limiting | `framework/ratelimit` | replay a request past the limit → `429` | [Rate limit](rate-limit.md) |
| An admin back-office | `battery/admin` | visit `/admin` | [Admin](admin.md) |
| First-boot setup on an empty DB | `battery/setup` | start against an empty database → setup token in the log | [First run](first-run.md) |
| Logs, metrics, panics | `battery/log`, `framework.WithMetrics`, `framework.App.WithAuditLog` | `curl localhost:8080/metrics` after `framework.WithMetrics()` | [Observability](observability.md), [Log](log.md), [Audit log](audit-log.md) |
| Health and readiness probes | wired by `Start` | `curl localhost:8080/healthz` | [Health checks](health-checks.md) |
| Run more than one replica | `framework.WithFanout` for SSE/presence; everything else is stateless | two processes, one database, sessions work on both | [Scaling](scaling.md) |
| Tests with a real DB and real HTTP | `framework.TestHarness`, `framework.AutoMigrate`, `framework/factory` | `go test ./...` | [Testkit](testkit.md), [Factories](factories.md) |

## What not to hand-write

Each of these is a declaration, and writing it by hand is the most common way
an app ends up with a bug the framework would have prevented:

- a `WHERE user_id = ?` filter → `OwnerField`
- a pagination/sort/filter query string parser → `framework/filter`,
  `framework/pagination`, `framework/dsl`
- a password hash and session cookie → `battery/auth`
- an OpenAPI document → generated from the entity declarations
- a retry loop around a background job → `battery/queue`

## Common mistakes

- **Reading topic docs before this page.** They are references, not
  orientation. Land on the row first, then open the one link it points at.
- **Hand-writing a filter for per-user data.** `OwnerField` is enforced on
  every generated surface — REST, batch, MCP, includes. A hand-written `WHERE`
  covers the one handler you remembered. `gofastr verify data` flags the gap.
- **Trusting `paths` in a stale mental model of the spec.** Under
  `framework.WithAPIPrefix` the OpenAPI path keys carry the prefix, so a
  documented path is the path you request. See
  [API versioning](api-versioning.md).
- **Adding an option before `framework.WithConfig`.** `WithConfig` replaces
  the whole `AppConfig` struct rather than merging into it, so any granular
  option placed *before* it is zeroed. Put granular options *after*
  `WithConfig` — later options win. Two guards make the mistake hard to keep:
  `gofastr init` scaffolds `WithConfig` as the *first* option (so pasting
  `framework.WithPublicOpenAPI()` anywhere below it works), and `NewApp` logs
  a warning naming each field an earlier option set that `WithConfig` zeroed
  and no later option restored. Replace semantics are deliberate: a merge
  could not tell an explicit zero from an unset field, so `WithConfig` could
  never turn a boolean back off.
- **Adding a route for in-page state.** Sorting and paginating are islands,
  not routes. That is a UI question — see
  [ui-capability-map.md](ui-capability-map.md).
