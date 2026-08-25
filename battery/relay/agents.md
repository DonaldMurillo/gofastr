# battery/relay

Declarative, hardened same-origin reverse proxy: serve third-party
services (analytics vendors, chat widgets, error trackers) from the
app's own origin so the strict default CSP stays untouched and the
app's cookies never reach the vendor.

**Use this when** the prompt mentions: analytics proxy, first-party
proxy, reverse proxy to a vendor, PostHog/Plausible/Statsig behind
your own domain, ad-blocker-resistant tracking, "keep CSP strict but
load a third-party script", CSP `connect-src` exceptions.

**Import:** `github.com/DonaldMurillo/gofastr/battery/relay`

**Shape:**
```go
r := relay.New(relay.Config{
    // Path defaults to "/__gofastr/t".
    Routes: []relay.Route{
        {Prefix: "e/", Upstream: "https://vendor.example/api/event",
         Methods: []string{"POST"}},                    // subtree
        {Prefix: "js/", Upstream: "https://vendor.example/js",
         Methods: []string{"GET"}, CacheOK: true},      // versioned assets
    },
})
app.RegisterPlugin(r)
base := r.Base() // point SDKs/templates here, don't hard-code the path
```

**Rules that will bite you if ignored:**
- One fixed upstream per route, compiled at `New`; request data never
  selects scheme/host/port. `New` PANICS on: non-https upstreams
  (http is loopback-only), private/internal hosts, empty `Methods`,
  duplicate prefixes, a `Path` colliding with a reserved
  `/__gofastr/...` route.
- Credentials are stripped both directions (Cookie/Authorization in,
  Set-Cookie out) and inbound `X-Forwarded-*` is never trusted. Do
  not "fix" a vendor integration by re-adding headers.
- Bodies are capped (8 MiB default, `MaxBodyBytes` per route);
  responses default to `Cache-Control: no-store` unless
  `CacheOK: true`.
- A global `app.Use(auth.CSRF(...))` 403s the relay's POST beacons —
  exempt the mount with `auth.WithCSRFSkipPaths`. Do not gate the
  relay behind `RequireSession` (beacons fire logged-out).

Full doc: `framework/docs/content/relay.md` (`gofastr docs relay`).
