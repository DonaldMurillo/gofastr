# Horizontal scaling

A single GoFastr replica works out of the box with zero external services because defaults live in process memory. The moment you run **two or more replicas**, any in-memory default must move to a shared store (Postgres/Redis/S3) or run on one designated worker process.

## Process-local defaults → replica-safe fixes

| Subsystem | Single-node default | Replica-safe replacement |
| --- | --- | --- |
| Auth sessions | `MemorySessionStore` | `auth.NewEntitySessionStore(db, "sessions")` |
| 2FA enrollment | `MemoryTwoFAStore` | `auth.NewEntityTwoFAStore(db, "auth_twofa", ...)` |
| Magic-link / reset / verify tokens | In-memory token store | `auth.NewSQLMagicLinkTokenStore(db)` |
| Rate limits | In-process `RateLimiter` | `auth.NewSQLRateLimitStore(db, "auth_rate_limits")` |
| Cron scheduler | Ticks on every replica | `GOFASTR_ROLE=worker`, `WithLeaderElection`, or `DBQueue` |
| Job queue | `MemoryQueue` | `queue.DBQueue` (`FOR UPDATE SKIP LOCKED`) |
| SSE / island push / presence | In-process `EventBus` | `framework.WithFanout(fanout.NewPostgres(dsn, db))` or `core/fanout.NewRedis`, OR poll with `data-cui-poll` |
| Cache | `MemoryCache` | `cache.NewRedisCache(client)` (or accept per-replica cache for derived data) |
| File uploads | `storage.NewLocalStorage` | `storage.NewS3Storage(...)` or shared volume |
| Runtime RBAC grants | In-memory `RolePolicy` | `framework.WithGrantStore(store)` + `framework.WithFanout(...)` |
| Idempotency keys | `NewMemoryIdempotencyStore` | `middleware.NewSQLIdempotencyStore(db)` |

In production (`DevMode: false`), `battery/auth` refuses to boot with in-memory session/2FA/token stores unless you explicitly acknowledge a single-node deploy with `AuthConfig.AllowInMemoryStores: true`.

Already replica-safe out of the box: migrations (advisory lock), startup seeds (separate advisory lock + `_gofastr_seeded` ledger — keep Postgres `MaxOpenConns > 1`), `queue.DBQueue`, `webhook.LeasedStore`, and stateless CRUD/API traffic.

## Process roles (`GOFASTR_ROLE`)

Run the exact same binary in dedicated roles:

```bash
GOFASTR_ROLE=serve  ./myapp   # full HTTP router; no cron, queue workers, or outbox relay
GOFASTR_ROLE=worker ./myapp   # cron, queue workers, outbox relay; /healthz + /readyz only
GOFASTR_ROLE=agent  ./myapp   # /mcp + /a2a + /healthz + /readyz only
./myapp                       # all (default)
```

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
-->
```go
app.Start(":8080") // role from GOFASTR_ROLE or framework.WithRole(...)
```

**Recommended starter topology:** 2+ `GOFASTR_ROLE=serve` web replicas + 1 `GOFASTR_ROLE=worker` process sharing Postgres, with `GOFASTR_SECRET` set identically across all replicas so signed uihost session tokens verify on every node without sticky sessions (`WithFanout` fails boot if `GOFASTR_SECRET` / `WithSecret` is unset).

## SSE across replicas

1. **Prefer polling (`data-cui-poll`)** for passive freshness (dashboards, badges) — any replica answers `GET` with zero fanout needed (see [Reactivity](reactivity.md)).
2. **Attach `framework.WithFanout`** (`fanout.NewPostgres` using Postgres `LISTEN/NOTIFY`, or `core/fanout.NewRedis`) when you genuinely need live push across replicas (presence, collaborative views, `_events`). Gate derived emits with `event.IsRemote(ctx)` so replicas don't echo loops.

## Common mistakes

- **Leaving `AllowInMemoryStores: true` on after scaling to 2 replicas:** users randomly log out across requests. Remove it before scaling out.
- **Forgetting `GOFASTR_SECRET` across replicas:** ephemeral boot secrets invalidate uihost sessions on other replicas.
- **Running `framework.Scheduler` on every replica without leader election or a worker role:** executes every cron job N times.
- **Using SSE push without `WithFanout` across replicas:** half your live updates silently miss browsers connected to peer nodes.
- **Keeping `LocalStorage` uploads on local container disks:** uploads on replica A return `404` on replica B.
