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

## Common mistakes

- **Subscribing to SSE for confirmations.** SSE is fire-and-forget.
  If your client needs to know "did my POST succeed?", read the POST
  response. SSE is for the broadcast to *other* clients.
- **Counting on every event arriving.** Backpressure may drop events.
  Use SSE for "something changed, refetch" — not for state machine
  transitions.
- **Forgetting `Cache-Control` on a proxy.** Some reverse proxies
  buffer responses; set `X-Accel-Buffering: no` on nginx, etc.


## Transactional outbox

The plain event bus emits *after* the transaction commits. A crash in
the gap between commit and emit silently drops the event. The
transactional outbox (`framework/outbox`) closes that gap: the event row
is written **inside** the write transaction, and a background relay
publishes committed rows to the bus with at-least-once semantics.

Enable it app-wide with one option:

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithOutbox(), // opts forwarded to outbox.New, e.g. outbox.WithPollInterval(…)
)
```

With `WithOutbox`, every entity lifecycle event
(`entity.created`/`updated`/`deleted`) is staged in the same transaction
as the CRUD write and delivered by the relay — which `App.Start` launches
and `Shutdown` drains. Subscribers on `app.Events()` see the same payload
shape as before, now with a durable `Event.ID`, and a rolled-back write
emits nothing. The live-bus emit is suppressed (the relay is the sole
deliverer), so nothing arrives twice on the happy path. `app.Outbox()`
exposes the handle for inspection (`List`), replaying dead rows
(`Replay`), and staging custom events.

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

Standalone use (no App) works the same way: `outbox.New(db)` +
`StartRelay(ctx, bus)`.

- **At-least-once.** The relay calls the synchronous `Emit`, then marks
  the row dispatched. A crash between the two re-delivers the row on
  restart. Consumers **must be idempotent** and deduplicate by
  `Event.ID` — which the relay stamps from the outbox row's primary key.
  Events emitted directly via `Emit`/`EmitAsync` carry an empty `ID` and
  have no durable identity.
- **Delivery is per-row, all-or-nothing across co-subscribers.** A row is
  dispatched only if *every* subscriber for its event type succeeds; the
  first one that errors or panics fails the whole row, so the others don't
  run on that attempt and any that already succeeded run **again** on
  retry (hence the idempotency requirement). A chronically-failing
  subscriber therefore blocks its co-subscribers for that event until it
  stops failing — and once the row dead-letters, co-subscribers never see
  it unless you `Replay`. Keep independent side effects in separate event
  types, or make every subscriber idempotent and resilient.
- **Stalled claims are logged.** If the relay can't claim a batch (missing
  table under `WithoutEnsureTable`, a renamed table, or a DB outage) it
  keeps polling rather than crashing, and logs once at Error on the
  failure onset and once at Info on recovery — so a stalled relay is
  visible without flooding the log every poll.
- **Data shape.** The relay stores `Data` as JSON and unmarshals it back
  into a `map[string]any` before emitting, so **`Data` must marshal to a
  JSON object** — a struct or map. A scalar or array (`Append(ctx, tx,
  "t", "hi")` or `[]int{1,2}`) fails to unmarshal into the map and the
  row is retried then marked `dead`; wrap such values in a map. JSON has
  no separate integer type, so numbers arrive as `float64` (e.g. a count
  of `3` becomes `3.0`). Marshal structs to a shape that tolerates this,
  or pass pre-encoded JSON.
- **Backoff.** A failing handler — one whose delivery returns an error
  **or panics** (the relay surfaces a panicking subscriber as a delivery
  error rather than swallowing it) — increments the row's `Attempts` and
  schedules an exponential backoff via `next_attempt_at`. After
  `MaxAttempts` (default 10) the row is marked `dead`; `outbox.Replay`
  resets a dead row to pending. A row is never marked `dispatched` unless
  delivery actually succeeded.
- **Table creation.** `WithOutbox` creates its `event_outbox` table on
  demand at `NewApp` time (a framework-owned bookkeeping table, like the
  seed ledger — `WithoutAutoMigrate` does not suppress it). If your policy
  forbids unattended DDL, pass `framework.WithOutbox(outbox.WithoutEnsureTable())`
  and create the table yourself through your migration pipeline before the
  app stages any event; the first `Append` fails fast if it is missing. The
  schema is:

  ```sql
  CREATE TABLE event_outbox (
      id              TEXT PRIMARY KEY,
      type            TEXT NOT NULL,
      payload         TEXT,
      status          TEXT NOT NULL DEFAULT 'pending',
      attempts        INTEGER NOT NULL DEFAULT 0,
      last_error      TEXT,
      created_at      TIMESTAMPTZ NOT NULL,  -- DATETIME on SQLite
      dispatched_at   TIMESTAMPTZ,
      next_attempt_at TIMESTAMPTZ,
      claimed_until   TIMESTAMPTZ
  );
  CREATE INDEX event_outbox_status_created_idx ON event_outbox (status, created_at);
  ```
- **Multi-replica safe.** The claim takes a lease (`claimed_until`), so
  a relay that dies mid-batch releases its rows after the lease expires
  and another relay reclaims them — no double-processing beyond the
  at-least-once caveat.
