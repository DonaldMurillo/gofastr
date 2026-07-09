# Horizontal scaling

One replica of a GoFastr app is self-contained: sessions, rate limits,
cron, queues, and live UI updates all work out of the box because their
default state lives **in the process**. The moment you run a second
replica behind a load balancer, every one of those defaults needs a
shared backend or a deliberate single-runner strategy. This page is the
complete list — what breaks, why, and the replica-safe alternative.

## The one-line summary

The database is the only state the replicas share. Anything that
defaults to process memory must either move into the DB (or another
shared store) or run on exactly one replica.

## What is process-local by default

| Subsystem | Default | Two-replica symptom | Replica-safe fix |
|---|---|---|---|
| Auth sessions | in-memory `MemorySessionStore` | login on A, logged-out on B; all sessions lost on restart | `auth.NewEntitySessionStore(db, "sessions")` |
| 2FA enrollment | in-memory `MemoryTwoFAStore` | worse than scaling: a **restart** silently reverts 2FA accounts to password-only | set `TwoFAConfig.Store` to a durable `TwoFAStore` |
| Login rate limits | in-process `RateLimiter` | attacker gets N attempts **per replica**; blocks don't propagate | no shipped shared store — size `MaxAttempts` for `attempts × replicas`, or rate-limit at the ingress |
| `framework/cron` scheduler | ticks in every process | every replica fires every job | run the scheduler on one replica, or use `battery/queue`'s `DBQueue` (see below) |
| `battery/queue` in-memory queue + Scheduler | per-process | duplicate jobs, lost jobs on restart | `queue.DBQueue` — `FOR UPDATE SKIP LOCKED` makes competing workers safe |
| Live events / SSE / island push | in-process `EventBus` + `island.Manager` | an event emitted on A never reaches a browser connected to B | `framework.WithFanout(fanout.NewPostgres(dsn, db))` — see "SSE across replicas" below |
| `battery/cache` memory backend | per-process | stale reads after another replica writes | `cache.NewRedisCache(client)`, or accept per-replica caching for derived-only data |

Auth warns about the first two at boot: production mode (`DevMode:
false`) on the default in-memory stores logs a WARN unless you set
`AuthConfig.AllowInMemoryStores: true` to acknowledge a deliberate
single-node deployment.

## What is already replica-safe

- **Migrations** — auto-migrate takes a Postgres advisory lock, so N
  replicas booting simultaneously run the migration once.
- **`queue.DBQueue`** — claims jobs with `FOR UPDATE SKIP LOCKED`;
  competing workers on every replica are the *intended* topology.
- **Webhook delivery (`LeasedStore`)** — leases deliveries so two
  replicas don't double-send.
- **Plain CRUD/API traffic** — stateless per request; scale freely.

## Recommended shapes

**Two web replicas + one worker.** Point sessions at
`EntitySessionStore`, disable in-process cron on the web replicas, and
run one worker process (same binary, a flag you define) that owns the
cron scheduler and the `DBQueue` workers. This is the smallest shape
with no shared-state caveats.

**Everything everywhere, DB-backed.** All replicas run `DBQueue`
workers (safe by design). For *scheduled* work, have the schedule
enqueue a `DBQueue` job instead of doing the work inline — then it
doesn't matter that every replica's scheduler fires, as long as the
job is idempotent or keyed for dedup. Neither scheduler ships a
distributed lock; the queue's claim semantics are the sanctioned
coordination point.

**Single node, on purpose.** Vertical scaling is underrated. Set
`AuthConfig.AllowInMemoryStores: true` to silence the boot warning and
skip this whole page until you add a replica. A restart still logs
everyone out and wipes in-memory 2FA enrollment — use the entity-backed
stores anyway if either matters.

## SSE across replicas

Server-pushed events flow over an SSE connection to the replica the
browser happened to reach. A write handled by a different replica emits
on *its* bus and pushes to *its* island manager, not the one holding
the connection. Options, in order of preference:

1. **Shared fan-out** — `framework.WithFanout` bridges the real-time
   lane across replicas. `framework/fanout.NewPostgres(dsn, db)` uses
   Postgres LISTEN/NOTIFY (no new infrastructure); `core/fanout.NewRedis`
   adapts a Redis client you bring. Entity `_events` SSE streams and
   island push then work from any replica. Read the "Cross-replica
   fan-out" section of the events doc first — with a fanout attached,
   `On`/`Subscribe` handlers fire on **every** replica, so side-effect
   work must move to outbox consumers and derived emits must gate on
   `event.IsRemote(ctx)`.
2. **Sticky sessions** (cookie-based affinity at the LB) — the browser
   and its mutations land on the same replica; push works unchanged.
   Still the recommendation for stateful uihost *widget* apps: the
   fanout fixes delivery-where-connected, but island objects and signal
   state remain per-replica, so an RPC landing on a replica without the
   widget's state can't re-render it.
3. **Design around it** — SSE push is for background events; if all
   your islands re-render from user actions (RPC round-trips), nothing
   is lost without push.

## Checklist before adding the second replica

- [ ] Sessions on `EntitySessionStore` (or another shared `SessionStore`).
- [ ] 2FA store durable (if the 2FA plugin is enabled).
- [ ] Cron scheduler runs on exactly one process, or jobs moved to `DBQueue`.
- [ ] Queue is `DBQueue`, not the in-memory variant.
- [ ] Login rate-limit budget sized for N replicas (or enforced at the ingress).
- [ ] SSE push crosses replicas: `WithFanout` attached (and side-effect
      handlers moved to outbox consumers), or sticky sessions configured.
- [ ] Cache backend shared (Redis) if cached data must be coherent across replicas.
- [ ] `AuthConfig.AllowInMemoryStores` **removed** — the boot warning is
      your regression test for the first two items.

## Common mistakes

- **Scaling to two replicas with default sessions.** Users get randomly
  logged out depending on which replica the LB picks. The boot WARN
  about the in-memory session store is telling you this will happen —
  don't silence it with `AllowInMemoryStores` while running N > 1.
- **Setting `AllowInMemoryStores: true` "to clean up the logs"** and
  then scaling later. The flag is an assertion about your topology, not
  a log filter; remove it the moment a second replica is on the table.
- **Running `framework/cron` on every replica** because each job
  "checks if it already ran" against the DB. That check is a race, not
  a lock. Move the work to `DBQueue` or pin the scheduler to one process.
- **Relying on SSE push without sticky sessions.** Everything appears
  to work in staging (one replica) and half of live updates silently
  vanish in production.
- **Treating the login rate limit as a security boundary at N replicas.**
  The budget multiplies by replica count; enforce hard limits at the
  ingress if the number matters.

See [Deployment](deploy.md) for the single-replica production checklist
and [Job queue](queue.md) for `DBQueue` worker sizing.
