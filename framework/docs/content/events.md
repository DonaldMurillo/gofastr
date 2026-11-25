# Entity events

Every CRUD-enabled entity emits structured lifecycle events (`entity.created`, `entity.updated`, and `entity.deleted` — state transitions emit `entity.updated`) both in-process in Go and over a per-entity SSE endpoint (`GET /<table>/_events`).

> **Remember the reactivity ladder:** for UI freshness, prefer `data-cui-poll` before reaching for SSE. When you do need sub-second push in the browser, subscribe through the single `/__gofastr/sse` bus rather than opening ad-hoc `EventSource` connections per widget — see [Reactivity](reactivity.md).

## Subscribing in Go

Register in-process subscribers on `app.Events` or through the generated typed wrappers in `entities/events.go` (each returns an unsubscribe `func()`):

```go
unsub := entities.OnOrdersCreated(app, func(ctx context.Context, order *entities.Orders) error {
	// enqueue email, update projection, etc.
	return nil
})
defer unsub()
```

Delivery semantics:
- **Post-commit only:** events fire only after the database write commits. A rolled-back transaction emits nothing.
- **Non-blocking:** subscribers receive committed notifications without holding open the SQL transaction. Keep handlers fast or hand off heavy work to `battery/queue`.
- **Multi-replica & durable delivery:** by default the event bus is in-process. Attach `framework.WithFanout(...)` to broadcast live events across replicas, or `framework.WithOutbox(...)` + `framework.WithOutboxConsumer(...)` for transactional, restart-safe delivery (see [Scaling](scaling.md), [Queue](queue.md), and [Webhooks](webhooks.md)).

On shutdown, an app with fanout detaches the event bridge and waits for its publisher until the drain context ends. If the publisher is still blocked, shutdown reports the context error and continues with the remaining stop hooks. The blocked publish can outlive shutdown if the fanout backend ignores its publish context. When attaching a fanout to a standalone bus, use `event.AttachFanoutDrainer` for a context-bounded publisher wait.

## HTTP event stream (`GET /<table>/_events`)

External consumers, CLIs, and local tools can subscribe to `GET /<table>/_events` (`text/event-stream`):

- **Auth & scoping:** the stream enforces the entity's `Exposure.Access.Read`, `Scope.OwnerField`, `Scope.MultiTenant`, and `ReadScope`. A subscriber only receives events for rows it is allowed to read.
- **Hidden fields stripped:** `Hidden` columns never appear in SSE payloads.
- **Event payload:** each SSE message carries the event type (`entity.created`, `entity.updated`, `entity.deleted`), entity name, record ID, and sanitized record JSON (using the mount's JSON wire casing). The stream does not apply query-string filters.
- **Backpressure & heartbeats:** each subscriber has a 32-event buffer; if a slow client falls behind, the oldest queued events are dropped rather than stalling writers. Periodic comment heartbeats keep proxies from dropping idle connections.

## Common mistakes

- **Opening multiple `EventSource` connections from browser UI widgets:** use `data-cui-poll` for passive freshness or the shared `/__gofastr/sse` bus for live push (see [Reactivity](reactivity.md)).
- **Relying on the default in-process bus for durable or multi-replica workflows:** add `framework.WithOutbox` for restart-safe delivery, `framework.WithFanout` for cross-replica broadcast, or `battery/queue` for background work.
- **Doing slow network I/O synchronously inside an event subscriber:** offload slow calls (emails, webhooks, LLM calls) to a background job so event dispatch stays fast.
- **Expecting events from rolled-back transactions:** events fire after commit, never before.
