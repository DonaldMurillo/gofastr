# Entity events

Every CRUD-enabled entity emits structured lifecycle events (`created`, `updated`, `deleted`, and state transitions) both in-process in Go and over a per-entity SSE endpoint (`GET /<table>/_events`).

> **Remember the reactivity ladder:** for UI freshness, prefer `data-cui-poll` before reaching for SSE. When you do need sub-second push in the browser, subscribe through the single `/__gofastr/sse` bus rather than opening ad-hoc `EventSource` connections per widget — see [Reactivity](reactivity.md).

## Subscribing in Go

Register in-process subscribers on your app or through the generated typed wrappers in `entities/events.go`:

```go
// Typed wrapper generated for each entity in entities/events.go:
// entities.OnOrdersCreated(app, func(ctx context.Context, order entities.Order) { ... })
// entities.OnOrdersUpdated(app, func(ctx context.Context, order entities.Order) { ... })
// entities.OnOrdersDeleted(app, func(ctx context.Context, id any) { ... })
```

Delivery semantics:
- **Post-commit only:** events fire only after the database write commits. A rolled-back transaction emits nothing.
- **Non-blocking:** subscribers receive committed notifications without holding open the SQL transaction. Keep handlers fast or hand off heavy work to `battery/queue`.
- **In-process only:** built-in entity events run inside a single server process. For multi-instance deployments or guaranteed retry/delivery across restarts, persist to a queue or outbox (see [Queue](queue.md) and [Webhooks](webhooks.md)).

## HTTP event stream (`GET /<table>/_events`)

External consumers, CLIs, and local tools can subscribe to `GET /<table>/_events` (`text/event-stream`):

- **Auth & scoping:** the stream enforces the entity's `Exposure.Access.Read`, `Scope.OwnerField`, `Scope.MultiTenant`, and `ReadScope`. A subscriber only receives events for rows it is allowed to read.
- **Hidden fields stripped:** `Hidden` columns never appear in SSE payloads.
- **Event payload:** each SSE message carries the event type (`created`, `updated`, `deleted`, `transitioned`), entity name, record ID, and sanitized record JSON (using the mount's JSON wire casing).
- **Filtering:** pass standard equality query filters (such as `?status=open`) to narrow the stream, respecting `NoQuery` rules.
- **Backpressure & heartbeats:** slow clients that fall behind their buffer are disconnected cleanly rather than stalling writers; periodic comment heartbeats keep proxies from dropping idle connections.

## Common mistakes

- **Opening multiple `EventSource` connections from browser UI widgets:** use `data-cui-poll` for passive freshness or the shared `/__gofastr/sse` bus for live push (see [Reactivity](reactivity.md)).
- **Relying on in-process events for durable workflows across restarts:** in-process events are fire-and-forget. Use `battery/queue` or an outbox table for work that must survive crashes or run across replicas.
- **Doing slow network I/O synchronously inside an event subscriber:** offload slow calls (emails, webhooks, LLM calls) to a background job so event dispatch stays fast.
- **Expecting events from rolled-back transactions:** events fire after commit, never before.
