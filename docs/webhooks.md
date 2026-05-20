# Outbound webhooks

`battery/webhook` delivers signed POST requests to subscriber URLs
with retry-with-backoff and a dead-letter terminal state. It's the
external mirror of `framework/event` (which is internal pub/sub) — use
events for in-process listeners, webhooks for talking to other
systems.

## Wiring

```go
import "github.com/DonaldMurillo/gofastr/battery/webhook"

store := webhook.NewMemoryStore()
mgr := webhook.New(store, webhook.Options{
    // All optional; defaults shown.
    // MaxAttempts:          6,
    // Backoff:              []time.Duration{30s, 1m, 5m, 15m, 1h, 3h},
    // PollInterval:         1 * time.Second,
    // MaxResponseBodyBytes: 64 << 10,   // 64 KiB; protects against malicious receivers
    // AllowPrivateNetworks: false,      // SSRF guard; flip true for dev/tests only
})
mgr.Start()
defer mgr.Stop(ctx)
```

`Stop(ctx)` cancels any in-flight HTTP attempt via the worker's context
and waits for the worker goroutine to drain. Pass a tight context if
shutdown speed matters more than letting a hung receiver complete.

Register a subscriber and publish:

```go
sub, err := mgr.Subscribe(ctx, webhook.Subscriber{
    URL:    "https://customer.example.com/hooks",
    Secret: "share-this-with-the-customer",
    Events: []string{"orders.**", "shipments.created"},
})
// ...
queued, err := mgr.Publish(ctx, "orders.created", []byte(`{"id":42}`))
```

`Publish` returns the number of subscribers the event was queued for.
The actual HTTP POST happens on the manager's worker goroutine.

## SSRF guard

By default, `Subscribe` rejects URLs that target internal
infrastructure: RFC1918 (`10.*`, `192.168.*`, `172.16-31.*`), loopback
(`127.0.0.0/8`, `::1`), link-local (`169.254.0.0/16`, including the
AWS/GCP metadata endpoint at `169.254.169.254`), the unspecified
address (`0.0.0.0`), `localhost`, `*.localhost`, `*.internal`, and
`metadata.google.internal`. Schemes other than `http`/`https` are
always rejected.

The guard runs both on the hostname directly and on every resolved IP
when the host is a name (DNS lookup at subscribe time). DNS rebinding
that flips to a private IP between subscribe and dial is mitigated by
the standard library's `net.DefaultResolver` honoring the dial context
— but if your threat model needs strict resolve-at-dial enforcement,
supply an `HTTPClient` with a `Transport.DialContext` that re-validates
the connecting IP.

For development and tests, opt out via `Options.AllowPrivateNetworks =
true`. The scheme guard still applies — `file://`, `gopher://`, etc.
are always refused.

## Delivered request shape

| Header                   | Meaning                                                                    |
|--------------------------|----------------------------------------------------------------------------|
| `Content-Type`           | `application/json`                                                         |
| `User-Agent`             | `GoFastr-Webhook/1`                                                        |
| `X-GoFastr-Event`        | The event name (`orders.created`)                                          |
| `X-GoFastr-Delivery-ID`  | Stable random ID for this delivery row                                     |
| `X-GoFastr-Timestamp`    | Unix seconds at send time (informational; the signed value is authoritative) |
| `X-GoFastr-Signature`    | `t=<unix>,v1=<hex HMAC-SHA256(secret, "<unix>.<body>")>`                    |

Binding the timestamp into the signed material is the same convention
Stripe uses; receivers reject captured payloads outside their tolerance
window so a leaked delivery cannot replay forever.

The body is whatever you passed to `Publish` — `application/json`
isn't required, but the header advertises it.

## Verifying inbound webhooks (receiver side)

```go
body, _ := io.ReadAll(r.Body)
ok := webhook.VerifyTimestamped(
    secret,
    r.Header.Get(webhook.SignatureHeader),
    body,
    5*time.Minute, // tolerance window
)
if !ok {
    http.Error(w, "bad signature", http.StatusUnauthorized)
    return
}
```

`VerifyTimestamped` is constant-time and rejects:

- empty secret
- header missing `t=` or `v1=` fields
- a timestamp outside the tolerance window (replay defense)
- mismatched HMAC (any body tampering)

The legacy `Sign` / `Verify` helpers (with the `sha256=` prefix) remain
for backwards compatibility with stored signatures but are deprecated —
they do not bind a timestamp and so cannot defend against replay.

## Response handling

Each attempt reads at most `Options.MaxResponseBodyBytes` of the
receiver's response body (default 64 KiB), then discards it. A
malicious or buggy receiver cannot exhaust the manager's memory by
returning a multi-gigabyte body.

## Event matching

