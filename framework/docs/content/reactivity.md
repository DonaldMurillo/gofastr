# Reactivity model

GoFastr ships four ways to make a page change after first paint. They form a
ladder, cheapest first. Pick the lowest rung that fits the product behavior;
reach for the next one only when the lower one cannot do the job.

| Rung | What it is | When it fits |
|---|---|---|
| **Client signal** | A string value held in the runtime signal store, mutated by `data-fui-signal-set/inc/toggle`. No server. | UI-only state. Tabs, toggles, theme, an open/closed panel. |
| **RPC** | A `fetch` to a server handler. The server reads from the DB and returns HTML or a value; the runtime swaps the region or sets a signal. | Anything the user triggered. The default for data. |
| **Polling** | The runtime re-fetches a region on an interval. | Passive freshness — a counter, a status, a dashboard — without holding a connection. |
| **SSE push** | One long-lived `/__gofastr/sse` connection per page. The server pushes when state changes. | Semantics that need the connection: presence, collaborative editing, sub-second internal dashboards. |

## The four rungs, in order

### 1. Client signals

A signal is a named string kept in the runtime signal store in the browser.
Three click-time primitives mutate it:

| Attribute | Effect |
|---|---|
| `data-fui-signal-set="name:value"` | Sets `name` to `value` |
| `data-fui-signal-inc="name[:delta]"` | Increments `name` by `delta` (default 1; negative decrements) |
| `data-fui-signal-toggle="name"` | Flips `name` between `"true"` and `"false"` |

Any element with `data-fui-signal="<name>"` re-renders when the value changes.
For typed, shared, SSR-seeded state, wrap signals with `core-ui/store` (see
[Signal store](signal-store.md)).

No server involvement. No RPC. No page reload.

### 2. RPC

A user click or submit fires a `fetch` to a server handler. The handler reads
from the DB, renders fresh HTML (or returns a value), and the runtime swaps the
region or writes the value into a signal.

- Stateless. Any replica can answer — there is no in-process session object
  tied to the connection.
- Authoritative. The HTTP response is the source of truth for the mutation;
  the runtime reconciles against it.
- Covers every user-initiated data change: paginate, filter, create, update,
  delete, optimistic toggles with rollback.

See [Interactive patterns](interactive-patterns.md) for the full attribute
vocabulary and [Optimistic UI](optimistic-ui.md) for the mutation lifecycle.

### 3. Polling

Polling re-fetches on an interval without holding a connection. It gives you
freshness for passive surfaces — a status pill, a counter, a dashboard — with
no fanout, no shared infrastructure, and no held connection.

**Page level.** Add two attributes to the region you want to refresh:

```html
<div data-fui-poll="30s" data-fui-poll-src="/islands/orders/today">
  …initial SSR content…
</div>
```

- `data-fui-poll` is a Go duration string (`30s`, `5m`, `1h`). Five seconds is
  the floor.
- `data-fui-poll-src` is the URL the runtime fetches. The response body
  replaces the region's `innerHTML`, exactly like an RPC signal swap.
- The interval is jittered so a fleet of tabs does not synchronize on a wall
  clock.
- The poller pauses while the tab is hidden and resumes when it returns.
- A failed fetch doubles the interval (capped at 5x the base, reset on the
  next success); the runtime keeps polling rather than
  spinning.

**Widget level.** A widget that wants to refresh its own signals on a cadence
calls `Builder.Poll(interval)`:

```go
panel := preset.FloatingPanel("ops-panel").
    Slot("body", bodyComponent).
    Signal("queue_depth", widget.SignalFunc(readQueueDepth)).
    Poll(15 * time.Second).
    Build()
```

On each interval the runtime re-fetches the widget's `/state` endpoint and
re-applies the signals that changed. No SSE binding, no event name, no
per-widget push channel.

**Why prefer polling.** Any replica can answer the fetch from the DB. You do
not need `WithFanout`, you do not need sticky routing, and the page works the
same way on a single replica as it does behind a load balancer. For dashboards,
counters, and statuses this is the recommended tier.

### 4. SSE push

The single `/__gofastr/sse` bus. One long-lived connection per page; the server
pushes frames when state changes.

SSE earns its cost when the product semantics need the connection — when the
fact "this client is connected" is itself part of the truth (presence, the
collaborative roster), or when updates land faster than a poll cycle could
sensibly reflect (sub-second internal dashboards).

- Best-effort, not durable. A slow consumer drops old frames; a reconnect
  does not replay them. Side effects that must not be lost go on the
  transactional outbox or `battery/queue`, never the SSE lane.
