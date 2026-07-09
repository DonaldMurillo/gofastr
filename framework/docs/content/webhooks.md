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
when the host is a name (DNS lookup at subscribe time). It also runs
**again at dial time**: the default delivery client's
`Transport.DialContext` installs a `net.Dialer.Control` hook that
re-validates the actual resolved IP at connect time. This closes the
DNS-rebinding / TOCTOU window where a host validates public at
`Subscribe` and is then re-pointed at `127.0.0.1` /
`169.254.169.254` / an RFC1918 address before the worker fires — the
connection is refused before any bytes leave the process.

Supplying your own `Options.HTTPClient` (proxy, tracing, custom
timeout) does **not** drop the guard: `New` wraps the client with a
per-request check that resolves the delivery target and refuses
internal IPs before your transport runs. Your transport itself is
used verbatim — a private egress proxy, SSH tunnel, or custom dialer
keeps working, since it is the *target* that must be public, not the
route to it. Only `AllowPrivateNetworks: true` opts out.

For development and tests, opt out via `Options.AllowPrivateNetworks =
true`. This disables both the subscribe-time and the dial-time IP
checks. The scheme guard still applies — `file://`, `gopher://`, etc.
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
  use. Options:
  - `WithSQLSubscribersTable(name)` / `WithSQLDeliveriesTable(name)`
    — override table names.
  - `WithSQLSecretCodec(codec)` — encrypt subscriber secrets at rest
    (see Secret encryption below). Default is `NoopSecretCodec`
    (plaintext).

Both bundled stores implement the optional `LeasedStore` interface:

```go
type LeasedStore interface {
    Store
    ClaimDueDeliveries(ctx, now, limit, leasePeriod) ([]Delivery, error)
}
```

`ClaimDueDeliveries` atomically reserves rows for the calling worker
and pushes their `NextAttemptAt` forward by `leasePeriod`, so a
concurrent Manager sees them as not-yet-due and skips them. The
Manager auto-detects the interface and uses the claim path — making
multi-instance deployments safe against double delivery. Set
`Options.LeasePeriod` (default 30s) above your worst-case handler
latency.

## Secret encryption at rest

The SQL store can encrypt `webhook_subscribers.secret` so a DB
snapshot doesn't hand out HMAC keys:

```go
key := mustReadFromKMS() // 32 bytes for AES-256
codec, err := webhook.NewAESGCMSecretCodec(key)
if err != nil { /* ... */ }
store, _ := webhook.NewSQLStore(db, webhook.WithSQLSecretCodec(codec))
```

The encoded format is `wbenc:v1:<base64(nonce||ciphertext)>`. Rows
without the `wbenc:` prefix are returned as-is on read so an existing
deployment can roll the codec without a one-shot rewrite job — each
subscriber's secret is re-encrypted the next time the row is
upserted.

Key rotation: re-encrypt subscribers by reading them through a codec
that decrypts with the old key and encrypts with the new one
(typically a small wrapper around two `NewAESGCMSecretCodec`
instances), then write each subscriber back through `AddSubscriber`.
The package keeps a single-key codec primitive on purpose; a key
ring belongs in your KMS adapter.

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

## Inbound ingestion

Everything above is outbound — you calling other systems. The receiving
side is `IngestHandler`: an HTTP handler that verifies a request, persists
an envelope, acks immediately, and hands the real work to the queue
battery. It is the inbound mirror of the Manager.

### Wiring

```go
import "github.com/DonaldMurillo/gofastr/battery/webhook"
import "github.com/DonaldMurillo/gofastr/battery/queue"

store := webhook.NewMemoryInboundStore()
q := queue.NewMemoryQueue(4)

// Build the ingestion endpoint. The handler does NOT touch the store
// until the signature checks out.
h, err := webhook.IngestHandler(webhook.IngestConfig{
    Source:       "github",
    Verifier:     webhook.TimestampedVerifier(secret, 5*time.Minute),
    Store:        store,
    Queue:        q,
    JobType:      "webhook.inbound",
    MaxBodyBytes: 1 << 20,             // default 1 MiB
    KeepHeaders:  []string{"X-GitHub-Event", "X-GitHub-Delivery"},
    DedupeKeyFunc: func(r *http.Request, _ []byte) string {
        return r.Header.Get("X-GitHub-Delivery")
    },
})
mux.Handle("/hooks/github", h)
q.Start()
defer q.Close()

// Register the queue handler that does the actual work.
q.RegisterHandler("webhook.inbound", webhook.ProcessInbound(store, func(ctx context.Context, e webhook.InboundEnvelope) error {
    // ...your business logic, with the verified payload in hand
    return nil
}))
```

