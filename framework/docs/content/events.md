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
- The initial `: subscribed <entity>` comment is flushed after the
  entity's event listeners are installed. A write published after that
  acknowledgement can enter the stream's bounded delivery buffer.
- Backpressure: each client has a bounded buffer. The default broker
  keeps emitters non-blocking by dropping the oldest queued event when a
  client cannot keep up, so the subscriber retains the latest events.
  This is intentional: SSE is for push notifications, not durable
  delivery. Use a real queue for that.
- Clients that prefer delivery over emitter latency can opt in with
  `?slow=block` or `X-SSE-Slow: block`, but only on a broker whose host
  set `SSEBrokerConfig.AllowClientSlowMode`. Without that, the parameter
  is ignored and the subscriber gets oldest-drop. A blocking subscriber
  stalls the publisher's goroutine and therefore every other subscriber,
  so on a public endpoint a request-selected block mode is an
  unauthenticated DoS. See "Default oldest-drop versus `?slow=block`" in
  `live-dashboards.md`.
- `subscriber_id` is honoured. A reconnect replaces an existing
  subscriber only when the broker can identify the caller through
  `SSEBrokerConfig.Principal`. A subscriber id is a client-chosen
  label, not an identity. With no `Principal` the broker evicts nothing,
  so a reconnect does not drop the previous entry, and one client cannot
  evict another's stream by guessing its id. `MaxSubscribers` is exact:
  past the cap `Subscribe` rejects rather than evicting. A client whose
  previous connection is half-open keeps its seat until the next
  heartbeat write fails and that stream unregisters itself, which
  reclaims the seat for every client, including the ones that send no
  `subscriber_id`. See `live-dashboards.md` for the `Principal`
  rationale.
- The stream returns `503 Service Unavailable` if the entity has no
  event bus configured (the default `framework.NewApp` wires one).
- One caller holds at most 16 streams on an entity
  (`CrudHandler.EventStreamSeats`, settable via `WithEventStreamSeats`;
  a negative value lifts the cap). The caller is the owner id on an
  owner-scoped entity, else the signed-in user, else the tenant; every
  anonymous stream shares one bucket. Past the cap the default answers
  429 with `Retry-After`. `WithEventStreamSeatOverflow(stream.SeatOverflowEvictOldest)`
  closes that caller's oldest stream instead and seats the new one. An
  evicted stream's handler returns even when it is parked in a write to a
  client that stopped reading: eviction sets a past write deadline on its
  response, so the cap bounds handlers and connections without a server
  `WriteTimeout`.
- The stream re-validates its caller for its whole life, not just at
  connect. On every delivery and on a 30-second ticker
  (`CrudHandler.EventStreamReauth`, settable via
  `WithEventStreamReauth`) it re-establishes the principal: the
  battery/auth middleware that authenticated the request looks the
  session, API token or JWT owner up again, and `access.Middleware`
  re-derives the roles. The entity's read permission is then checked on
  that fresh principal, and the read-scope lift is re-evaluated on it
  before each event goes out, so the first private row written after a
  lifting role is removed does not reach the caller. A caller who would now be refused (session deleted, token
  revoked or expired, user deleted, role dropped, a resource decider
  flipped to deny) has the stream closed at the next check, even on an
  entity with no read permission. The EventSource reconnects and meets
  the full connect-time gate, which now refuses.
- Custom auth middleware opts in through `handler.WithPrincipalCheck`:
  install a check after `handler.SetUser` that re-resolves the caller
  and returns false once a fresh request would be refused. `SetUser`
  drops any earlier check. Without one, the stream keeps the identity it
  connected with and only the permission re-check applies. The island
  SSE stream (`/__gofastr/sse`) runs the same check before every push
  and heartbeat.

## Tenant scoping

When the entity is multi-tenant, the SSE stream filters events to the
tenant ID extracted from the request context. Other tenants' events
never reach the subscriber. Each re-check compares the refreshed
principal's tenant with the one the stream connected under, and closes
the stream when they differ. The tenant changes on a re-check only when
the middleware that set it re-resolves it: `tenant.ResolveMiddleware`
does, so a member removed from a tenant, or moved to another, loses the
feed at the next check. A middleware that calls `SetTenantID` once keeps
the connect-time tenant for the life of the stream.

## Read scope

An entity with `Exposure.ReadScope` filters each event against the
declared predicates in Go, since the feed has no query to narrow. Values
compare by the column's type, as List and Get bind them: a Bool scope
written `"1"`, `"t"` or `"true"` is the same value, as are `"+5"` and
`"5"` on an Int, `"1.50"` and `"1.5000"` on a Decimal, and two spellings
of one Timestamp instant. A record that lacks a scoped column, or holds
NULL in one, is withheld, so a delete (its payload is the key alone) does
not reach a restricted caller.

