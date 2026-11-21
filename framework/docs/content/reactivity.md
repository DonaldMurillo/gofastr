# Reactivity model

GoFastr gives you four ways to update a page after first paint, ordered from cheapest to heaviest. Pick the lowest rung that fits the job; move up only when the lower rung can't do it.

| Rung | Mechanism | Best for |
|---|---|---|
| **1. Client signals** | Browser signal store (`data-cui-signal-*`). Zero network. | UI-only state: tabs, toggles, theme, open/closed panels. |
| **2. Island RPC** | `fetch` to a server handler; swaps HTML or sets a signal. | Every user-triggered data action (forms, filters, sorts, CRUD). |
| **3. Polling** | Interval GET (`data-cui-poll` or `Builder.Poll`). No held socket. | Passive freshness: counters, job status, dashboards. |
| **4. SSE push** | Single `/__gofastr/sse` stream per page. | Presence, collaboration, and sub-second live updates. |

## 1. Client signals

Signals hold lightweight UI state in the browser with no server round-trip:

| Attribute | Effect |
|---|---|
| `data-cui-signal-set="name:value"` | Sets `name` to `value` |
| `data-cui-signal-inc="name[:delta]"` | Increments `name` by `delta` (default `1`) |
| `data-cui-signal-toggle="name"` | Flips `name` between `"true"` and `"false"` |

Any element carrying `data-cui-signal="<name>"` updates automatically when that signal changes. For typed, namespaced, SSR-seeded state shared across components, use `core-ui/store` ([Signal store](signal-store.md)).

## 2. RPC

A click or form submit fires an HTTP request to your Go handler. The handler reads or mutates the DB and returns authoritative HTML (to swap an island) or a value (to update a signal):

- **Stateless & authoritative:** any replica can serve any RPC, and the HTTP response is the source of truth for validation, errors, and optimistic rollback.
- **Screen-policy enforced:** compiled server actions (`/__gofastr/action`) run the screen's policy chain before executing, and JSON bodies reject duplicate/case-folded keys. Per-record authorization still belongs in your handler.
- **Screen cache invalidation:** the client router caches up to 20 visited screens per tab for instant Back/Forward navigation. Non-`GET` RPCs automatically evict the current screen; call `ui.InvalidateScreens(w, "/orders")` when a mutation also stales *other* cached routes.

See [Interactive patterns](interactive-patterns.md) and [Optimistic UI](optimistic-ui.md) for the full attribute vocabulary and mutation patterns.

## 3. Polling

Polling refreshes passive UI without holding an open connection or needing cross-replica fanout—any replica answers directly from the database.

**Page region:**

```html
<div data-cui-poll="30s" data-cui-poll-src="/islands/orders/today">
  …initial SSR content…
</div>
```

- `data-cui-poll` takes a Go duration (minimum `5s`) with automatic jitter.
- Identical HTML responses skip DOM writes so focus and scroll stay untouched; swaps pause while a user is typing or navigating inside the region, and restore focus by `id` or `href` afterward.
- Polls pause automatically when off-screen or when the browser tab is hidden, and back off on errors.
- **Stop on completion:** return header `X-Gofastr-Poll-Stop: 1` (or remove the `data-cui-poll` marker) when a job reaches a terminal state.

**Widget level:** call `Builder.Poll(interval)` (and optionally `Builder.PollTerminal(fn)`) to refresh a widget's `/state` signals on a cadence:

```go
panel := preset.FloatingPanel("ops-panel").
	Slot("body", bodyComponent).
	Signal("queue_depth", widget.SignalFunc(readQueueDepth)).
	Poll(15 * time.Second).
	Build()
```

## 4. SSE push

All server-pushed page updates share one `/__gofastr/sse` connection per page. Use SSE only when the connection itself is part of the product (presence rosters, collaboration) or updates must arrive faster than a poll interval:

- **Push-only & best-effort:** never deliver the answer to a user's own click over SSE (that belongs in the RPC response). Slow consumers drop stale frames; durable side effects belong in `battery/queue` or the transactional outbox.
- **Multi-replica:** enable `framework.WithFanout` so pushes on one replica reach sockets on another.
- **Long-lived & bounded:** streams outlive the request timeout once flushed, send keepalive heartbeats (default `15s`, `island.WithSSEHeartbeat`), and reclaim stranded streams after a bounded lifetime (default `5m`, `island.WithSSEStreamBound`).
- **Connection caps:** default caps are 16/session, 256/client, and 4096/replica (`island.WithStreamCaps`, `island.WithPeerStreamCap`), rejecting excess connects with `429` + `Retry-After`. Behind a reverse proxy, set `AppConfig.TrustedProxies` (or `uihost.WithTrustedProxies`) so clients aren't grouped under the proxy IP.

See [Events and SSE](events.md), [Presence](presence.md), and [Live dashboards](live-dashboards.md).

## WebSockets (custom bidirectional protocols)

For bidirectional protocols outside the page model (multiplayer state, WebRTC signaling), use `core/stream` directly—or `battery/rtc` for WebRTC rooms ([WebRTC rooms](rtc.md)).

When you own a WebSocket, handle reconnects cleanly with two primitives:

- **Sequenced snapshots (`stream.StateChannel` + `__gofastr.createSequencedReducer`):** send one consistent state snapshot on connect followed by strictly increasing event sequences so late messages never resurrect stale state.
- **Reconnect generations (`__gofastr.connectWebSocket`):** track each reconnect as a fresh generation (`onGenerationStart`, `onHydrated`, `onGenerationEnd`) so higher-level state (like WebRTC peer connections or dedup caches) resynchronizes explicitly instead of assuming the socket coming back fixed everything.

## Statelessness & sessions

Durable state lives in your database; UI projections live in the browser signal store—never in server RAM tied to a connection.

- **Signed transport sessions:** `uihost` uses HMAC-signed tokens rather than server-side session records, so sticky routing is never required.
- **Single replica:** zero config—an ephemeral boot secret is minted automatically, and open tabs transparently roll over to the new token after a deploy (without logging users out of `battery/auth`).
- **Multiple replicas:** set a shared secret via `framework.WithSecret` or `GOFASTR_SECRET`. Enabling `WithFanout` without a configured secret fails loudly at boot so tokens never silently mismatch across replicas.

## Which rung should I pick?

1. **Purely visual / local toggle?** → Client signal.
2. **User clicked or submitted something?** → Island RPC.
3. **Passive freshness (dashboard, status, counter)?** → Polling.
4. **Live presence or sub-second push to other users?** → SSE push.

## Common mistakes

- **Jumping to SSE for ordinary dashboards.** A counter or status card that refreshes every few seconds is a poll, not an SSE stream.
- **Answering a user's own click over SSE.** The initiating RPC response must return the mutation's result; SSE is for background or other-user updates.
- **Scaling to two replicas without `GOFASTR_SECRET`.** Set a shared secret before adding a second replica so transport tokens verify everywhere.
- **Storing widget or island state in process memory.** Reconstruct from URL, session, and DB on each request so any replica can answer.
- **Pointing `data-cui-poll-src` at a mutating handler.** Polls fire repeatedly on every open tab—keep polled endpoints read-only.
- **Assuming a reconnected WebSocket restored protocol state.** Resynchronize state explicitly per reconnect generation using `stream.StateChannel` and the `ws` module hooks.