`IngestConfig.Queue` is optional. Leave it nil to persist-and-forget
(the envelope is still acked 202); set it to fan processing off the
request path.

### Request flow

1. Non-POST → `405`.
2. Body read through `http.MaxBytesReader`; oversize → `413`.
3. **Verify, then persist** — an unverified payload is never written to
   the store. A verification failure responds `401` with a generic body
   (`signature verification failed`); no header value or reason detail
   is echoed.
4. Dedupe: if `DedupeKeyFunc` returns a non-empty key the store has
   already seen for this source, the handler acks `200` immediately
   without re-persisting or enqueuing — idempotent redelivery.
5. Persist the envelope as `received`, allowlisted headers only. With a
   queue wired, the dedupe key is deliberately **not** stored yet.
6. Enqueue a `queue.Job{Type: JobType, Payload: {"envelope_id", "source"}}`.
7. Register the dedupe key on the envelope — only now that the event is
   durably queued may redeliveries be dedupe-acked.
8. Respond `202` with `{"id": "<envelope id>"}`.

On enqueue failure the handler responds `500` (the sender retries) and
best-effort marks the just-written envelope `failed` with `LastError` as
a forensic record. Because the key is registered only *after* a
successful enqueue (step 7), an envelope that never reached the queue
can never dedupe-ack the sender's retry — the redelivery persists and
enqueues a fresh copy even if the forensic marking itself failed.
Without a queue, persistence alone is durable acceptance, so the key is
stored with the envelope up front. Failures of these best-effort updates
are reported through `IngestConfig.Logger` (default `log.Printf`).

### Verifiers

Two are bundled:

- `TimestampedVerifier(secret, tolerance)` — wraps
  `VerifyTimestamped` over `X-GoFastr-Signature`. The timestamp is bound
  into the signed material, so a captured request can't replay past
  `tolerance`. **Preferred whenever the sender supports it.**
- `HMACSHA256Verifier(header, prefix, secret)` — GitHub-style
  `<prefix><hex-hmac-sha256-of-body>` (e.g. header
  `X-Hub-Signature-256`, prefix `sha256=`). Uses `hmac.Equal`, but note
  there is **no timestamp binding** — it offers no replay defense. Use it
  for providers that don't send a timestamp; pair it with a short
  dedupe window if you can.

Provide your own `InboundVerifier` for other schemes (RSA signatures,
mTLS-extracted identity, a multi-key rotation lookup). Implementations
must be constant-time on the secret.

### Envelope lifecycle

`ProcessInbound` adapts your business function into a `queue.Handler`
that drives the envelope through its states:

```
received → processing → processed
                   └→ failed  (fn returned an error)
```

It loads the envelope by `envelope_id` from the job payload, marks it
`processing` (incrementing `Attempts`) before calling your function, then
`processed` on success or `failed` (with `LastError` set) on error. It
returns your function's error so the queue's own retry/backoff can
reschedule it.

### Stores

Two `InboundStore` implementations ship:

- `NewMemoryInboundStore()` — in-process map; tests and single-instance.
- `NewSQLInboundStore(db, opts...)` — SQL-backed (sqlite + postgres),
  creates `webhook_inbound` on first use. `WithInboundTable(name)`
  overrides the table name. Headers persist as a JSON TEXT column.

Dedupe is enforced application-side via `SeenDedupeKey` rather than a
database unique constraint: a portable partial index (Postgres) can't be
written in one DDL that also works on SQLite, and a plain unique index
would forbid two legitimately-undedupe'd (empty-key) requests from
coexisting. The `(source, dedupe_key)` index keeps the lookup cheap, but
there is an inherent check-then-insert race window under concurrency —
make your `ProcessInbound` handler idempotent to absorb the rare duplicate.

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
- **Multi-instance writers need the lease path.** The bundled SQL
  store implements `LeasedStore` — on Postgres via
  `FOR UPDATE SKIP LOCKED` inside an `UPDATE … RETURNING`, on SQLite
  via a serializable `BEGIN IMMEDIATE` transaction. The Manager
  automatically uses it when the store implements the interface.
  Custom stores that don't implement `LeasedStore` are safe for
  single-instance deployments only — concurrent workers against a
  plain `DueDeliveries`-only store can double-deliver.
- **The bridge calls `Publish` synchronously inside the emitter's
  goroutine.** With a SQL store, that means each `Emit` does a write
  per matching subscriber before returning. If the emitter is a hot
  path, switch to `EmitAsync` or fork a goroutine in your own
  subscribe handler.