A `BeforeList` or `BeforeGet` hook that scopes reads (the "hide drafts
from non-editors" shape in [hooks and transactions](hooks-and-transactions.md))
binds the feed too. Each delivery runs the subscriber's own BeforeList
chain and its BeforeGet chain, with the row's id, on the context the
stream's last re-check refreshed, and looks the stored row up under the
clauses they append, one keyed `SELECT` per delivery and only when a hook
appended something. A row either route would hide is not sent. A hard
delete has no row left to look up, so it reaches only a subscriber the
hooks append no clause for, the way a delete does not reach a caller a
`ReadScope` restricts; a soft delete leaves the row in place and is
checked against it. A Before hook that errors withholds the event.

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

`app.Events()` is a method that returns the in-process `*EventBus`,
the same bus the SSE stream subscribes to. Calling `unsubscribe`
removes the handler and the bus keeps no reference to it, so a closure
that captured a request context or buffered events can be collected.

In-process subscribers are not subject to the 32-event SSE buffer.
`EmitAsync` runs each subscriber on its own goroutine, so a slow or
blocked handler does not hold back the others. That goroutine is new for
every event, so two events emitted one after the other can reach the same
subscriber in either order, or both at once. A subscriber that needs
order reads it from the data (a timestamp or version column), not from
arrival. The subscriber's context
keeps the emitter's values (the user, the tenant, the request id) but not
its cancel, because CRUD emits on the request context and the handler
has usually returned by the time a subscriber runs. Each run is bounded
by `event.AsyncHandlerTimeout` (one minute); work longer than that starts
its own job. A payload whose
`MarshalJSON` panics skips the fanout and still reaches local
subscribers.

## When NOT to use SSE

SSE is **push only**. Responses to user actions (clicks, form submits,
filter changes) must come back over the request that triggered them,
never via SSE. The framework's island runtime enforces this rule on
the UI side; the same rule applies to your own clients.

SSE is the last rung of the reactivity ladder. Reach for client
signals, RPC, and polling first; reserve SSE for product semantics
that need the connection: presence, collaborative editing,
sub-second internal dashboards. See [Reactivity model](reactivity.md)
for the full ladder and the decision criteria.

## See also

- [Live dashboards](live-dashboards.md) composes this push lane into a realistic ops dashboard, with the delivery-semantics decision table.
- [Reactivity model](reactivity.md) covers the four ways to make a page change after first paint; SSE is the last rung.
- [UI capability map](ui-capability-map.md) distinguishes realtime UI invalidation from durable workflow delivery.
- [Presence](presence.md) uses the same push lane for self-healing rosters.
- [Horizontal scaling](scaling.md) covers fanout and replica topology.
- [Benchmarks](benchmarks.md) documents SSE delivery/drop measurements.
## Common mistakes

- **Subscribing to SSE for confirmations.** SSE is fire-and-forget.
  If your client needs to know "did my POST succeed?", read the POST
  response. SSE is for the broadcast to *other* clients.
- **Counting on every event arriving.** Backpressure may drop events.
  Use SSE for "something changed, refetch", not for state machine
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
`cache.RedisClient`). The fanout is caller-owned: close it after the
app shuts down, and close the Postgres one before its `*sql.DB`. Its
`Close` cancels a fallback-row fetch in flight and returns once the
LISTEN dispatcher has stopped and released its pool connection.

**Semantics change under fanout: the bus becomes a broadcast.** Every
`On`/`Subscribe` handler fires on **every** replica for every event.
That is exactly right for UI push (each replica notifies its own
connected clients) and exactly wrong for side effects: a handler that
sends an email would send it N times. Per-event work belongs on the
durable lane (`WithOutboxConsumer`), which delivers to each consumer
once regardless of replica count.

Two rules for handlers under fanout:

- **Derive on the origin only.** A handler that emits a *new* event in
  response to one it received must gate on `event.IsRemote(ctx)`.
  Otherwise every replica derives its own copy and subscribers see
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
replica's listener is reconnecting is gone. A fanout backend whose
`Publish` panics is treated the same way: the bridge's publisher
goroutine recovers, logs the panic, and keeps draining — the lossy lane
degrades, the process does not die. Nothing about the durable lane
changes.

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
> upgrading**: the new relay ignores the old parent `dead`/single-row
> state (the DDL only adds the child `event_outbox_delivery` table; there
> is no automatic backfill of pre-upgrade rows).

