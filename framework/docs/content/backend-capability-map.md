# Backend capability map

Start here when you know the *job* you need done—"how do I add auth", "how do I scope rows per user", "how do I run background jobs"—and want the exact package, verification command, and doc without reading five guides first.

For the UI side, see [ui-capability-map.md](ui-capability-map.md).

## The app spine

Every `framework.App` starts with `framework.NewApp(...)`. Register custom handlers on `app.Router()`, mount opt-in `battery/` packages, and use `app.Entity(...)` whenever auto-wired CRUD, OpenAPI, and MCP tools save you time:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app := framework.NewApp(
	framework.WithDB(db),            // any *sql.DB, sqlite3 or postgres
	framework.WithAPIPrefix("/api"), // optional; moves entity routes + OpenAPI paths under /api
)
app.Entity("tickets", entity.EntityConfig{
	Table: "tickets",
	Scope: &framework.ScopeConfig{OwnerField: "user_id"}, // per-user scoping
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
	},
})
app.Start(":8080")
```

## Quick sanity checks

```bash
gofastr verify                                        # static + contract checks
curl localhost:8080/healthz                           # health probe
curl localhost:8080/openapi.json | jq '.paths | keys' # mounted API paths
```

Note: `/openapi.json` and `/api/llm.md` answer `401` by default (pass `framework.WithPublicOpenAPI()` to open them, or sign in—authenticated callers see only entities they can read). `/metrics` mounts only when `framework.WithMetrics()` is set. The startup banner prints every mounted route and its auth gate.

## Jobs

| Job to be done | Compose | Prove it | Docs |
|---|---|---|---|
| Custom HTTP / typed JSON routes | `app.Router()` (`core/router`, `core/handler`) | `curl localhost:8080/...` | [Custom endpoints](endpoints.md) |
| CRUD over a table | `framework.App.Entity` with `entity.EntityConfig` | `curl localhost:8080/api/tickets` | [Entity declarations](entity-declarations.md) |
| Scope rows to the signed-in user | `EntityConfig.Scope.OwnerField` (not a manual `WHERE`) | `gofastr verify data` flags unscoped user tables | [Entity declarations](entity-declarations.md), [Access control](access-control.md) |
| Login, signup, sessions, resets | `battery/auth`: `auth.New`, `auth.SessionMiddleware` | `POST /auth/login` → `Set-Cookie` | [Auth](auth.md) |
| Roles and permissions | `access.NewRolePolicy`, `access.Middleware`, `access.Wildcard` | `gofastr verify permissions` | [Access control](access-control.md) |
| Signed sessions across replicas | `framework.WithSecret` / `GOFASTR_SECRET`, `framework.WithSecretRotation` | restart process; existing cookie still works | [Scaling](scaling.md), [Auth](auth.md) |
| Schema changes | auto-migration on `Start`, or `gofastr migrate` for SQL files | `gofastr migrate status` | [Migrations](migrations.md) |
| Typed REST clients | `gofastr generate sdk` | generated Go/TS client compiles against spec | [SDK](sdk.md) |
| Expose app to agents | `framework.WithMCP`, `framework.WithMCPTools`, `framework.WithMCPIntrospection` | `POST /mcp` (`tools/list`) or `GET /api/llm.md` | [Agent-ready](agent-ready.md) |
| Background jobs & retries | `battery/queue` | queue depth on `/metrics` | [Queue](queue.md) |
| Scheduled jobs | `framework.Scheduler` (cron) | run counter advances on `/metrics` | [Cron](cron.md) |
| React to writes | `framework/hook` (in-tx), `battery/webhook` (outbound) | hook error rolls back the transaction | [Hooks and transactions](hooks-and-transactions.md), [Webhooks](webhooks.md) |
| Send email | `battery/email` | log sender prints sends in dev; swap SMTP for prod | [Email](email.md) |
| File & image uploads | `battery/storage`, `framework/imagefield` | `curl -F file=@x.png localhost:8080/api/posts` | [Storage](storage.md), [Uploads](uploads.md) |
| Full-text & vector search | `battery/search`, `battery/semantic` | `curl 'localhost:8080/api/posts?q=term'` | [Search](search.md), [Semantic search](semantic-search.md) |
| Site-wide page search | `battery/sitesearch` | `curl 'localhost:8080/__gofastr/search/query?q=term'` | [Site search](site-search.md) |
| Multi-tenancy | `framework/tenant` | `gofastr verify data` | [Multi-tenant](multi-tenant.md) |
| Soft delete | `framework/softdelete` | `?trashed=true` lists deleted rows | [Entity declarations](entity-declarations.md) |
| Rate limiting | `framework/ratelimit` | exceed limit → `429` | [Rate limit](rate-limit.md) |
| Admin back-office | `battery/admin` | visit `/admin` | [Admin](admin.md) |
| First-boot onboarding | `battery/setup` | boot empty DB → setup token in logs | [First run](first-run.md) |
| Logs, metrics, audit trail | `battery/log`, `framework.WithMetrics`, `framework.App.WithAuditLog` | `curl localhost:8080/metrics` | [Observability](observability.md), [Log](log.md), [Audit log](audit-log.md) |
| Health & readiness probes | wired by `Start` | `curl localhost:8080/healthz` | [Health checks](health-checks.md) |
| Multiple replicas | `framework.WithFanout` for SSE/presence | two processes sharing one DB + secret | [Scaling](scaling.md) |
| Integration tests | `framework.TestHarness`, `framework.AutoMigrate`, `framework/factory` | `go test ./...` | [Testkit](testkit.md), [Factories](factories.md) |

## What not to hand-write

Hand-writing these is the fastest way to introduce bugs the framework already prevents:

- Per-user `WHERE user_id = ?` filters → use `Scope.OwnerField` (covers REST, batch, MCP, and includes together)
- Query string pagination/sort/filter parsers → `framework/filter`, `framework/pagination`, `framework/dsl`
- Password hashing & session cookies → `battery/auth`
- Manual OpenAPI YAML → generated from entity declarations
- Ad-hoc goroutine retry loops → `battery/queue`

## Common mistakes

- **Reading deep topic docs before checking this map.** Use the table to pick the right package first, then open its guide.
- **Hand-writing per-user SQL filters on CRUD entities.** A manual `WHERE` only protects one handler; `Scope.OwnerField` enforces isolation across REST, batch, includes, and MCP (`gofastr verify data` catches the gap).
- **Forgetting `WithAPIPrefix` when checking OpenAPI paths.** OpenAPI paths include your configured prefix (`/api/tickets`, not `/tickets`).
- **Assuming `/openapi.json` or `/metrics` is broken when it returns `401` or `404`.** Check the startup banner: OpenAPI is auth-gated by default and `/metrics` requires `framework.WithMetrics()`.
