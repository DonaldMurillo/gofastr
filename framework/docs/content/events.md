# Entity events & SSE

GoFastr emits an event for every successful entity write. Events flow
through an in-process bus and are served over Server-Sent Events at
`GET /{table}/_events`.

## Event types

| Constant         | Wire name         | Fires after           |
|------------------|-------------------|-----------------------|
| `EntityCreated`  | `entity.created`  | Successful Create / BatchCreate commit |
| `EntityUpdated`  | `entity.updated`  | Successful Update / BatchUpdate commit |
| `EntityDeleted`  | `entity.deleted`  | Successful Delete / BatchDelete commit |

Events fire **after** the transaction commits. A hook that errors and
rolls back the transaction emits no event.

## SSE endpoint

```bash
curl -N http://localhost:8080/posts/_events
```

```
event: entity.created
data: {"type":"entity.created","data":{"entity":"posts","table":"posts","record":{…}}}

event: entity.updated
data: {"type":"entity.updated","data":{"entity":"posts","table":"posts","record":{…}}}
```

- The connection stays open until the client disconnects.
- Backpressure: each client has a bounded buffer. The default broker
  keeps emitters non-blocking by dropping the oldest queued event when a
  client cannot keep up, so the subscriber retains the latest events.
  This is intentional — SSE is for push notifications, not durable
  delivery. Use a real queue for that.
- Clients that prefer delivery over emitter latency can opt in with
  `?slow=block` or `X-SSE-Slow: block`. In that mode `Publish` waits
  for buffer space for that subscriber. Use it only when a slow client is
  allowed to backpressure the emitter.
- The stream returns `503 Service Unavailable` if the entity has no
  event bus configured (the default `framework.NewApp` wires one).

## Tenant scoping

When the entity is multi-tenant, the SSE stream filters events to the
tenant ID extracted from the request context. Other tenants' events
never reach the subscriber.

## Payload shape

```json
{
  "type": "entity.created",
  "data": {
    "entity": "posts",
    "table":  "posts",
    "record": { "id": "p1", "title": "…", … },
    "tenantId": "t1"
  }
}
```

`tenantId` is only present on multi-tenant entities. `record` is the
post-write entity in its canonical JSON shape (same casing as the
HTTP responses).

## Subscribing from Go

```go
unsubscribe := app.Events().Subscribe(framework.EntityCreated,
    func(ctx context.Context, ev framework.Event) error {
        data := ev.Data.(map[string]any)
        if data["entity"] != "posts" { return nil }
        record := data["record"].(map[string]any)
        return notifySlack(ctx, record)
    })
defer unsubscribe()
```

`app.Events()` is a method that returns the in-process `*EventBus` —
the same bus the SSE stream subscribes to.

In-process subscribers are not subject to the 32-event SSE buffer;
they run on the emitter's goroutine via `EmitAsync`, but a slow
handler will not block other subscribers.

## When NOT to use SSE

SSE is **push only**. Responses to user actions (clicks, form submits,
filter changes) must come back over the request that triggered them —
never via SSE. The framework's island runtime enforces this rule on
the UI side; the same rule applies to your own clients.

## See also

- [Live dashboards](live-dashboards.md) composes this push lane into a realistic ops dashboard, with the delivery-semantics decision table.
- [UI capability map](ui-capability-map.md) distinguishes realtime UI invalidation from durable workflow delivery.
- [Presence](presence.md) uses the same push lane for self-healing rosters.
- [Horizontal scaling](scaling.md) covers fanout and replica topology.
- [Benchmarks](benchmarks.md) documents SSE delivery/drop measurements.
## Common mistakes

- **Subscribing to SSE for confirmations.** SSE is fire-and-forget.
  If your client needs to know "did my POST succeed?", read the POST
  response. SSE is for the broadcast to *other* clients.
- **Counting on every event arriving.** Backpressure may drop events.
  Use SSE for "something changed, refetch" — not for state machine
  transitions.
