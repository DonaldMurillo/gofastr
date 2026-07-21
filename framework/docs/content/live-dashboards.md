# Live dashboards

A live dashboard is the canonical composition of GoFastr's push primitives:
server-rendered charts and StatCards, a bounded activity feed, a keyed
changing collection, an authoritative refresh endpoint, and the
connection-health banner. This doc is the reference for how those
primitives fit together — and the boundaries that decide when each is
the right shape versus when you need a durable queue.

The runnable proof lives at
[`/examples/live-dashboard?presence=live-dashboard-demo`](../../../examples/site/screen_livedash.go)
(`examples/site`). Open it in two browsers: both see the same metric
ticks, because pushes are broadcast to every session that joined the
`live-dashboard-demo` presence topic.

## What the example demonstrates

The dashboard composes existing primitives — no bespoke CSS, no new
runtime JS. Each region maps to one product job:

| Region | Primitive | What it proves |
|---|---|---|
| Metric StatCards (throughput, workers, queue depth, p99) | `ui.StatCard` inside a `data-island` slot | Server re-renders fresh HTML on each tick and `PushUpdate`s it; the runtime swaps just that region's `innerHTML`. The cards are NOT `aria-live` — high-frequency numbers would flood assistive tech. |
| Activity feed | `ui.Timeline` in a `data-island` slot with `role="status"` | The polite announcement lane. The server trims to the N most recent entries before each push — the client never buffers. |
| Jobs table | `ui.DataTable` (rows keyed by `Row.ID`) in a `data-island` slot | A keyed changing collection. Successive pushes produce HTML that differs only on rows that actually changed. |
| Operational status pill | `store.Computed` bound via `Slice.Bind` | A derived client-side value. The operator's +/- buttons mutate two signals with `data-fui-signal-inc`; the reducer runs in the browser and the pill updates live. No RPC. |
| Connection-health banner | `ui.NetworkRetryBanner` | Watches the SSE lane. `SSESilenceMs` trips the banner if the ticker goes quiet; the Retry button probes a health endpoint. |
| Reconnect refresh | `GET /__site/livedash/refresh?island=…` | The islands are reconstructable. SSE is lossy; on reconnect an app fetches the current island HTML to reconcile. The runtime does not do this for you. |
| Topic-scoped delivery | `host.Islands.PresenceSessions(topic)` | Only sessions that joined the topic receive pushes. The demo uses ONE fixed topic because it has no identity; a multi-tenant app derives a tenant-qualified topic per authenticated user (see [Tenant isolation](#tenant-isolation)). |

## The wiring in one paragraph

A page links with `?presence=live-dashboard-demo`. `handlePage` threads
that value into the SSE `<meta>` tag so the client's `EventSource` opens
`/__gofastr/sse?session=X&presence=live-dashboard-demo`. The server-side
join happens at SSE-connect time. A background ticker advances the demo
state under a mutex, re-renders three islands from the snapshot, and
calls `host.Islands.PushUpdate(IslandUpdate{IslandID, HTML}, sessionID)`
for every session on the topic. The runtime's `sse` module reflects each
"island" SSE event into the matching `[data-island="<id>"]` region,
swapping its `innerHTML`. No client-side data fetch, no full reload, no
re-render of unrelated page state.

```go
mgr.OnPresenceChange = func(topic string) {
    rosterHTML := string(renderRoster(mgr.PresenceRoster(topic)))
    for _, sid := range mgr.PresenceSessions(topic) {
        mgr.PushUpdate(island.IslandUpdate{
            IslandID: "roster-" + topic,
            HTML:     rosterHTML,
        }, sid)
    }
}
```

The dashboard's ticker uses the identical shape — it just fires on a
timer instead of on a roster change.

## Update scheduling and DOM discipline

This section is documentation, not a thing to build. The runtime already
implements every discipline below per-feature; reach for the same
patterns when an island you own needs them.

### Coalesce burst updates into one rAF flush

A signal change that mutates the DOM more than once per frame is wasted
work. The runtime's rAF-throttled modules (`backtotop`, `popover`,
`scrollspy`, `toc`, `dropdown`) all use the same shape: a boolean
`_rafPending` gate, a single `requestAnimationFrame` callback that walks
the pending set, then clears the gate. The pattern in `popover.js` is
the cleanest example — capture-phase scroll listener, one `place()` per
frame even on a furious wheel-spin.

```js
let rafPending = false;
scrollEl.addEventListener('scroll', () => {
  if (rafPending) return;
  rafPending = true;
  requestAnimationFrame(() => { rafPending = false; place(); });
}, { capture: true, passive: true });
```

For island pushes, the runtime already batches per SSE frame — one
`el.innerHTML = html` per island event. If your island handler emits
multiple signal updates that touch the same region, fold them into one
re-render on the server side and push once.

### Latest-value for metrics, bounded append for feeds

These are two different update shapes and they must not share a region:

- **Latest-state metric** (throughput, queue depth). The new value
  replaces the old. Intermediate frames may drop without harming the
  user — a future tick heals a missed one. This is the default SSE
  contract (`?buffer=N` with oldest-drop).
- **Bounded append feed** (activity log, audit trail). New entries
  prepend/append; old entries trim. The server trims to the N most
  recent before re-render — never the client. The dashboard caps the
  feed at eight entries on the server (`liveDashFeedCap`) and ships a
  Timeline that fits in one viewport without scrolling.

A feed that must not lose entries is not an SSE feed — see the decision
table below.

### Keyed row updates without rebuilding unrelated regions

A `data-island` slot swap replaces the slot's `innerHTML` wholesale.
That is the entire update granularity: the slot, not the row. To make
swaps cheap and DOM-stable:

- Give each row a stable `id` (`DataTable`'s `Row.ID`) so the browser's
  HTML parser reuses nodes when the new HTML is byte-similar to the old.
- Keep the slot boundary narrow. The dashboard pushes three independent
  islands (stats, feed, jobs) so a metric tick does not re-encode the
  jobs table. One monolithic island would re-render everything per tick.
- Do not push unrelated chrome. The PageHeader, the operator console,
  and the "How this is wired" prose are SSR-only — they never appear in
  a `PushUpdate` payload.

### Separate high-frequency visuals from polite aria-live

This is the most common composition mistake. Numeric StatCards that
update multiple times per second are visually rich and aurally hostile:
an `aria-live` region announces every change, flooding screen readers.

The rule:

- High-frequency numbers → NOT `aria-live`. They are background visuals.
- Activity feed, toasts, status transitions → `role="status"`
  (implicit `aria-live=polite`) or `role="alert"` for urgent
  transitions.
- The dashboard's StatCard grid has no ARIA live attribute on purpose;
  the Timeline feed carries the polite lane.

### Pause work for hidden and background tabs

A tab moved to the background is throttled by the browser — rAF callbacks
slow to ~1Hz, IntersectionObservers stop firing, smooth scrolling
freezes. Background work that ignores this wastes battery and CPU.

The carousel module is the canonical example: it listens for
`visibilitychange` and stops its autorotate timer when
`document.visibilityState === "hidden"`, restarting on return. SSE
itself stays connected in the background (the OS keeps the network
socket alive), but you should not spin rAF animations or poll loops
against a hidden tab — gate them on `visibilitychange`.

### Dispose subscriptions on navigation

The runtime's `animate` and `computed` modules subscribe element closures
into `G._signals[name].listeners` on wire. The same modules splice those
closures back out on `gofastr:navigate` once the element is detached —
without this, every SPA page swap would leak closures (and the detached
DOM nodes they close over) for the lifetime of the session.

The lesson for app-owned islands: if you add a `MutationObserver`, an
`IntersectionObserver`, or a manual `addEventListener` to a region that
can be navigated away from, disconnect it on `gofastr:navigate`. The
existing modules (`toc`, `scrollspy`, `networkretrybanner`) show the
shape: track active observers in a `Set`, walk it on navigate, disconnect
each, then re-scan after the new DOM settles.

## Delivery semantics

The delivery lane you choose is the contract you are promising. **SSE
is not a durable ledger.** It is best-effort push for "the server's
current state changed; here is the new view."

### Pull (poll) vs push (SSE)

Pick the lane by who needs to know *when*:

- **Poll when the dashboard shows the server's current value on a
  cadence.** `data-fui-poll="5s" data-fui-poll-src="/islands/stats"`
  re-fetches the region on the interval; any replica answers from the
  DB. No fanout, no held connection, no per-session push channel. This
  is the recommended tier for a status pill, a counter, a queue-depth
  gauge, or any surface where a few seconds of staleness is fine and
  the user did not ask for the update.
- **Push with SSE when the server needs to initiate the update or the
  cadence is sub-second.** Presence rosters and collaborative surfaces
  need the connection itself — either because the connection IS part
  of the truth (presence) or because polling at the cadence the
  product needs would hammer the server. SSE costs a held connection
  per page and (across replicas) a fanout.

The demo on this page uses SSE because the ticker fires faster than a
sensible poll and the doc needs to show the push lane. A real
dashboard that ticks once every 5–30 seconds should poll. See
[Reactivity model](reactivity.md) for the full ladder.

### Decision table

| Job | Source of truth | Lane | Reconnect behavior | Duplicate handling |
|---|---|---|---|---|
| Latest-state dashboard (throughput, gauge, badge count) | Server's current value | SSE with default `?buffer=N` (oldest-drop) | Refetch current island HTML; intermediate frames are gone. A future frame heals the miss. | Idempotent — same value applied twice is a no-op. |
| Event feed / status timeline | Durable rows in your DB (the feed is a projection) | SSE for invalidation; the rows are source of truth | Authoritative refresh: refetch the rendered feed island, OR refetch the page slice and re-render. The runtime does not replay missed frames. | Idempotent if your feed renderer dedups by event id. Plan for duplicates anyway — at-least-once under fanout. |
| Every-event-matters (billing, audit, workflow transition, ordered processing) | **Durable queue or transactional outbox** | SSE NOT used for delivery. SSE may be used to *invalidate* a view ("an event you care about arrived"), but the durable lane owns the side effect. | The outbox relay redelivers undelivered rows on restart. SSE reconnect changes nothing about durable delivery. | Consumers must be idempotent, keyed on `(consumer, Event.ID)`. |

If the question is "did this exact event reach the browser?", the
answer is "do not use SSE to answer that." Use RPC for the initiating
request's confirmation and a durable queue for any side effect.

### Default oldest-drop versus `?slow=block`

The SSE broker gives each subscriber a bounded buffer. When a subscriber
cannot keep up, the default is **oldest-drop**: the oldest queued frame
is dropped so the subscriber always sees the most recent state. This is
correct for dashboards (a stale gauge is worse than a missed frame) and
presence rosters (a stale roster is a bug).

`?slow=block` (or the `X-SSE-Slow: block` header) opts into the opposite:
the broker blocks the emitter until buffer space is available. Use it
only when a slow subscriber is allowed to backpressure the emitter —
never for fanout from a hot path. The default keeps emitters non-blocking.

### Fanout across replicas

By default the SSE bus and the island manager are per-process: a push
on replica A reaches only sessions connected to A. Attaching a fanout
(`framework.WithFanout`) bridges the real-time lane across replicas, so
an SSE subscriber on B receives a push emitted on A. The fanout is
**also lossy** — it is a broadcast, not a log. A message published while
a replica's listener is reconnecting is gone.

Under fanout, every `On`/`Subscribe` handler fires on **every** replica
for every event. That is right for UI push and wrong for side effects.
A handler that emits a new event in response must gate on
`event.IsRemote(ctx)` to avoid every replica deriving its own copy. Side
effects belong on the durable lane (`framework.WithOutboxConsumer`).
See [Events and SSE](events.md) for the full semantics.

### Reconnect, duplicates, sequence tokens

SSE reconnect is built into the runtime's `sse` module: on transport
error it closes, waits 3s, and reconnects. `window.__gofastr.sseStatus`
mutates in place (`{connected, lastEventAt, retryCount}`) and a
`gofastr:sse-status` CustomEvent fires on every transition. The
connection-health banner listens for the event and re-probes its health
endpoint on reconnect.

What SSE reconnect **does not** do:

- Replay missed frames. The new connection sees only frames emitted
  after it opens.
- Refetch island HTML. Your islands show whatever the last successful
  push left there. If you want them to reconcile, add a listener:

  ```js
  document.addEventListener('gofastr:sse-status', (ev) => {
    if (!ev.detail.connected) return;
    // The stream just reconnected — refetch the current island HTML.
    fetch("/__site/livedash/refresh?island=stats")
      .then(r => r.text())
      .then(html => {
        const el = document.querySelector('[data-island="livedash-stats"]');
        if (el) el.innerHTML = html;
      });
  });
  ```

  The `/__site/livedash/refresh` endpoint is exactly this reconcile
  surface; the dashboard ships it but does not wire the listener, so the
  doc can show it in isolation.

- Deduplicate. If the emitter pushed the same value twice and the broker
  delivered both, the runtime applies both. Idempotency is the island
  renderer's job — and the easiest idempotency is "render the latest
  state," which is what the dashboard does.

For an every-event-matters surface, do not try to bolt sequence numbers
onto SSE. Use the transactional outbox: each row has a stable id,
consumers deduplicate on `(consumer, Event.ID)`, and the relay guarantees
at-least-once delivery. SSE's role shrinks to "wake the view; reconcile
from the durable source."

### Stale-update rejection

When a durable feed (entity events, queue transitions) drives a
dashboard, a late-arriving event from a previous window can overwrite a
newer view. Two patterns handle this:

1. **Version tokens.** A sortable column on the source row
   (`created_at`, `version`, `id`) ships in the SSE payload. The
   renderer keeps the highest-seen token per row and drops frames whose
   token is older. This is what `sortablelist`'s `data-fui-sortable-version`
   + 409 reconciliation path does for reorders.
2. **Authoritative refetch.** On any ambiguous signal (reconnect, a
   duplicate, an out-of-order id), refetch the rendered island HTML from
   the source of truth and replace the slot wholesale. Simpler than
   in-client dedup, at the cost of one request.

The dashboard uses pattern 2 (the refresh endpoint). Pattern 1 is the
right shape when the renderer must converge without a round-trip.

## Tenant isolation

The demo uses ONE fixed topic (`live-dashboard-demo`) because the demo
has no authenticated identity — every viewer is meant to see the same
metric ticks. **A fixed topic is wrong for a multi-tenant app.** Two
different tenants' users would join the same topic and `pushAll` would
broadcast identical HTML to all of them — no isolation, *regardless of
any `AuthorizeTopic` gate you add*. `Manager.AuthorizeTopic` only checks
*whether* a session may join a topic; it does not invent a per-tenant
topic. The gate is correct, but it must gate a topic that is **already
tenant-qualified**.

The right shape for a multi-tenant dashboard:

1. **Derive the topic server-side from the authenticated identity.**
   Never trust a client-supplied topic. Read the tenant id from the
   request-context user and build the topic from it:

   ```go
   func dashTopicFor(ctx context.Context) (string, error) {
       tenantID, err := identity.TenantFrom(ctx)
       if err != nil {
           return "", err
       }
       return "tenant:" + tenantID + ":dashboard", nil
   }
   ```

   The page link then carries the derived topic:
   `/examples/live-dashboard?presence=tenant:<serverID>:dashboard`.
   `handlePage` threads the `?presence=` value into the SSE `<meta>`
   tag exactly as the demo does — the wiring does not change, only the
   topic value does.

2. **Authorize the requested topic against the identity.**
   `Manager.AuthorizeTopic` verifies the session may join the topic it
   asked for — i.e. the identity actually belongs to that tenant. A
   client that asks for `?presence=tenant:someone-else:dashboard` is
   refused at SSE-connect time:

   ```go
   host.Islands.AuthorizeTopic = func(ctx context.Context, topic string) bool {
       want, err := dashTopicFor(ctx)
       if err != nil {
           return false
       }
       return topic == want
   }
   ```

   Rejected topics are dropped silently — the client is simply never
   subscribed, so an unauthorized viewer can't distinguish a forbidden
   topic from a nonexistent one.

3. **Render per-tenant state.** The snapshot must come from the
   tenant-scoped source of truth, not the package-global demo state.
   Each tick iterates the tenants with active viewers and pushes only
   that tenant's snapshot to its sessions:

   ```go
   for _, topic := range host.Islands.ActiveTopics() {
       tenantID, ok := tenantFromTopic(topic) // "tenant:<id>:dashboard" → "<id>"
       if !ok { continue }
       snap := repo.Snapshot(ctx, tenantID)
       pushTenantIslands(host.Islands, topic, snap)
   }
   ```

   `PresenceSessions(topic)` already returns only sessions that joined
   the topic, so a push to `tenant:X:dashboard` reaches only tenant X's
   viewers — no cross-tenant bleed.

4. **Apply the same derivation on the refresh endpoint.**
   `/__site/livedash/refresh` must re-derive the tenant from the
   authenticated request and render that tenant's snapshot. Never let
   an `?island=` or `?tenant=` query param select state — otherwise a
   dropped SSE frame lets one tenant reconcile another tenant's view
   by hand-crafting the URL. The `?island=` param may select *which*
   island to render, but *whose* data feeds it is identity-derived.

The demo intentionally skips all four steps because it is single-tenant
by design. Copying the demo's fixed topic into an authed app is the
most common isolation bug — `AuthorizeTopic` alone does not fix it.

## Performance evidence

The numbers below are measured by the existing framework benchmarks —
not new claims. Re-run with `go test -run=^$ -bench='BenchmarkSSE|BenchmarkEventBus' -benchmem ./framework/`.

| Benchmark | What it measures | Result (v0.26.0) |
|---|---|---|
| `BenchmarkSSE_BackpressureDropRate` | Drop rate under a slow subscriber paired with a fast emitter through the production `core/stream.SSEBroker` (`?buffer=128`) | 5000 fast-published events → 130 delivered, 4870 dropped (drop rate 0.974). The intended contract: bounded, non-blocking, latest-state retention. |
| `BenchmarkSSEWriter_Write` | Per-frame encode + write cost through the production SSE writer | The per-event cost the EventStream handler pays on every outgoing frame. |
| `BenchmarkT9_IslandRPC_Concurrency` | Island RPC tail latency at worker=64 concurrency | p50 12µs, p90 37µs, p99 **5.2ms**, p999 14ms — p99 below the 10ms target. |
| `BenchmarkT9_UIHostPageRender` | UI-host full-page SSR cost | `/` at 3.7ms / 59k allocs (14.8KB response) — the per-render ceiling for a complex page. |

What this tells you about a dashboard:

- A dashboard that pushes one island per tick at 1–10Hz is comfortably
  inside the broker's budget. Single-replica fanout to a few hundred
  sessions is not a measured ceiling; it depends on the broker's buffer
  and your payload size, not on framework overhead.
- Drop rate spikes only when a subscriber cannot drain faster than the
  emitter pushes — which is the **intended** backpressure path. If a
  subscriber reports dropped frames, that subscriber is the bottleneck;
  either reduce the push rate, narrow the payload, or accept that the
  subscriber sees latest-state rather than every frame.
- Page SSR cost dominates first paint, not the live updates. A dashboard
  that takes 4ms to SSR will feel instant; one that takes 40ms has a
  render cost problem, not a delivery cost problem.

Unmeasured: browser hydration, layout cost of `innerHTML` swaps, and
the actual fanout ceiling for your payload size across your topology.
Treat those as unmeasured — profile them in your own app before
promising a number.

## Wiring checklist

1. **Pick the lane per region.** Metrics → SSE latest-state. Feed → SSE
   for invalidation, durable source for truth. Side effects → outbox.
2. **Pick the slot boundary.** One island per region that changes
   independently; do not bundle unrelated state into one push.
3. **Pick the aria-live policy.** Polite for feeds and status
   transitions; nothing for high-frequency numbers.
4. **Pick the reconnect policy.** Either accept "latest-state wins"
   (default) or wire a `gofastr:sse-status` listener that refetches the
   island HTML on reconnect.
5. **Pick the scope.** Use `PresenceSessions(topic)` for push targets.
   For multi-tenant apps, derive a tenant-qualified topic from the
   authenticated identity and gate it with `Manager.AuthorizeTopic`
   (see [Tenant isolation](#tenant-isolation) — a fixed topic is wrong
   for multi-tenant, and `AuthorizeTopic` alone does not fix it).
6. **Stop the ticker on shutdown.** `app.OnStop` cancels the ticker
   context so SIGTERM drains cleanly.

## See also

- [UI capability map](ui-capability-map.md) places live dashboards in
  the wider state-and-delivery model.
- [Signal store](signal-store.md) for `store.Computed` and the typed
  projection primitive the status pill uses.
- [Events and SSE](events.md) for the broker's backpressure modes,
  fanout semantics, and the transactional outbox.
- [Presence](presence.md) — the same push lane used for "who's here"
  rosters. The dashboard reuses `PresenceSessions(topic)` as its
  delivery target list.
- [Interactive patterns](interactive-patterns.md) for `data-fui-signal-inc`
  and the rest of the in-page mutation vocabulary.
- [Horizontal scaling](scaling.md) for the multi-replica delivery story.
- [Benchmarks](benchmarks.md) and [Performance results](perf-results.md)
  for the measured limits cited above.

## Common mistakes

- **Treating SSE as a durable log.** It is best-effort push. Use the
  transactional outbox for any side effect that must not be lost.
- **Putting high-frequency numbers in an `aria-live` region.** They
  flood assistive tech. The polite lane is for feeds and status
  transitions, not gauges.
- **One monolithic island.** Pushing the whole dashboard body per tick
  re-renders unrelated regions. Split into per-region islands so a
  metric tick does not re-encode the jobs table.
- **Buffering the feed client-side.** Trim server-side, before
  re-render. The wire payload stays small and the client never has to
  reason about eviction policy.
- **Pushing to every session.** Address pushes to
  `PresenceSessions(topic)` so only viewers who joined the topic
  receive them. **A fixed topic is wrong for multi-tenant apps** —
  derive a tenant-qualified topic from the authenticated identity and
  gate it with `Manager.AuthorizeTopic` (see [Tenant isolation](#tenant-isolation)).
- **SPA navigation won't join the topic.** The SSE topic is read from
  the page's `<meta name="gofastr-sse">` on initial load; partial-nav
  (an in-site link click) does NOT re-thread the topic, so a user who
  arrives at the dashboard via SPA nav sees the SSR paint but receives
  no live updates, and leaving the page does not unsubscribe them.
  Full-load the dashboard URL (or re-open the SSE connection) so the
  topic join fires. The command-palette entry for the dashboard uses
  normal SPA nav and so exhibits this — open the page directly to see
  live updates. See [Presence](presence.md) — the same limitation
  applies to the roster demo ("Re-threading presence on SPA navigation").
- **Stopping the ticker on shutdown.** A goroutine that pushes into a
  closed manager on SIGTERM is a leak. Register it with `app.OnStop`.
