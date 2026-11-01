# Live presence

Presence tracks **who is currently connected** to a given topic on the
server, so a page can show a live roster of its viewers: the avatar
stack with green/away dots, the "3 people editing" indicator, the
who's-online sidebar.

The roster is **aggregated across replicas**: when a `core/fanout.Fanout`
is attached (`framework.WithFanout`, the same seam island push and entity
events use), each replica broadcasts its local roster per topic over a
dedicated presence lane, and `PresenceRoster` returns the merged union.
Without a fanout it degrades to exactly the single-replica roster (no
goroutines, no behavior change). See "Cross-replica aggregation" below.

---

## The security model (read this first)

**Identity is SERVER-DERIVED. Never client-supplied.**

The `{userID, displayName}` in a roster entry come exclusively from the
request context's authenticated user, resolved at SSE-connect time via
`island.PresenceIdentityFromContext(ctx)`: a READ of the
`handler.GetUser(ctx)` value that battery/auth's `SessionMiddleware`
(or JWT `RequireAuth`) seeds. A client may **ask for a topic** (the
`?presence=` query param, untrusted, bounded, and refused unless an
`AuthorizeTopic` hook admits it) but may **never claim an identity**. There is no `?user=` or `?name=` parameter; any
such value is ignored.

This is the invariant that makes the roster trustworthy: a user cannot
spoof being someone else in the roster. It is proven by
`TestPresenceIdentityServerDerived` and
`TestHandleSSEPresenceAuthedUserRecords` (which sends `?user=attacker`
and verifies the roster still shows the ctx user).

**Anonymous (unauthenticated) connections** are tracked with a
server-derived *pseudo-identity* synthesized from the session id
(`anon:<session>` / `Guest <tail>`), so presence is useful on apps
without auth. The pseudo-identity is deterministic per session: two
tabs of the same browser (same session cookie) share one pseudo-identity
and show as one viewer (correct: same person). Two browsers get two
sessions and two viewers.

---

## How it works

1. The screen declares its topics by implementing
   `uihost.PresenceScreen`:

   ```go
   func (s *DocScreen) PresenceTopics(ctx context.Context) []string {
       return []string{"doc:" + s.doc.ID}
   }
   ```

   The host asks the loaded instance, after `SetParams` and `Load`, so a
   detail screen can name the record it shows.
