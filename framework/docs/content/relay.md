# First-party relay

`battery/relay` is a declarative reverse proxy that serves third-party
services — analytics vendors, chat widgets, error trackers — from your
own origin. You declare one mount path and, under it, a fixed list of
routes: prefix, upstream, allowed methods. Visitors' browsers then talk
to `https://your-app.example.com/__gofastr/t/...` instead of the
vendor's domain, and your strict default Content-Security-Policy
(`default-src 'self'`) stays untouched: no `script-src` or
`connect-src` exceptions, no third-party cookies on your origin, no
vendor DNS on your pages. It is a `framework.Plugin`, not a Battery:
no dependencies, no workers, one shared HTTP transport whose idle
connections close on app shutdown.

## Wiring

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
	// Path defaults to "/__gofastr/t"; any absolute path works.
	Routes: []relay.Route{
		{
			Prefix:   "e/",   // subtree: POST /t/e/{rest} → plausible.io/api/event/{rest}
			Upstream: "https://plausible.io/api/event",
			Methods:  []string{"POST"},
		},
		{
			Prefix:   "js/",   // GET /t/js/{rest} → plausible.io/js/{rest}
			Upstream: "https://plausible.io/js",
			Methods:  []string{"GET"},
			CacheOK:  true,   // immutable, versioned scripts may cache
		},
	},
})
app.RegisterPlugin(r)
```

Point the vendor's client SDK at the mount with `Base()` instead of
hard-coding the prefix:

```go
plausibleBase := r.Base() + "/e"
```

`New` panics on invalid config (unknown scheme, `http://` to a
non-loopback host, an internal/private upstream, empty `Methods`,
traversal in a prefix, a mount colliding with a reserved
`/__gofastr/...` route). A misdeclared relay is a programmer error you
want at startup, not a runtime surprise.

## Route table semantics

| Prefix shape    | Matches                          | Upstream mapping                          |
|-----------------|----------------------------------|-------------------------------------------|
| `e/` (subtree)  | `/t/e/` and everything below it  | base path + the sanitized tail            |
| `ping` (exact)  | `/t/ping` only                   | base path as-is                           |

- The request's **query string passes through** verbatim
  (`/t/e/x?v=2` → upstream `…/x?v=2`); analytics SDKs need it.
- **Methods are an allow-list.** Empty is invalid — be explicit.
  Anything undeclared gets `405` with an `Allow` header; tails under
  no declared prefix get `404`.
- **Bodies** are capped per route at `MaxBodyBytes` (default 8 MiB),
  covering both declared `Content-Length` and chunked bodies → `413`.
  The same budget bounds the upstream's response. Its status and headers
  are already sent when the cap is reached, so the relay aborts the
  stream: the client sees a broken response, never a clean one cut
  short. A response of exactly the budget arrives whole.
- **Caching**: `CacheOK: false` (default) forces
  `Cache-Control: no-store` on every response. `CacheOK: true` passes
  upstream cache headers through, for immutable versioned assets.
  `304 Not Modified` always passes through.

## The hardening contract

None of this is configurable. The relay is on your public edge, so its
posture is fixed:

| Guard | Behavior |
|---|---|
| Fixed origins | Scheme, host, and port come ONLY from `Route.Upstream`. The path tail and query are the only attacker-influenced parts of the outbound request. |
| Tail validation | Traversal (`..`, `%2e%2e`), backslashes, NUL/control bytes, empty segments (`//`), encoded slashes (`%2F`, caught via `RawPath`), and fragment markers are refused with `400`. The router bounds path params; the relay still validates at the sink. |
| Credentials stripped (inbound) | `Cookie`, `Authorization`, `Proxy-Authorization`, `X-CSRF-Token`, `X-API-Key`, every `X-Forwarded-*`, and every `X-Gofastr-*` (the app's own headers, among them the embed grant an embed frame sends on each fetch) never reach the vendor. `Referer` is cut to its origin (`https://app.example/`): a relayed request is same-origin, so the browser sends the full page URL, query string and any token in it included, where a request straight to the vendor would carry the origin alone. A `Referer` that is not an absolute `http(s)` URL is dropped. |
| Forwarded metadata | `X-Forwarded-For` is the connection's peer (or your `Config.ClientIP`), `X-Forwarded-Proto` comes from the actual TLS state. Inbound forwarding headers are never trusted — they are one `curl -H` away from arbitrary values. |
| Auth stripped (outbound) | `Set-Cookie`, `WWW-Authenticate`, `Proxy-Authenticate`, and `Access-Control-*` never reach the browser. `X-Content-Type-Options: nosniff` is always added. |
| Browser state stripped (outbound) | `Refresh` (a header-driven navigation to any origin) `Clear-Site-Data` (deletes the visitor's app-origin storage from the vendor's side) and `Service-Worker-Allowed` (lets vendor script register a service worker over every app page) never reach the browser. A `Link` header value with a target on another origin is dropped whole. The target is read the way the browser's URL parser reads it: tabs and newlines removed, `\` as `/`, so `<\\vendor.example/x>` is the protocol-relative URL it parses as. A target with a scheme (`https:`, `data:`) or an authority (`//host`) counts as another origin; relative targets (`</page/2>; rel="next"`) resolve against the app origin and are forwarded. |
| No informational responses | An upstream `1xx` (`103 Early Hints`) is not forwarded: its headers skip the outbound strips, so a vendor `Link` preload would reach the browser. The headers the app set before the relay ran (the security headers) stay on the final response, ahead of the vendor's. |
| No redirects | Any `3xx` carrying a `Location` header is replaced with a plain `502` and logged: forwarding it would leak the vendor origin (absolute) or point outside the mount (relative). `304` passes. |
| No protocol switches | WebSockets and other upgrades are not relayed. The inbound `Connection: Upgrade` / `Upgrade` pair is dropped, so the vendor sees a plain HTTP request, and a `101 Switching Protocols` the vendor sends anyway is refused with a `502` and its connection closed: an open socket would run past the body cap and the request deadline. A chat widget that needs a socket connects to the vendor directly. |
| Compression | Proxied bytes are never decompressed or re-encoded: a gzip body arrives byte-identical. |
| Timeouts | Dial 5s, TLS handshake 5s, response headers 10s, whole request min(inherited context, 30s). Deadlines/timeouts → `504`; other transport failures → `502`. Error bodies are plain text; upstream error detail goes to the log only. |
| Upstream restrictions | `https://` only, except `http://` for loopback (tests/dev). RFC1918, link-local (incl. cloud metadata), CGNAT, IPv6 unique-local, and `*.internal` upstreams are refused at `New` regardless of scheme. `localhost` and `*.localhost` count as loopback. |

## Threat model

**This is not an open proxy.** An open proxy takes a URL from the
request and fetches it; the relay takes nothing. Every route names its
upstream at construction, request data cannot select scheme, host, or
port (verified by hostile-tail tests in `battery/relay`), credentials
flow neither direction, and the outbound surface per route is bounded
by the method list and the body cap.

**Egress is your cost now.** Every relayed byte leaves from your
servers, and every request your page makes to the vendor is a request
an attacker can also make to your wallet: the mount is public unless
you gate it (see below). The 8 MiB default body cap is a brake, not a
budget — think before raising it.

**Ad-block honesty.** Serving the vendor first-party defeats
*domain-based* block lists (the request no longer goes to
`plausible.io`, it goes to you). It does NOT defeat *path-based* lists:
ad-blockers that ship generic rules for `/api/event`,
`/collect`, `/analytics` style paths will match those same paths under
your mount. That is why the default `Path` is the neutral
`/__gofastr/t` and prefixes are yours to choose: pick names that
describe YOUR integration (`t/e`, `t/js`), not the vendor's
vocabulary, and don't expect immunity — visitors who block trackers on
behavior rather than origin will still block.

## Common mistakes

- **Forgetting `auth.CSRF()` when you add `app.Use(auth.CSRF(...))`
  app-wide.** The relay's POST routes are form-less beacons; a global
  CSRF middleware 403s them because they carry no token. Exempt the
  mount: `app.Use(auth.CSRF(auth.WithCSRFSkipPaths("/__gofastr/t/")))`
  — the credential-stripping contract above is what makes this safe:
  no cookies reach the vendor, so there is nothing to forge with.
- **Gating the relay behind `auth.RequireSession`.** Analytics and
  error-tracking beacons fire before login and from logged-out pages;
  a session gate turns your first-party numbers into
  "only logged-in users". Gate per-route with your own middleware if
  you must, not app-wide.
- **Assuming ad-block immunity.** First-party origin defeats
  domain-based lists only. Path-based lists still match; see
  "Ad-block honesty" above.
- **Raising `MaxBodyBytes` without thinking.** The cap is an egress
  brake: every accepted byte is billed to your upstream bandwidth.
  Beacon endpoints need kilobytes; if a vendor wants 100 MiB uploads
  through your relay, that traffic belongs on the vendor's own origin,
  not yours.
