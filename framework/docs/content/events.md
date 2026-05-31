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