- Multi-replica needs `WithFanout` so a push emitted on one replica reaches
  subscribers connected to another. Without it, push stays per-process.
- Push only. The result of a user action arrives in the RPC response, never
  on the event stream.

See [Events and SSE](events.md) for the broker contract, [Presence](presence.md)
for the canonical push case, and [Live dashboards](live-dashboards.md) for the
decision between SSE push and polling.

## Which rung fits

Three questions, in order:

1. **Did the user trigger it?** Use an RPC. The server renders from the DB and
   the runtime swaps the region or sets a signal.
2. **Is it passive freshness — the server's current value, on a cadence, with
   no held connection?** Poll. Any replica serves the fetch.
3. **Does the product need to know who is connected right now, or push under a
   second?** Use SSE. Wire `WithFanout` if you run more than one replica.

A fourth rule, kept negative: **never** deliver the result of a user action
over SSE. SSE is push for *other* clients and for background changes. The
action's own response comes back on the RPC that triggered it.

## Statelessness contract

State lives in two places: the database (durable truth) and the client signal
store (UI state). It does not live in server RAM tied to a connection.

The interactive layer is stateless. Any replica can serve any RPC, any poll
fetch, and any SSE subscribe — the request reads from shared state (the DB, or
a shared store like Redis) and renders the response. There is no in-process
widget or island object the next request depends on.

Practical consequences:

- A poll fetched on replica B sees the same data a push from replica A would
  have shown.
- An RPC that lands on the replica the user has never talked to still renders
  the right thing, because it reconstructs from the URL, the session, and the
  DB.
- The only multi-replica requirement for push is `WithFanout`, which bridges
  the SSE lane; the state itself was never per-replica.

If a surface cannot be reconstructed from shared state on every request, that
is a design smell, not a topology choice. Redesign the handler to reconstruct,
or move the state into the DB.

## Sessions

The uihost session is an HMAC-signed token, not a server-side record. A token
issued by one replica verifies on every other replica that shares the secret.

- **One replica, no secret configured.** The framework mints an ephemeral boot
  secret at startup. Sessions work, but they roll over on restart because the
  next boot mints a new one. Zero config; the trade-off is that after a deploy
  each open page transparently re-mints its UI session (this is the
  SSE/interaction transport session, not `battery/auth` login state — nobody is
  logged out). Recovery needs no user action: a page re-mints on its next render
  or navigation, and a purely idle tab re-mints from the SSE module itself
  (`POST /__gofastr/session`) once its stream reconnect starts failing.
- **Multi-replica.** Set a shared secret so every replica verifies the same
  tokens. Two ways to set it:
  - `framework.WithSecret(secret)` in code, or
  - the `GOFASTR_SECRET` environment variable.
- **Fanout without a configured secret fails at boot.** A multi-replica
  deployment that wired `WithFanout` but forgot the secret refuses to start,
  with an error naming both. This is deliberate — silent token mismatch in
  production is worse than a loud boot failure.

Sticky sessions are not part of the contract. A token is portable; route the
request to whichever replica the load balancer picks.

## See also

- [Interactive patterns](interactive-patterns.md) — the full `data-fui-*`
  vocabulary, including the RPC and signal primitives summarized above.
- [Widgets](widgets.md) — `Builder.Poll` and the widget builder.
- [Presence](presence.md) — the canonical SSE push case.
- [Live dashboards](live-dashboards.md) — choosing between polling and SSE for
  a live surface.
- [Events and SSE](events.md) — the broker contract and the durable outbox.
- [Scaling](scaling.md) — multi-replica delivery, `WithFanout`, and the
  session-token checklist.
- [Runtime contract](runtime-contract.md) — the SSR / hydration / island / SSE
  boundary and the `data-fui-*` attribute reference.

## Common mistakes

- **Jumping to SSE for a dashboard.** A counter or status pill that ticks
  every few seconds is a poll, not a push. SSE is the right answer when the
  connection itself is part of the truth or the cadence is sub-second.
- **Delivering a user action over SSE.** SSE is push for *other* clients. The
  result of the click the user just made comes back on the RPC response.
- **Forgetting `GOFASTR_SECRET` before adding a second replica.** Sessions
  issued by one replica stop verifying on the other. The fanout-without-secret
  boot failure catches this for push; set the secret anyway, before you scale.
- **Holding widget or island state in process RAM.** A request landing on a
  different replica cannot read it. Move the state to the DB or redesign the
  handler to reconstruct.
- **Polling a path that mutates.** `data-fui-poll-src` should be a read. Polls
  fire on a timer; a poll that writes would write on every tab, on every
  interval, in every browser.
