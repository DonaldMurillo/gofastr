# Live dashboards

A live dashboard combines server-rendered UI (`ui.StatCard`, `ui.Timeline`, `ui.DataTable`), derived client state (`store.Computed`), narrow `data-island` push slots, a reconnect refresh endpoint, and `ui.NetworkRetryBanner`.

For a runnable reference, see [`/examples/live-dashboard`](../../../examples/site/screen_livedash.go) in `examples/site`.

## Poll first, SSE when you actually need push

- **Use `data-cui-poll` (recommended for 5–30s cadences):** `<div data-cui-poll="10s" data-cui-poll-src="/islands/stats">`. Any replica answers from the DB—no held connections, no fanout required.
- **Use SSE (`/__gofastr/sse`) for sub-second ticks or live presence:** Costs a held connection per tab and requires `framework.WithFanout` across multiple replicas.
- **Never use SSE for durable side effects:** SSE is best-effort push. Billing, audit logs, and every-event-matters workflows belong on `framework.WithOutboxConsumer` or `battery/queue` (see [events.md](events.md)).

## Wiring an SSE-pushed dashboard

1. **Declare the topic on the screen (`uihost.PresenceScreen`):** `uihost` signs `PresenceTopics(ctx)` into `<meta name="gofastr-sse">`, so the client automatically joins on SSE connect.
2. **Split each region into its own narrow `data-island` slot:** Keep metric cards, activity feeds, and tables separate so a metric tick doesn't re-render the whole page. Give `ui.DataTable` rows stable `Row.ID`s, and trim activity feeds server-side before pushing.
3. **Push rendered HTML to topic viewers:**

```go
mgr.SetOnPresenceChange(func(topic string) {
    rosterHTML := string(renderRoster(mgr.PresenceRoster(topic)))
    for _, sid := range mgr.PresenceSessions(topic) {
        mgr.PushUpdate(island.IslandUpdate{
            IslandID: "roster-" + topic,
            HTML:     rosterHTML,
        }, sid)
    }
})
```

A background ticker uses the same `PresenceSessions(topic)` + `PushUpdate` loop—stop it in `app.OnStop` so `SIGTERM` drains cleanly.

## Accessibility, DOM discipline & reconnect

- **Never put high-frequency numbers in `aria-live`.** Rapidly changing `StatCard` numbers flood screen readers; put `role="status"` only on bounded activity feeds or status transitions.
- **Coalesce bursts & pause hidden work:** Fold multiple server updates into one `PushUpdate`, coalesce custom DOM listeners with `requestAnimationFrame`, pause client timers on `visibilitychange`, and disconnect custom observers on `gofastr:navigate`.
- **Reconcile on reconnect:** The runtime's `sse` module reconnects with jittered exponential backoff (capped at 60s) and dispatches `gofastr:sse-status` (`{connected, lastEventAt, retryCount}`), which `ui.NetworkRetryBanner` listens to automatically (`HealthEndpoint` must be a relative path or `http(s)` URL). Because SSE does not replay missed frames, listen for reconnects and refetch authoritative island HTML when needed:

```js
document.addEventListener('gofastr:sse-status', (ev) => {
  if (!ev.detail.connected) return;
  // fetchPage writes through the runtime's Trusted Types policy; a raw
  // el.innerHTML = html throws under the default CSP.
  window.__gofastr.fetchPage("/__site/livedash/refresh?island=stats",
    '[data-island="livedash-stats"]');
});
```

## SSE broker backpressure & caps

When configuring a custom `core/stream.SSEBroker`, default delivery is **oldest-drop** (stale metrics drop so viewers always see the latest state):

```go
stream.NewSSEBroker(stream.SSEBrokerConfig{
    Topic:                "dashboard",
    AllowClientSlowMode:  false,          // keep false on public endpoints; ?slow=block stalls publishers
    BlockTimeout:         2 * time.Second,
    MaxSubscribers:       500,
    MaxSeatsPerPrincipal: 16,             // default 16 per caller (-1 lifts)
    SeatOverflow:         stream.SeatOverflowEvictOldest, // or SeatOverflowRefuse (429)
    Principal:            principalFromRequest, // required for safe per-caller eviction
})
```

Always set `Principal` to a server-derived user/session ID—without it, all anonymous callers share one seat bucket and `?subscriber_id` reconnects won't evict stale entries. Under `framework.WithFanout`, gate event-derived side effects on `!event.IsRemote(ctx)` so every replica doesn't duplicate work.

## Tenant isolation (critical for multi-tenant apps)

Never copy a single fixed topic (like the single-tenant demo's `"live-dashboard-demo"`) into a multi-tenant app. Even with an authorization hook, a shared topic broadcasts identical HTML to every subscriber on it.

1. **Derive a tenant-qualified topic server-side** from the authenticated context (`"tenant:" + tenantID + ":dashboard"`) inside `PresenceTopics(ctx)`.
2. **Install `host.Islands.SetAuthorizeTopic`** so topic membership is re-checked at SSE connect and on every heartbeat (silent drop on failure):

```go
host.Islands.SetAuthorizeTopic(func(ctx context.Context, topic string) bool {
    want, err := dashTopicFor(ctx)
    return err == nil && topic == want
})
```

3. **Render and push per-tenant snapshots** to `host.Islands.PresenceSessions(topic)` (track active topics when admitted and prune when empty).
4. **Derive tenant from auth on the refresh endpoint too**—never accept `?tenant=` from the query string.

## Common mistakes

- **Using SSE when `data-cui-poll` would do.** A dashboard that updates every 10–30 seconds doesn't need held connections or multi-replica fanout.
- **Treating SSE as a durable event log.** Use the transactional outbox or `battery/queue` when every event matters.
- **Putting live numeric gauges inside `aria-live`.** Reserve `role="status"` for bounded feeds and state changes.
- **Pushing one giant dashboard island.** Split stats, feeds, and tables into independent `data-island` slots.
- **Sharing a fixed topic across tenants.** Always qualify topics by server-derived tenant ID and verify with `SetAuthorizeTopic`.
- **Expecting SPA navigation from another page to join a new SSE topic.** `<meta name="gofastr-sse">` is stamped at initial document load; full-load the dashboard URL (or mark its link outside SPA) if the user navigated from an unrelated page without that topic.
- **Forgetting to stop background tickers in `app.OnStop`.** Always cancel ticker contexts on shutdown.
