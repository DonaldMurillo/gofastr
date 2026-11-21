# Live presence

Presence tracks **who is currently connected** to a topic on the server so pages can render live viewer rosters (`ui.AvatarGroup` with online dots, "3 people viewing"). Because live presence depends on the open connection itself, it is the canonical use case for GoFastr's `/__gofastr/sse` push bus.

## Security model

- **Identity is strictly server-derived.** Roster entries `{UserID, DisplayName}` come exclusively from `island.PresenceIdentityFromContext(ctx)` (reading `handler.GetUser(ctx)` set by `battery/auth`). Query parameters like `?user=` are ignored. Unauthenticated sessions receive a deterministic pseudo-identity per session cookie (`anon:<session>` / `Guest <tail>`), so two tabs in one browser deduplicate into one viewer.
- **No built-in HTTP roster endpoint.** Exposing "who is viewing topic X" over an ungated URL would leak user identities. Access rosters in-process via `Manager.PresenceRoster(topic)` and push rendered HTML to authorized viewers—or build your own auth-gated endpoint.
- **Topic bounds:** Up to 16 topics per connection, 128 bytes per topic, duplicates collapsed (`island.ParsePresenceTopics`).
- **Topic authorization:**
  - **Declared topics** (returned by a screen's `PresenceTopics(ctx)`) are HMAC-signed into `<meta name="gofastr-sse">` for that viewer and join automatically.
  - **Client-requested topics** (`?presence=...`) are **refused by default** unless `Manager.SetAuthorizeTopic` admits them. Once installed, `SetAuthorizeTopic` governs *all* topics (declared and requested) at connect time and on every heartbeat, silently dropping unauthorized topics so callers can't probe topic existence.

## Wiring a presence page

1. **Declare topics on the screen (`uihost.PresenceScreen`):**

```go
func (s *DocScreen) PresenceTopics(ctx context.Context) []string {
    return []string{"doc:" + s.doc.ID}
}
```

2. **Render a roster island slot in the screen:**

```go
html.Div(html.DivConfig{
    ExtraAttrs: html.Attrs{"data-island": "presence-roster-" + topic},
    Role:       "status",
    AriaLabel:  "Who's viewing",
}, renderRoster(mgr.PresenceRoster(topic)))
```

3. **Wire `SetOnPresenceChange` (and `SetAuthorizeTopic` if access can change mid-session or clients pass `?presence=`):**

```go
mgr.SetAuthorizeTopic(func(ctx context.Context, topic string) bool {
    return strings.HasPrefix(topic, "public:") || userMayViewTopic(ctx, topic)
})

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

No custom client JS is required—the built-in `sse.js` module swaps the `[data-island="..."]` slot on every join and leave. For custom transports outside `uihost`, call `mgr.ServeSSEWithPresence(w, r, id, island.PresenceTopics{Declared: ..., Requested: ...})`.

Try it live at `/examples/presence` in `examples/site` (open two browsers or a normal + private window to get distinct sessions).

## Cross-replica aggregation

Attach `framework.WithFanout(...)` and presence rosters merge automatically across replicas—no extra presence wiring needed:

- Each replica broadcasts its local roster per active topic on `gofastr.presence` on change and on a 15-second heartbeat.
- Remote entries expire after ~45 seconds (3× heartbeat) if a replica crashes; graceful `Shutdown` publishes an empty roster immediately.
- Panics inside `SetOnPresenceChange` are recovered and logged per topic so one bad callback never breaks other topics.
- Without a fanout, presence stays single-replica with zero background goroutines.

## Common mistakes

- **Trusting client-supplied names or topics.** Identities always come from auth context; requested `?presence=` topics require `SetAuthorizeTopic`.
- **Exposing `PresenceRoster` on a public HTTP route.** Always gate any custom roster endpoint behind per-topic authorization.
- **Expecting instant cross-replica convergence.** Fanout is lossy and self-healing—dropped frames heal within one 15s heartbeat, and crashed replicas expire after ~45s.
- **Expecting SPA navigation from another page to join a new topic.** `<meta name="gofastr-sse">` is rendered on initial document load (leaving a presence page closes the stream and drops you from the roster, and returning re-joins the original document's topic, but navigating from an unrelated page won't join a new topic). Full-load presence pages when entering from unrelated routes.