Subscriber `Events` is a list of glob patterns. Two wildcards are
supported:

| Pattern         | Matches                                                |
|-----------------|--------------------------------------------------------|
| `*`             | everything                                             |
| `**`            | everything (alias of `*`)                              |
| `orders.created`| exact only                                             |
| `orders.*`      | `orders.created`, `orders.shipped` (single segment)    |
| `orders.**`     | `orders.created`, `orders.line.added`, `orders.x.y.z`  |
| `*.created`     | `orders.created`, `users.created` (single segment)     |
| `a.*.c`         | `a.b.c`, `a.x.c`                                       |
| `a.**.c`        | `a.b.c`, `a.b.x.c`, ...                                |

Pass an empty list to `Subscribe` and it defaults to `["*"]`.

## Retry semantics

A delivery succeeds when the receiver returns 2xx. Anything else (4xx,
5xx, network error) marks the delivery `failed`, schedules the next
attempt using `Options.Backoff`, and the worker picks it up at the
scheduled time.

After `Options.MaxAttempts` attempts the delivery transitions to
`dead`. Dead deliveries are not retried; they remain in the store for
inspection / replay via your own admin tooling.

If a subscriber is removed while a delivery for it is pending, the
delivery transitions straight to `dead` with `LastError =
"subscriber gone or inactive"`.

## Pausing a subscriber

Set `Paused: true` in the `Subscriber` you pass to `Subscribe` to
register it inactive; `Publish` skips paused subscribers and no
deliveries are queued. To resume, re-subscribe (Subscribe upserts on
ID) with `Paused: false`. The default for a new subscription is active.

## Stores

`webhook.Store` covers subscribers and deliveries:

```go
type Store interface {
    AddSubscriber(ctx, Subscriber) error
    GetSubscriber(ctx, id) (*Subscriber, error)
    ListSubscribers(ctx) ([]Subscriber, error)
    DeleteSubscriber(ctx, id) error

    AddDelivery(ctx, Delivery) error
    UpdateDelivery(ctx, Delivery) error
    ListDeliveries(ctx, subscriberID, limit) ([]Delivery, error)
    DueDeliveries(ctx, now, limit) ([]Delivery, error)
}
```

Two stores are bundled:

- `NewMemoryStore()` — in-process maps, suitable for tests and
  single-instance apps that tolerate restart loss.
- `NewSQLStore(db, opts...)` — SQL-backed (sqlite + postgres),
  creates `webhook_subscribers` and `webhook_deliveries` on first
  use. Pass `WithSQLSubscribersTable(...)` /
  `WithSQLDeliveriesTable(...)` to override names. Deliveries
  survive restart and the worker picks back up on whatever rows are
  due.

## Auto-bridging from `framework/event`

When you want every internal event to fan out to webhook subscribers
without per-event glue, call `webhook.Bridge`:

```go
cancel := webhook.Bridge(app.Events(), mgr)              // entity.created/updated/deleted
defer cancel()

cancel := webhook.Bridge(app.Events(), mgr, "orders.**") // custom list
```

The bridge subscribes one handler per event type, marshals
`event.Event.Data` to JSON, and calls `Manager.Publish`. The returned
`cancel` detaches every subscription at once — call it before
`Manager.Stop` so no Emit lands after the worker exits.

## Common mistakes

- **Don't pass the same secret to every subscriber.** Generate one
  per subscriber on registration so a leaked secret only exposes one
  endpoint.
- **Don't trust an unsigned request.** Even if the URL is private,
  rotate keys via the same code path you use for public consumers —
  signature checks are cheap insurance.
- **Don't put a database call on the publish path.** `Publish` writes
  one delivery row per matching subscriber. If you have many
  subscribers per event, accept the latency or denormalize.
- **Don't catch up old events at startup by replaying everything.**
  Use the `DueDeliveries` query for genuinely retryable rows; events
  that pre-date the subscriber should be backfilled deliberately, not
  resurrected by the retry loop.
- **Don't run multiple workers against the same SQL store without a
  row claim.** Today the SQL store's `DueDeliveries` query is a plain
  `SELECT` — two concurrent workers can pick the same row and double-
  deliver. Run a single Manager per database, or wrap your own
  `FOR UPDATE SKIP LOCKED` (postgres) / equivalent.
- **Secrets are stored plaintext.** `webhook_subscribers.secret` is
  not encrypted at rest today. Use a column-level encryption layer
  (or an envelope-encrypted column in your own schema) until that
  ships natively.
- **The bridge calls `Publish` synchronously inside the emitter's
  goroutine.** With a SQL store, that means each `Emit` does a write
  per matching subscriber before returning. If the emitter is a hot
  path, switch to `EmitAsync` or fork a goroutine in your own
  subscribe handler.
