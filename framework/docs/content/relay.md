# First-party relay (`battery/relay`)

`battery/relay` is a locked-down reverse proxy plugin that serves allowlisted third-party endpoints (analytics, error tracking, scripts) from your own origin (`/__gofastr/t/...`) so your strict `default-src 'self'` CSP stays intact and user cookies never reach third-party vendors.

## Quickstart

<!-- gofastr:compile
stmt: _ = app
-->
```go
import (
	"github.com/DonaldMurillo/gofastr/battery/relay"
	"github.com/DonaldMurillo/gofastr/framework"
)

app := framework.NewApp(
	framework.WithConfig(framework.AppConfig{Name: "myapp"}),
)
r := relay.New(relay.Config{
	Routes: []relay.Route{
		{
			Prefix:   "e", // exact endpoint: POST /__gofastr/t/e → plausible.io/api/event
			Upstream: "https://plausible.io/api/event",
			Methods:  []string{"POST"},
		},
		{
			Prefix:   "js/", // subtree: GET /__gofastr/t/js/{rest} → plausible.io/js/{rest}
			Upstream: "https://plausible.io/js",
			Methods:  []string{"GET"},
			CacheOK:  true,
		},
	},
})
app.RegisterPlugin(r)
```

Use `r.Base() + "/e"` to build client URLs instead of hardcoding `/__gofastr/t`.

## Fixed hardening contract

`battery/relay` is **not an open proxy**. Every guard is always on:
- **Fixed upstreams:** scheme/host/port come solely from `Route.Upstream` (`https://` required; RFC 1918, loopback except explicit dev/test, link-local/metadata, and `*.internal` rejected at construction). Path tails reject traversal (`..`, `%2e%2e`), `%2F`, backslashes, and control bytes with `400`.
- **No credential leakage either way:** inbound `Cookie`, `Authorization`, `Proxy-Authorization`, `X-CSRF-Token`, `X-API-Key`, `X-Forwarded-*`, and `X-Gofastr-*` (including embed grants) are stripped, and `Referer` is truncated to origin only. Outbound `Set-Cookie`, `WWW-Authenticate`, `Access-Control-*`, `Refresh`, `Clear-Site-Data`, `Service-Worker-Allowed`, `NEL`/`Report-To`, `Alt-Svc`, `Strict-Transport-Security`, and cross-origin `Link` headers are stripped.
- **Strict content-type allowlist:** allows scripts, CSS, images, fonts, JSON, and `text/plain` (SVG gets `Content-Security-Policy: sandbox`). `text/html` and other types return `502` unless explicitly opted in via `Route.ContentTypes` (which trusts that vendor on your origin — prefer iframes on the vendor's origin instead).
- **No redirects, no 1xx hints, no WebSocket upgrades:** `3xx` with `Location` and `101` upgrades return `502`.
- **Bounded bodies & timeouts:** default 8 MiB request/response body cap (`MaxBodyBytes`), 30s request budget, `Cache-Control: no-store` unless `CacheOK: true`.

## Common mistakes

- **Forgetting to exempt the relay from app-wide CSRF middleware:** cookie-free analytics POSTs get `403` unless you pass `auth.WithCSRFSkipPaths("/__gofastr/t/")` (safe because `battery/relay` strips all cookies/credentials).
- **Gating `/__gofastr/t/` behind `RequireSession`:** drops logged-out pageviews and error beacons.
- **Expecting ad-block immunity for paths named `/collect` or `/analytics`:** first-party proxying defeats domain lists, not generic path-based rules. Use neutral route prefixes like `e/` and `js/`.
- **Raising `MaxBodyBytes` blindly:** all relayed egress is billed to your server bandwidth.