- **Forgetting `Cache-Control` on a proxy.** Some reverse proxies
  buffer responses; set `X-Accel-Buffering: no` on nginx, etc.

## Cross-replica fan-out

By default the event bus is per-process: an event emitted on replica A
never reaches an SSE subscriber connected to replica B. Attaching a
fanout bridges the real-time lane across replicas:

```go
import "github.com/DonaldMurillo/gofastr/framework/fanout"

pgf, err := fanout.NewPostgres(dsn, db) // Postgres LISTEN/NOTIFY
if err != nil { log.Fatal(err) }
defer pgf.Close()

app := framework.NewApp(
    framework.WithDB(db),
    framework.WithFanout(pgf),
)
```

`WithFanout` bridges the app's event bus (every emit is mirrored to the
other replicas and re-emitted on their local buses) and wires any
mounted UI host's island manager, so `/entity/_events` SSE streams and
island push both work regardless of which replica holds the connection.
Backends: `framework/fanout.NewPostgres` (uses the DB you already have;
payloads over the NOTIFY size limit spill to a small fallback table)
and `core/fanout.NewRedis` (bring-your-own client, mirroring
`cache.RedisClient`). The fanout is caller-owned — close it after the
app shuts down.

**Semantics change under fanout — the bus becomes a broadcast.** Every
`On`/`Subscribe` handler fires on **every** replica for every event.
That is exactly right for UI push (each replica notifies its own
connected clients) and exactly wrong for side effects — a handler that
sends an email would send it N times. Per-event work belongs on the
durable lane (`WithOutboxConsumer`), which delivers to each consumer
once regardless of replica count.

Two rules for handlers under fanout:

- **Derive on the origin only.** A handler that emits a *new* event in
  response to one it received must gate on `event.IsRemote(ctx)` —
  otherwise every replica derives its own copy and subscribers see
  duplicates:

  ```go
  app.Events().On("order.paid", func(ctx context.Context, ev event.Event) error {
      if event.IsRemote(ctx) {
          return nil // the origin replica already derived + broadcast it
      }
      return app.Events().Emit(ctx, receiptRequested(ev))
  })
  ```

- **The transport is trusted input.** Write access to the fanout's
  channel (the Postgres database, the Redis pub/sub) is equivalent to
  emitting arbitrary events on every replica's bus. Payloads are not
  authenticated; don't share the channel with less-trusted systems.

The lane stays lossy under fanout: a message published while a
replica's listener is reconnecting is gone. Nothing about the durable
lane changes.

## Transactional outbox

The plain event bus emits *after* the transaction commits. A crash in
the gap between commit and emit silently drops the event. The
transactional outbox (`framework/outbox`) closes that gap: the event row
is written **inside** the write transaction, and a background relay
delivers each committed row to every declared durable consumer with
at-least-once semantics.

> **Breaking change (per-consumer delivery).** The outbox shipped in
> v0.15.0 with whole-row, all-or-nothing delivery (one failing subscriber
> failed the entire row). It now delivers to each declared consumer
> **independently**. `StartRelay` lost its `bus` argument; durable
> delivery now requires declaring named consumers via
> `framework.WithOutboxConsumer`. **Drain in-flight outbox rows before
> upgrading** — the new relay ignores the old parent `dead`/single-row
> state (the DDL only adds the child `event_outbox_delivery` table; there
> is no automatic backfill of pre-upgrade rows).

### Two delivery lanes

Delivery is split across two disjoint lanes, so neither duplicates the
other:

- **Real-time lane (best-effort, ephemeral).** The live event bus is
  notified post-commit by `EmitEvent`, feeding SSE streams and ephemeral
  `On`/`Subscribe` handlers. This happens whether or not an outbox is
  configured. It is lossy by design — a crash here drops the in-memory
  signal, but the durable lane still guarantees delivery.