### Two delivery lanes

Delivery is split across two disjoint lanes, so neither duplicates the
other:

- **Real-time lane (best-effort, ephemeral).** The live event bus is
  notified post-commit by `EmitEvent`, feeding SSE streams and ephemeral
  `On`/`Subscribe` handlers. This happens whether or not an outbox is
  configured. It is lossy by design: a crash here drops the in-memory
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

A payload that carries a user's data is staged with `AppendFor`, which names
the user: `App.EraseUserData` writes JSON `null` over the payload of every row
staged for the erased user, so neither a pending delivery nor a `Replay`
hands their record to a consumer. CRUD stages each lifecycle event this way,
for the record's owner (`Scope.OwnerField`); `Append` is `AppendFor` with no
owner.

```go
app.Outbox().AppendFor(ctx, tx, customerID, "order.placed", map[string]any{"id": orderID})
```

A host that creates the table itself (`outbox.WithoutEnsureTable`) adds the
`owner TEXT NOT NULL DEFAULT ''` column; the outbox adds it to a table it
manages on boot.

Standalone use (no App): `outbox.New(db)` + `ob.Consume(name, type,
handler)` + `ob.StartRelay(ctx)`.

### Semantics

- **Durable consumers receive the raw row.** The outbox stages the
  post-write `RETURNING` row verbatim, and the relay delivers that staged
  record with no AfterGet pass: durable consumers are trusted machinery
  and own their masking. The real-time lane is the opposite — SSE
  delivery runs the record through the entity's AfterGet hooks, so a
  field hidden by a read hook never reaches a subscriber in the clear.
  A webhook bridge, audit sink, or search indexer that forwards outbox
  records on to a less-trusted surface must apply its own masking.
  The staged row is the entity's own: children a cascade write nested
  into the response are left out, and each arrives as its own event.
- **At-least-once, per consumer.** The relay invokes each consumer's
  handler, then marks that consumer's delivery dispatched only after it
  returns nil. A crash between the two re-delivers, so consumers **must be
  idempotent**. Deduplicate on **`(consumer, Event.ID)`**, not `Event.ID`
  alone: `Event.ID` is the outbox row id and is the *same* for every
  consumer's delivery of that row, so a dedup store keyed on `Event.ID`
  alone that is shared across consumers would let one consumer's success
  suppress another's delivery, breaking sibling isolation.
- **Sibling isolation.** Each (row, consumer) pair has its own delivery
  row, retried and dead-lettered independently. One consumer that errors
  or panics never blocks its siblings or fails the whole row. A parent row
  is marked `dispatched` once it has no `pending` deliveries left (all
  `dispatched`/`dead`/`abandoned`), **and** every consumer the relay
  declares for its type has a delivery row, **and** it is older than the
  handler grace. The middle check matters on a backlog: each consumer's
  deliveries are created in its own batch, so a parent can hold one
  consumer's delivery before another's exists, and completing it then
  would skip the other consumer for good. It may complete with some
  deliveries dead; `Replay` / `ReplayConsumer` resurrects them.
- **Handler budget (30s default).** Each handler invocation gets a
  wall-clock budget (`outbox.WithHandlerTimeout`, 30s by default, the
  same posture as both queue backends). At the deadline the handler's
  context is cancelled; a handler that respects its context returns and
  the delivery retries as usual, while one that blocks past it has its
  delivery settled as failed — retries, siblings, and the rest of the
  batch proceed, so a hung dependency cannot wedge the durable lane.
  A handler that ignores its cancelled context cannot be killed; its
  late side effects are the duplicate idempotency already covers.
  The outcome is recorded under its own 10s window that opens when the
  handler returns, detached from relay shutdown, so a handler that runs
  longer than 10s (inside its budget) still settles `dispatched`.
- **Completion is age-gated.** A parent is not marked `dispatched` until it
  is older than `WithHandlerGrace` (default 15m), even once every current
  delivery is terminal. This is what makes a rolling deploy that *adds* a
  consumer safe: the added consumer's delivery row is created by an
  up-to-date replica on its own poll, and a parent completed too early
  would let the relay skip it forever (expand only touches `pending`
  parents). **Delivery to consumers is unaffected and prompt**: only the
  parent's `dispatched` bookkeeping (and retention/GC) lags by the grace.
