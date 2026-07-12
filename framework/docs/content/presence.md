# Live Presence

Presence tracks **who is currently connected** to a given topic on the
server, so a page can show a live roster of its viewers — the avatar
stack with green/away dots, the "3 people editing" indicator, the
who's-online sidebar.

This is the **single-replica foundation**: the roster reflects only the
SSE connections on *this* server. Cross-replica roster aggregation
(fanout of connection state across replicas) is tracked separately and
**not** attempted here — a multi-replica deployment would under-count
until that lands.

---

## The security model (read this first)

**Identity is SERVER-DERIVED. Never client-supplied.**

The `{userID, displayName}` in a roster entry come exclusively from the
request context's authenticated user, resolved at SSE-connect time via
`island.PresenceIdentityFromContext(ctx)` — a READ of the
`handler.GetUser(ctx)` value that battery/auth's `SessionMiddleware`
(or JWT `RequireAuth`) seeds. A client may **name a topic** (the
`?presence=` query param — it is untrusted and bounded) but may **never
claim an identity**. There is no `?user=` or `?name=` parameter; any
such value is ignored.

This is the invariant that makes the roster trustworthy: a user cannot
spoof being someone else in the roster. It is proven by
`TestPresenceIdentityServerDerived` and
`TestHandleSSEPresenceAuthedUserRecords` (which sends `?user=attacker`
and verifies the roster still shows the ctx user).

**Anonymous (unauthenticated) connections** are tracked with a
server-derived *pseudo-identity* synthesized from the session id
(`anon:<session>` / `Guest <tail>`), so presence is useful on apps
without auth. The pseudo-identity is deterministic per session — two
tabs of the same browser (same session cookie) share one pseudo-identity
and show as one viewer (correct: same person). Two browsers get two
sessions and two viewers.

---

## How it works

1. A page links with `?presence=<topic>` in its URL.
2. `handlePage` threads that value into the SSE `<meta>` tag, so the
   client's `EventSource` opens
   `/__gofastr/sse?session=X&presence=<topic>`.
3. `handleSSE` resolves the identity from `r.Context()` and calls
   `Manager.ServeSSEWithPresence`, which joins the connection onto the
   topic for the lifetime of the SSE stream.
4. On join/leave the manager fires `OnPresenceChange(topic)`. The host
   wires that callback to re-render the roster island and push it to
   every session on the topic via the existing `PushUpdate` lane — no
   new transport.
5. On disconnect (including the ref-counted last-tab case), the
   connection's contribution is removed and the roster updates — no
   ghost presence.

### Topic bounds (invariant #2)

`?presence=` is client-supplied, so it is bounded by
`island.ParsePresenceTopics`:
- at most **16** topics per connection,
- at most **128** bytes per topic,
- duplicates collapse, empty/oversize entries drop.

The raw value is also length-capped (`maxPresenceParamLen`) before it
enters the HTML `<meta>` tag, and `url.QueryEscape`d to prevent
attribute injection.

---

## API

All in `core-ui/island` (the `Manager` you already use for island
push):

```go
// Resolve the trusted identity from the request context.
// Returns zero (anonymous) when no user is present.
id := island.PresenceIdentityFromContext(r.Context())

// Parse + bound the client-supplied topic list.
topics := island.ParsePresenceTopics(r.URL.Query().Get("presence"))

// Read the roster (deduped by UserID, sorted).
members := mgr.PresenceRoster("doc:42") // []PresenceMember

// The push targets for a live roster update.
sessions := mgr.PresenceSessions("doc:42") // []sessionID

// Live roster push: fire on join/leave. The host wires this.
mgr.OnPresenceChange = func(topic string) {
    html := renderRoster(mgr.PresenceRoster(topic))
    for _, sid := range mgr.PresenceSessions(topic) {
        mgr.PushUpdate(island.IslandUpdate{
            IslandID: "presence-roster-" + topic,
            HTML:     string(html),
        }, sid)
    }
}
```

`PresenceJoin`/`PresenceHandle.Leave` are called automatically by
`ServeSSEWithPresence` — you do not call them directly unless you're
building a custom transport.

### No generic HTTP roster endpoint (by design)

There is deliberately **no** framework endpoint that returns a topic's
roster over HTTP. A roster is "who is viewing topic X" — exposing it on an
ungated URL would leak identities (emails) to anyone who can guess a topic
string. The framework can't know your per-topic authorization, so the
roster stays an **in-process** primitive (`Manager.PresenceRoster`) plus
the live SSE push above. If you want an HTTP roster, build one behind your
own authorization (e.g. "only members of this document may see who's
viewing it").

---

## Wiring a presence page

1. Render a roster **island slot** in your screen:

```go
html.Div(html.DivConfig{
    ExtraAttrs: html.Attrs{"data-island": "presence-roster-" + topic},
    Role:       "status",
    AriaLabel:  "Who's viewing",
}, renderRoster(mgr.PresenceRoster(topic)))
```

2. Wire `OnPresenceChange` (once, at startup) to push the re-rendered
   roster to every session on the topic — see the API example above.

3. Link the page with the topic: `<a href="/my-page?presence=my-topic">`.
   The SSE meta tag picks it up automatically.

That's it — no client-side JavaScript. The existing `sse.js` module
delivers the roster island updates; the runtime swaps the slot's
innerHTML.

---

## Demo

`examples/site` ships a live presence demo at
`/examples/presence?presence=presence-demo` (`screen_presence.go`). It
renders a `ui.AvatarGroup` with `AvatarOnline` status dots.

**To see two viewers:** open that URL in two browsers (or one normal +
one private window — they get distinct sessions). Each appears in the
other's roster within a second; close one and its avatar drops.

Two tabs of the *same* browser share one session and show as a single
viewer — that's correct (same person).

---

## What's deferred (multi-replica)

The roster reflects **this replica's connections only**. On a
multi-replica deployment:

- A viewer connected to replica B won't appear in replica A's roster.
- `PushUpdate` already crosses replicas (via `SetFanout`), so a roster
  *push* reaches a viewer on another replica — but the *roster itself*
  is local, so it would under-count.

Cross-replica roster aggregation (fanout of connection join/leave
events) is future work. Until then, presence is correct and complete on
a single replica, and sticky-session deployments where all of a topic's
viewers land on one replica work fully.

## Common mistakes

- **Trusting a client-supplied name.** Identity is server-derived from the
  request-context user — never from a query param or body. A client may
  name a *topic* but can never claim another user's identity in the roster.
- **Exposing the roster on an ungated URL.** There is deliberately no
  framework HTTP roster endpoint — "who is viewing X" leaks identities.
  Build your own roster route behind your app's per-topic authorization.
- **Assuming cross-replica rosters.** The roster reflects THIS replica's
  connections only. On a multi-replica deployment without sticky sessions,
  each replica sees a partial roster (aggregation is future work).
- **Re-threading presence on SPA navigation.** The SSE topic is set from
  the page's `?presence=` on initial load; a client-side nav to a presence
  page won't re-join the topic. Full-load the presence page (or re-open the
  SSE connection) so the join fires.