- **Durable lane (per-consumer, tracked).** When an outbox is configured,
  the relay delivers each committed row to the consumers declared via
  `WithOutboxConsumer`. The relay does **not** touch the live bus, so
  there is no double delivery.

### Declaring durable consumers

Enable the outbox app-wide, then declare one or more durable consumers.
A consumer is a stable (name, event-type) identity tracked across
restarts/replicas:

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithOutbox(), // opts forwarded to outbox.New
    framework.WithOutboxConsumer("welcome-email", framework.EntityCreated,
        func(ctx context.Context, ev framework.Event) error {
            data := ev.Data.(map[string]any)
            if data["entity"] != "users" { return nil }
            return sendWelcome(ctx, data["record"])
        }),
)
```

`WithOutbox` stages every entity lifecycle event in the same transaction
as the CRUD write; `App.Start` launches the relay and `Shutdown` drains
it. `app.Outbox()` exposes the handle for inspection (`List`,
`ListDeliveries`), replaying dead deliveries (`Replay` /
`ReplayConsumer`), and staging custom events.

### Staging custom events

To stage your own events in the same transaction as a business write,
call `Append` with the transaction:

```go
// inside a CRUD hook or an App.InTx block:
tx, _ := db.BeginTx(ctx, nil)
// …your business write via tx…
app.Outbox().Append(ctx, tx, "order.placed", map[string]any{"id": orderID})
tx.Commit()
app.Outbox().Nudge() // wake the relay immediately; don't wait for the poll interval
```

Standalone use (no App): `outbox.New(db)` + `ob.Consume(name, type,
handler)` + `ob.StartRelay(ctx)`.

### Semantics

- **At-least-once, per consumer.** The relay invokes each consumer's
  handler, then marks that consumer's delivery dispatched only after it
  returns nil. A crash between the two re-delivers, so consumers **must be
  idempotent**. Deduplicate on **`(consumer, Event.ID)`**, not `Event.ID`
  alone: `Event.ID` is the outbox row id and is the *same* for every
  consumer's delivery of that row, so a dedup store keyed on `Event.ID`
  alone that is shared across consumers would let one consumer's success
  suppress another's delivery — breaking sibling isolation.
- **Sibling isolation.** Each (row, consumer) pair has its own delivery
  row, retried and dead-lettered independently. One consumer that errors
  or panics never blocks its siblings or fails the whole row. A parent row
  is marked `dispatched` once it has no `pending` deliveries left (all
  `dispatched`/`dead`/`abandoned`) **and** it is older than the handler
  grace — it may complete with some deliveries dead; `Replay` /
  `ReplayConsumer` resurrects them.
- **Completion is age-gated.** A parent is not marked `dispatched` until it
  is older than `WithHandlerGrace` (default 15m), even once every current
  delivery is terminal. This is what makes a rolling deploy that *adds* a
  consumer safe: the added consumer's delivery row is created by an
  up-to-date replica on its own poll, and a parent completed too early
  would let the relay skip it forever (expand only touches `pending`
  parents). **Delivery to consumers is unaffected and prompt** — only the
  parent's `dispatched` bookkeeping (and retention/GC) lags by the grace.
- **Dead-letter & replay.** A delivery that returns an error **or
  panics** (the relay surfaces a panicking consumer as a delivery error
  rather than swallowing it) increments its `attempts` and schedules an
  exponential backoff. After `MaxAttempts` (default 10) it is marked
  `dead`. `Replay(rowID)` resets all dead/abandoned deliveries of a row;
  `ReplayConsumer(rowID, name)` resets one.
- **Removed consumers are abandoned (time-based, not snapshot-based).** A
  delivery whose consumer has no handler on *any* replica is
  `abandoned` — settled terminal so it can't orphan the parent — but only
  once it is older than the **handler grace** (`WithHandlerGrace`, default
  15m). This is deliberately time-based: a lagging replica in a rolling
  deploy never abandons a *newly-added* consumer's fresh deliveries (the
  up-to-date replica delivers them first), and a genuinely-removed
  consumer's deliveries age out and abandon everywhere. The grace **must
  exceed your rolling-deploy overlap window plus worst-case clock skew**
  between replicas (delivery/parent timestamps are written on one replica
  and compared on another) — keep a floor of a few minutes. Re-adding a
  removed consumer resumes delivery for events staged from the re-add
  forward automatically; events abandoned during the gap are recovered
  with `ReplayConsumer`.
- **Undeclared event types are dropped after the grace.** A staged event
  whose type has no consumer gets no deliveries and is marked `dispatched`
  once older than the handler grace (deliveries are expanded oldest-first,
  so an old parent with none has no consumer anywhere). Events staged more
  than the grace before a type's *first* consumer is added are not
  back-delivered — a retention-style boundary. If **no** consumer is
  declared at all, the relay logs a Warn at start.
- **Retention (optional).** `WithRetention(d)` makes the relay purge
  fully-settled (`dispatched`) parent rows and their deliveries once older
  than `d`. Pending, dead, and abandoned rows are never purged. Unset
  (default) keeps every row forever.
- **Stalled relays are logged.** If the relay can't claim/expand (missing
  table under `WithoutEnsureTable`, a renamed table, or a DB outage) it
  keeps polling rather than crashing, and logs once at Error on onset and
  once at Info on recovery.
- **Data shape.** `Data` is stored as JSON and unmarshalled into a
  `map[string]any` before delivery, so **`Data` must marshal to a JSON
  object** — a struct or map. A scalar/array fails to unmarshal and the
  delivery is retried then marked `dead`; wrap such values in a map.
  Numbers arrive as `float64` (JSON has no separate integer type).
- **Table creation.** `WithOutbox` creates its tables on demand at
  `NewApp` time (framework-owned bookkeeping tables;
  `WithoutAutoMigrate` does not suppress them). If your policy forbids
  unattended DDL, pass `framework.WithOutbox(outbox.WithoutEnsureTable())`
  and create them yourself before the app stages any event. The schema:

  ```sql
  CREATE TABLE event_outbox (
      id              TEXT PRIMARY KEY,
      type            TEXT NOT NULL,
      payload         TEXT,
      status          TEXT NOT NULL DEFAULT 'pending',  -- pending|dispatched
      created_at      TIMESTAMPTZ NOT NULL,             -- DATETIME on SQLite
      dispatched_at   TIMESTAMPTZ
      -- (attempts/last_error/next_attempt_at/claimed_until are vestigial;
      --  per-attempt state lives in event_outbox_delivery below)
  );
  CREATE INDEX event_outbox_status_created_idx ON event_outbox (status, created_at);

  CREATE TABLE event_outbox_delivery (
      row_id          TEXT NOT NULL,                       -- FK event_outbox.id
      consumer        TEXT NOT NULL,                       -- consumer name
      status          TEXT NOT NULL DEFAULT 'pending',     -- pending|dispatched|dead|abandoned
      attempts        INTEGER NOT NULL DEFAULT 0,
      last_error      TEXT,
      created_at      TIMESTAMPTZ NOT NULL,                -- when THIS delivery row was created
      next_attempt_at TIMESTAMPTZ,
      claimed_until   TIMESTAMPTZ,
      dispatched_at   TIMESTAMPTZ,
      PRIMARY KEY (row_id, consumer)
  );
  -- The PK's leading row_id column already serves row_id lookups, so no
  -- separate row_id index is needed.
  CREATE INDEX event_outbox_delivery_claim_idx ON event_outbox_delivery (status, next_attempt_at);
  ```
- **Multi-replica safe.** The claim takes a lease (`claimed_until`) at
  the delivery grain, so a relay that dies mid-batch releases only its
  claimed deliveries after the lease expires and another relay reclaims
  them — no double-processing beyond the at-least-once caveat.