- **Dead-letter & replay.** Every **claim consumes one attempt** in SQL
  (`attempts = attempts + 1` in the claim UPDATE, the battery/queue and
  battery/webhook spelling), so a relay that crashes between claim and
  settle (kill -9, OOM, mid-handler deploy) still burns budget and a
  claim/crash loop terminates at `MaxAttempts` instead of re-delivering
  forever; a delivery whose lease expired at an already-spent budget is
  dead-lettered by the pre-claim sweep, visible and replayable. A
  delivery that returns an error **or panics** (the relay reports a
  panicking consumer as a delivery error rather than swallowing it)
  schedules an exponential backoff; the settle writes state only, never
  the counter — two overlapping runners under lease overrun both count
  because both claims counted. After `MaxAttempts` (default 10) the
  delivery is marked `dead`. `WithMaxAttempts`, `WithBatchSize` (default
  100) and `WithPollInterval` (default 1s) keep their defaults when
  given zero or less. `Replay(rowID)` resets all dead/abandoned
  deliveries of a row; `ReplayConsumer(rowID, name)` resets one. A reset
  delivery's age starts over, so the handler grace below runs again from
  the replay.
- **Removed consumers are abandoned (time-based, not snapshot-based).** A
  delivery whose consumer has no handler on *any* replica is
  `abandoned`, settled terminal so it can't orphan the parent, but only
  once it is older than the **handler grace** (`WithHandlerGrace`, default
  15m). This is deliberately time-based: a lagging replica in a rolling
  deploy never abandons a *newly-added* consumer's fresh deliveries (the
  up-to-date replica delivers them first), and a genuinely-removed
  consumer's deliveries age out and abandon everywhere. The grace **must
  exceed your rolling-deploy overlap window plus worst-case clock skew**
  between replicas (delivery/parent timestamps are written on one replica
  and compared on another). Keep a floor of a few minutes. Re-adding a
  removed consumer resumes delivery for events staged from the re-add
  forward automatically; events abandoned during the gap are recovered
  with `ReplayConsumer`.
- **Undeclared event types are dropped after the grace.** A staged event
  whose type has no consumer gets no deliveries and is marked `dispatched`
  once older than the handler grace (deliveries are expanded oldest-first,
  so an old parent with none has no consumer anywhere). Events staged more
  than the grace before a type's *first* consumer is added are not
  back-delivered, a retention-style boundary. If **no** consumer is
  declared at all, the relay logs a Warn at start.
- **Retention (optional).** `WithRetention(d)` makes the relay purge
  fully-settled (`dispatched`) parent rows and their deliveries once older
  than `d`. Pending, dead, and abandoned rows are never purged, and a
  dispatched parent still carrying any undispatched delivery is
  retained with it, so `Replay` keeps the payload and `last_error` it
  needs, and a delivery `Replay` reset while the parent completed on
  Postgres is still delivered. Retention is not a dead-letter TTL: such a parent lives until
  its delivery is replayed to completion. Unset (default) keeps every
  row forever.
- **Stalled relays are logged.** If the relay can't claim/expand (missing
  table under `WithoutEnsureTable`, a renamed table, or a DB outage) it
  keeps polling rather than crashing, and logs once at Error on onset and
  once at Info on recovery.
- **Data shape.** `Data` is stored as JSON and unmarshalled into `any`
  before delivery — whatever `Append` accepts and stages inside the
  business transaction reaches the consumer (objects arrive as
  `map[string]any`, arrays as `[]any`, scalars as their JSON types),
  matching the live bus contract; a staged payload can never be an
  undeliverable poison row. Numbers arrive as `float64` (JSON has no
  separate integer type).
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
- **Multi-replica safe.** The claim takes a lease (`claimed_until`) and
  consumes one attempt (`attempts = attempts + 1`) at the delivery
  grain, so a relay that dies mid-batch releases only its claimed
  deliveries after the lease expires and another relay reclaims them,
  and every claim spent budget toward `MaxAttempts`. Every settle is
  fenced on the lease its own claim wrote (`claimed_until`): a relay
  whose lease expired and was re-claimed settles nothing, so its late
  write cannot clear the re-claimant's lease and let a third relay claim
  the delivery mid-run. The current claimant's outcome decides the row.
  The lease is five minutes, or twice the handler budget plus the 10s
  settle window when that is longer: a `WithHandlerTimeout` of ten
  minutes gets a lease of 20m20s, so a slow handler still inside its
  budget is never re-claimed by another relay. The cost is that a relay
  that crashes holding such a claim hands it on that much later.
  A relay that has less lease left than one handler run plus its settle
  hands the rest of its batch back unstarted, attempts refunded; the
  first delivery of a batch always runs. A graceful shutdown mid-batch
  (the relay's context cancelled) hands back the unstarted rest the same
  way; only a relay that dies outright leaves them leased.
  There is no double-processing beyond the at-least-once caveat.