2. `handlePage` signs the list into the SSE `<meta>` tag for the viewer it
   rendered the page for, so the client's `EventSource` opens
   `/__gofastr/sse?session=X&declared=<topics>&dsig=<signature>`. The
   signature is an HMAC under a key derived from the session key. It
   covers the topics, the viewer's user id and an expiry one session
   lifetime out, and it does not name the stream id, so the runtime's
   in-place session re-mint keeps it. Under a `RenderAlt` decision the
   page drawn is the alternate, so the guarded screen's topics never ride
   it, and a page that answers anything but 2xx (a Load that set 404 for
   another tenant's record) signs none.
3. `handleSSE` checks the signature (against the current session key and
   any rotation keys), resolves the identity from `r.Context()` and calls
   `Manager.ServeSSEWithPresence` with the declared topics and any
   `?presence=` requests. The connection joins for the lifetime of the
   SSE stream.
4. On join/leave the manager fires `SetOnPresenceChange`'s callback. The host
   wires that callback to re-render the roster island and push it to
   every session on the topic via the existing `PushUpdate` lane; no
   new transport.
5. On disconnect (including the ref-counted last-tab case), the
   connection's contribution is removed and the roster updates; no
   ghost presence.

### Topic bounds (invariant #2)

Declared and requested topics share one set of bounds
(`island.ParsePresenceTopics`):
- at most **16** topics per connection, the two lists counted together,
- at most **128** bytes per topic,
- duplicates collapse, empty/oversize entries drop. A declared topic
  holding a comma is dropped too, since the list travels comma-separated.

The raw `?presence=` value is also length-capped (`maxPresenceParamLen`)
before it enters the HTML `<meta>` tag, and every value is
`url.QueryEscape`d to prevent attribute injection.

### Topic authorization (invariant #3)

Bounding a topic string is not the same as *authorizing* it. A client may
name **any** topic within the bounds, including one it should not see, and a
roster can contain emails (the display name). So a topic the client names
joins only when an app hook says yes:

- **Declared topics** join with no hook. Rendering the page for this viewer
  was the authorization, and the signature keeps the list from moving to
  another user or growing.
- **Requested topics** (`?presence=`, anything the browser builds) are
  refused when no hook is installed. Apps that build topics in the browser
  install one.

Call `Manager.SetAuthorizeTopic` to admit requested topics:

```go
mgr.SetAuthorizeTopic(func(ctx context.Context, topic string) bool {
    // Public topics: always allow.
    if strings.HasPrefix(topic, "public:") {
        return true
    }
    // Private topics: check the request's authenticated user against the
    // topic's ACL. ctx is the SSE request context; read the trusted user
    // with island.PresenceIdentityFromContext(ctx) or handler.GetUser(ctx).
    return userMayViewTopic(ctx, topic)
})
```

Once installed, the hook answers for **every** topic, declared ones too. It
runs at SSE-connect time, **before** any subscription or roster emission,
and again on each stream heartbeat: an unauthorized topic is dropped, so the
viewer never receives the roster or join/leave events for it, and a viewer
whose access was removed leaves the roster on the next beat. Rejection is
**silent** (the topic is simply not joined): there is no distinguishable
error, so the gate is not a private-topic existence oracle. Keep an
ergonomic public path (e.g. a `public:` prefix) so only genuinely private
topics pay the check.

Without a hook, removing a viewer's access to a declared topic takes effect
when the signature expires (one session lifetime) or when the viewer next
loads the page without that access. Install the hook when access to a
declared topic can change while a page stays open.

---

## API

All in `core-ui/island` (the `Manager` you already use for island
push):

```go
// Resolve the trusted identity from the request context.
// Returns zero (anonymous) when no user is present.
id := island.PresenceIdentityFromContext(r.Context())

// Parse + bound a client-supplied topic list.
requested := island.ParsePresenceTopics(r.URL.Query().Get("presence"))

// A custom transport passes both lists; with no hook only Declared joins.
mgr.ServeSSEWithPresence(w, r, id, island.PresenceTopics{
    Declared:  declared, // topics your server vouches for
    Requested: requested,
})

// Read the roster (deduped by UserID, sorted).
members := mgr.PresenceRoster("doc:42") // []PresenceMember

// The push targets for a live roster update.
sessions := mgr.PresenceSessions("doc:42") // []sessionID

// Live roster push: fire on join/leave. The host wires this.
mgr.SetOnPresenceChange(func(topic string) {
    html := renderRoster(mgr.PresenceRoster(topic))
    for _, sid := range mgr.PresenceSessions(topic) {
        mgr.PushUpdate(island.IslandUpdate{
            IslandID: "presence-roster-" + topic,
            HTML:     string(html),
        }, sid)
    }
})
```

`PresenceJoin`/`PresenceHandle.Leave` are called automatically by
`ServeSSEWithPresence`; you do not call them directly unless you're
building a custom transport.

### No generic HTTP roster endpoint (by design)

There is deliberately **no** framework endpoint that returns a topic's
roster over HTTP. A roster is "who is viewing topic X": exposing it on an
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

2. Wire `SetOnPresenceChange` (once, at startup) to push the re-rendered
   roster to every session on the topic; see the API example above.

3. Declare the topic on the screen (`PresenceTopics`, step 1 of "How it
   works"). The SSE meta tag carries it, signed, automatically.

That's it: no client-side JavaScript. The existing `sse.js` module
delivers the roster island updates; the runtime swaps the slot's
innerHTML.

---

## Demo

`examples/site` ships a live presence demo at
`/examples/presence` (`screen_presence.go`), whose screen declares the
`presence-demo` topic. It renders a `ui.AvatarGroup` with `AvatarOnline`
status dots.

**To see two viewers:** open that URL in two browsers (or one normal +
one private window; they get distinct sessions). Each appears in the
other's roster within a second; close one and its avatar drops.

Two tabs of the *same* browser share one session and show as a single
viewer; that's correct (same person).

---
## Cross-replica aggregation

With a fanout attached, the roster is the **merged union** of every
replica's connections; no under-counting. You get this automatically from
`framework.WithFanout` (it wires the UI host's island manager, which owns
presence); there is no separate presence wiring to do.

**How it converges** (`core-ui/island/presence_fanout.go`):

- Each replica broadcasts its **full local roster** per active topic on a
  dedicated presence lane (`gofastr.presence`), a separate fanout topic
  from island invalidations, both lossy best-effort.
- A remote-roster table keyed by `(replicaID, topic)` holds each peer's
  contribution with a **TTL of ~45s** (3× the 15s heartbeat). `PresenceRoster`
  returns local ∪ live-remote, deduped by UserID exactly like the local
  dedup.
- A **periodic 15s heartbeat** rebroadcasts every topic's full roster, so a
  dropped announcement heals on the next beat. A **crashed replica** stops
  heartbeating and its members vanish from peers within the TTL; no
  explicit goodbye needed. The expiry fires `SetOnPresenceChange` like
  any other departure, so viewers get the shrunk roster pushed. A **graceful stop** (`SetFanout`'s returned
  `stop`, called by app `Shutdown`) stops the heartbeat, waits for a
  roster already being published, then publishes an empty roster as the
  replica's last message, so a rolling restart converges promptly. The stopping replica drops every
  remote member and fires `SetOnPresenceChange` for each topic whose
  roster shrank, so its own viewers see them leave.
- A roster change from a remote merge fires the same `SetOnPresenceChange` callback →
  `PushUpdate` path as a local join, so viewers on every replica see the
  update live (island push already crosses replicas).
- Every path that fires `SetOnPresenceChange` (join, leave, expiry,
  remote merge, graceful stop) recovers a panic in the callback and logs
  it with the topic, then fires the remaining topics. A panicking hook
  costs one topic's push, never the replica.

**Identity safety is unchanged.** Announcements carry only the same
server-derived `{UserID, DisplayName}` the local roster exposes, never a
session id, IP, or anything not already visible in `PresenceRoster` output.
There is still no HTTP roster endpoint.

**No fanout attached** ⇒ the presence lane is a complete no-op: no
goroutines, no remote state, and `PresenceRoster` returns the byte-identical
single-replica result. Attach `framework.WithFanout` when you run a
second replica so rosters merge across processes.

Presence is the canonical case for SSE push: the roster IS who is
currently connected, so the connection itself is part of the truth a
poll cannot reconstruct. See [Reactivity model](reactivity.md) for
where it sits in the ladder.

## See also

- [Live dashboards](live-dashboards.md) reuses `PresenceSessions(topic)` as the delivery-target list for a dashboard's SSE island push.
- [UI capability map](ui-capability-map.md) places collaborative awareness in the wider state and delivery model.
- [Events and SSE](events.md) defines the shared best-effort push lane.
- [Horizontal scaling](scaling.md) covers fanout and affinity implications.

## Common mistakes

- **Trusting a client-supplied name.** Identity is server-derived from the
  request-context user, never from a query param or body. A client may
  name a *topic* but can never claim another user's identity in the roster.
- **Exposing the roster on an ungated URL.** There is deliberately no
  framework HTTP roster endpoint: "who is viewing X" leaks identities.
  Build your own roster route behind your app's per-topic authorization.
- **Expecting instant cross-replica convergence.** A remote member can take
  up to one heartbeat (~15s) to appear after a dropped announcement, and up
  to the TTL (~45s) to disappear after a replica crash. This is by design
  (lossy, self-healing); wire `WithFanout` for aggregation; without it the
  roster stays single-replica.
- **Linking with `?presence=` and no hook.** A topic in the URL is a
  request, refused unless `AuthorizeTopic` admits it. Declare the topic on
  the screen instead.
- **Re-threading presence on SPA navigation.** The SSE topics are set from
  the page's declared list on initial load; a client-side nav to a presence
  page won't re-join a NEW topic (the meta is not rewritten). The stream
  itself now follows the page: it closes when the last push target
  leaves (so navigating away drops the viewer from the roster) and a
  navigation back to the roster island re-joins the topic the meta
  carries — the topic the document was loaded with. A user who landed
  on the presence page via SPA nav from an unrelated page joins
  nothing; full-load the presence URL so the join fires.
