# Analytics recipes

How to run a hosted product-analytics or experimentation vendor (PostHog,
Statsig, Plausible-class tools) through your app first-party: the vendor's
script and beacons served from your origin, pageviews tracking client-side
navigation, visitors identified from your session, and boolean experiment
gates surfaced through `core/featureflag`. Everything here composes five
shipped mechanisms: [`battery/relay`](relay.md), a host-authored bootstrap
served with `uihost.ScriptHandler` and registered with
`RegisterExternalScript`, the runtime's `gofastr:navigate` event,
`handler.GetUser`, and the `featureflag.Store` seam. The default CSP
(`default-src 'self'`) stays intact, no `connect-src`/`script-src`
exceptions, no third-party cookies on your origin, and no inline JS
anywhere.

## The pattern

One relay mount, one external bootstrap script, one identity endpoint:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/relay"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var host *uihost.UIHost
-->
```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))

// 1. The relay: the vendor's script and its endpoints become same-origin.
//    Upstreams are fixed at construction; request data never picks a host.
app.RegisterPlugin(relay.New(relay.Config{
	Routes: []relay.Route{
		{Prefix: "v-assets/", Upstream: "https://cdn.vendor.example",
			Methods: []string{"GET"}, CacheOK: true},
		{Prefix: "v/", Upstream: "https://ingest.vendor.example",
			Methods: []string{"GET", "POST"}},
	},
}))

// 2. The bootstrap: host-authored JS, served as a versioned external
//    script and emitted on every full-page render, after runtime.js.
js := []byte("/* configure the SDK, load it through the relay, wire pageviews + identity */")
app.Router().Get("/fp/analytics.js", uihost.ScriptHandler(js))
if err := host.RegisterExternalScript(uihost.ScriptURL("/fp/analytics.js", js)); err != nil {
	panic(err) // refused only once serving has begun; wire in main or plugin Init
}
```

What each piece buys you:

- `uihost.ScriptHandler` serves the bytes with the framework's
  versioned-script policy: a strong ETag, and immutable caching when the
  request's `?v=` matches the content hash. `ScriptURL` produces exactly
  that URL, so editing the bootstrap cache-busts it automatically.
- `RegisterExternalScript` puts the tag on every full-page render, after
  runtime.js. SPA partial responses carry no script tags, so the listener
  your bootstrap installs is installed once per real page load and
  survives every client-side navigation without double-firing. It refuses
  registration once a page has been served: a script that appears on some
  pages and not others is a wiring bug, so wire it in `main.go` or a
  plugin's `Init`. (`uihost.WithExtraScripts` is the construction-time
  equivalent.)
- Use `relay.Base()` when composing URLs server-side instead of
  hard-coding the mount prefix.

The recipes below only differ in the route table, the SDK configuration,
and the identity calls.

## Pageviews on a client-side-navigation app

GoFastr swaps page content client-side; there is no reload between `/a`
and `/b`, so a vendor's default "fire on script load" pageview counts one
visit, ever. The runtime fires `gofastr:navigate` on `window` when a
navigation completes. Its `detail` carries `path` (the destination),
`prevPath`, `cached` (true when the screen replayed from the client
cache), and `root` (the swapped element).

```js
window.addEventListener('gofastr:navigate', function (e) {
  var path = (e.detail && e.detail.path) || location.pathname;
  // fire your SDK's pageview here, e.g. posthog.capture('$pageview', ...)
});
```

Two rules the event's shape imposes:

- **The initial pageview is yours to fire.** `gofastr:navigate` fires on
  completed client-side navigations; first load is not one. Fire it once
  from the bootstrap, after the SDK has loaded and identity resolved.
- **Do not hook `gofastr:beforenavigate`.** That event is cancelable and
  fires before the router commits; a listener calling `preventDefault()`
  claims the click and no navigation happens at all. Counting there
  records pageviews for visits that never occurred. (The site's e2e
  analytics suite pins both rules: one pageview per `gofastr:navigate`,
  zero for a cancelled `beforenavigate`.)

## Identity

A 10-line same-origin endpoint over `handler.GetUser`:

<!-- gofastr:compile
import "encoding/json"
import "fmt"
import "github.com/DonaldMurillo/gofastr/core/handler"
import "github.com/DonaldMurillo/gofastr/framework"
import "net/http"
var app = framework.NewApp()
-->
```go
app.Router().Get("/fp/whoami", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // identity changes any moment
	var id any // nil marshals to JSON null
	if u, ok := handler.GetUser(r.Context()); ok && u != nil {
		switch v := u.(type) {
		case string:
			id = v
		case fmt.Stringer:
			id = v.String()
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}))
```

It answers `{"id":"..."}` or `{"id":null}`, never a guess, and
`no-store` so a login or logout is never served a cached identity. It
only knows a user when `auth.SessionMiddleware` (or your equivalent) put
one in the request context for that route chain.

The client side fetches it with a generation counter, so a stale response
can never overwrite a newer refresh's identity:

```js
var idGen = 0;
function refreshIdentity(apply) {
  var my = ++idGen;
  fetch('/fp/whoami', {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
    .then(function (r) { return r.json(); })
    .then(function (me) {
      if (my !== idGen) return; // superseded by a newer refresh
      apply(me && me.id ? me.id : null);
    })
    .catch(function () { /* analytics must never break the page */ });
}
```

Call it once after the SDK loads and again after any navigation that can
change identity (your login/logout destinations).

Sequencing the transitions matters as much as fetching them:

| Transition         | PostHog                    | Statsig                          |
|--------------------|----------------------------|----------------------------------|
| anonymous → A      | `identify(A)`              | `updateUserAsync({userID: A})`   |
| A → anonymous      | `reset()`                  | `updateUserAsync({})`            |
| A → B (switch)     | `reset()`, then `identify(B)` | `updateUserAsync({userID: B})` |

PostHog's `identify` links the current anonymous id into the named
user's chain; calling it while A is active merges A's history into B's.
`reset()` severs the chain first, which is why A→B is reset-then-identify
and never a bare identify. Statsig has no such chain; a plain user swap
is correct.

## PostHog

PostHog's proxy layout is two rules: `/static/*` and `/array/*` live on
the region's assets host, everything else (`/e`, `/s`, `/i/v0/e`,
`/flags`, `/decide`, `/batch`) on the ingestion host. `/decide` is legacy
but still called by current SDKs; the subtree route covers it for free.
PostHog's docs say to allow GET and POST on all proxied paths:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/relay"
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))
app.RegisterPlugin(relay.New(relay.Config{
	Routes: []relay.Route{
		// /static/* and /array/*: the SDK and its array of loader assets.
		{Prefix: "ph-assets/", Upstream: "https://us-assets.i.posthog.com",
			Methods: []string{"GET", "POST"}, CacheOK: true},
		// Everything else: /e, /s, /i/v0/e, /flags, /decide, /batch.
		{Prefix: "ph/", Upstream: "https://us.i.posthog.com",
			Methods: []string{"GET", "POST"}},
	},
}))
```

EU projects swap in `https://eu-assets.i.posthog.com` and
`https://eu.i.posthog.com`.

Two headers are load-bearing for PostHog, and the relay already does
both: the outbound request carries the upstream's own `Host` (PostHog
answers requests with the wrong `Host` 401), and `X-Forwarded-For` is
built from the connection's real peer, never from inbound headers, which
is what PostHog resolves geo from. Nothing to configure. Do not override
`relay.Config.ClientIP` with anything that reads inbound headers.

The bootstrap, whole:

```js
(function () {
  'use strict';
  var API = '/__gofastr/t/ph';
  var ASSETS = '/__gofastr/t/ph-assets';

  var s = document.createElement('script');
  s.src = ASSETS + '/array.js';
  s.async = true;
  s.onload = function () {
    var ph = window.posthog;
    if (!ph) return;
    ph.init('<phc_project_api_key>', {
      api_host: location.origin + API,     // the relay's ingestion route
      asset_host: location.origin + ASSETS, // the relay's assets route
      ui_host: 'https://us.posthog.com',   // the real region UI (EU: eu.posthog.com)
      capture_pageview: false,             // pageviews are ours (see below)
    });

    var known = null;
    function applyIdentity(id) {
      if (id === known) return;
      if (known) ph.reset(); // A→anon and A→B: sever the old chain first
      known = id;
      if (id) ph.identify(id);
    }
    // refreshIdentity is the helper from the Identity section above;
    // this bootstrap needs it in the same file.
    refreshIdentity(applyIdentity);

    // The initial pageview: gofastr:navigate does not fire on first load.
    ph.capture('$pageview', { $current_url: location.href });

    window.addEventListener('gofastr:navigate', function (e) {
      var path = (e.detail && e.detail.path) || location.pathname;
      ph.capture('$pageview', { $current_url: location.origin + path });
    });
  };
  document.head.appendChild(s);
})();
```

On the init options:

- `ui_host` must stay the real region UI (`https://us.posthog.com` /
  `https://eu.posthog.com`). Point it at the relay and the toolbar and
  the session-replay player break: they load UI assets from that host.
  It is configuration for PostHog tools, not a script your page loads.
- `capture_pageview: false` plus the manual `$pageview` calls above is
  the primary recipe: one pageview per completed client-side navigation,
  same timing as the content it names. The zero-code alternative,
  `capture_pageview: 'history_change'`, makes the SDK watch the History
  API itself; it fires when history changes, not when GoFastr's swap
  completes, so the pageview can precede the content it names.
- PostHog's own docs advise against proxy path names like `/analytics`,
  `/tracking`, or `/posthog`: path-based block lists carry generic rules
  for exactly those. The relay's neutral default mount (`/__gofastr/t`)
  already conforms, and the prefixes above describe the integration, not
  the vendor.

**Session replay.** Replay uploads can reach 64 MB request bodies. The
relay's default 8 MiB cap 413s them, which is the correct posture unless
you actually enable replay. If you do, raise the cap on the ingestion
route only, and read it as an egress number: every accepted byte is
billed to your bandwidth.

```go
{Prefix: "ph/", Upstream: "https://us.i.posthog.com",
	Methods: []string{"GET", "POST"}, MaxBodyBytes: 64 << 20},
```

## Statsig

Statsig's browser client talks to two endpoints on two different hosts:
`https://featureassets.org/v1/initialize` and
`https://prodregistryv2.org/v1/rgstr`. Both are POST beacons, and each is
a single fixed URL, so exact routes (no subtree prefix) fit:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/relay"
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))
app.RegisterPlugin(relay.New(relay.Config{
	Routes: []relay.Route{
		{Prefix: "sg-init", Upstream: "https://featureassets.org/v1/initialize",
			Methods: []string{"POST"}},
		{Prefix: "sg-events", Upstream: "https://prodregistryv2.org/v1/rgstr",
			Methods: []string{"POST"}},
	},
}))
```

Point the client at them with the per-endpoint overrides, not
`networkConfig.api`: `api` is one host override, and these two endpoints
live on two different hosts.

```js
statsig.initialize({
  clientSDKKey: 'client-your-key',
  networkConfig: {
    initializeUrl: location.origin + '/__gofastr/t/sg-init',
    logEventUrl: location.origin + '/__gofastr/t/sg-events',
  },
});
```

`initializeFallbackUrls` and `logEventFallbackUrls` also exist, for
multi-proxy failover; a single relay has none to offer.

The vendor HTML snippet loads a major-version pinned bundle
(`/@statsig/js-client@3/...` tracks every 3.x release; it is not a
frozen version). Two ways to serve it without a CSP exception:

```go
// Option A: relay jsdelivr and load the bundle through the mount.
{Prefix: "sg-js/", Upstream: "https://cdn.jsdelivr.net/npm",
	Methods: []string{"GET"}, CacheOK: true},
```

```html
<script src="/__gofastr/t/sg-js/@statsig/js-client@3/build/statsig-js-client+session-replay+web-analytics.min.js" defer></script>
```

Option B is to npm-bundle the SDK into your own asset build, in which
case only the two endpoints above go through the relay. Either way the
bootstrap follows the same shape as PostHog's: initialize, then identity
transitions, then the initial pageview and one `gofastr:navigate`
listener firing your page-view event (`statsig.logEvent` with your
schema).

Statsig's identity calls map straight onto the transition table:

```js
var known = null;
function applyIdentity(id) {
  if (id === known) return;
  known = id;
  statsig.updateUserAsync(id ? { userID: id } : {});
}
```

One vendor constraint the relay satisfies by construction: Statsig's
proxy guidance says never deserialize or inspect payload bodies. The
relay streams request and response bytes through untouched, capping
without parsing; there is no code path that could read them.

## Server-side feature flags

The same vendor can gate server code through `core/featureflag`: implement
`featureflag.Store` with an adapter that asks the vendor's server SDK
(posthog-go, the Statsig server SDK, whatever your app imports; the SDK
is a host-app dependency, never the framework's) and returns a `Flag`
that reproduces the vendor's decision.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core/featureflag"
import "github.com/DonaldMurillo/gofastr/framework"

type vendorClient struct{}

func (vendorClient) BoolGate(ctx context.Context, key, userID string) (bool, bool, error) {
	return false, false, nil // stand-in for posthog-go / the Statsig server SDK
}

var app = framework.NewApp()
stmt: app.SetFlagStore(vendorFlagStore{client: vendorClient{}})
-->
```go
type vendorFlagStore struct{ client vendorClient }

// Get implements featureflag.Store. The request ctx arrives here, so
// FromContext yields the caller the middleware attached.
func (s vendorFlagStore) Get(ctx context.Context, key string) (*featureflag.Flag, error) {
	ec := featureflag.FromContext(ctx)
	on, exists, err := s.client.BoolGate(ctx, key, ec.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return &featureflag.Flag{Key: key, Enabled: on, Rollout: 100}, nil
}
```

Each return is load-bearing:

- **`Rollout: 100` is required.** The vendor already decided per user;
  any rollout under 100 would re-bucket subjects from a stable hash and
  split that decision. `Enabled` alone carries the answer: `true` turns
  the flag on for every caller, `false` is the kill switch and returns
  false for everyone.
- **`(nil, nil)` only when the vendor proves the key unknown.** That is
  the "genuinely absent" answer `BoolDefault` needs to honor its
  fallback, so a typo'd kill-switch key still fails safe to the default
  you chose.
- **Any other outcome returns the error.** The evaluator fails closed:
  `Bool` and `BoolDefault` both answer false on storage errors, so a
  vendor outage disables gated code instead of opening it.

Wire it before the first flag check, and attach the caller once per
request:

<!-- gofastr:compile
import "context"
import "fmt"
import "github.com/DonaldMurillo/gofastr/core/handler"
import "github.com/DonaldMurillo/gofastr/core/featureflag"
import "github.com/DonaldMurillo/gofastr/framework"
import "net/http"

type vendorFlagStore struct{ client vendorClient }
type vendorClient struct{}

// stub for the block above; the real adapter implements Get
func (vendorFlagStore) Get(ctx context.Context, key string) (*featureflag.Flag, error) {
	return nil, nil
}

var app = framework.NewApp()
-->
```go
// Before any handler calls Flags()/IsEnabled: SetFlagStore panics once
// the lazy default evaluator has been used, by design.
app.SetFlagStore(vendorFlagStore{client: vendorClient{}})

app.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ec featureflag.EvalContext
		if u, ok := handler.GetUser(r.Context()); ok && u != nil {
			// Same normalization as the identity endpoint: a mismatch
			// here means flags evaluate anonymous for users the
			// endpoint identifies.
			switch v := u.(type) {
			case string:
				ec.UserID = v
			case fmt.Stringer:
				ec.UserID = v.String()
			}
		}
		next.ServeHTTP(w, r.WithContext(featureflag.WithContext(r.Context(), ec)))
	})
})
```

Boolean gates only. Multi-variant experiments (variants, payloads,
holdouts) do not fit `Flag`'s on/off model; gate those handlers on the
vendor SDK directly. See [Feature flags](feature-flags.md) for the
evaluator itself.

## Common mistakes

- **A bare mount is not a beacon URL.** A subtree route's fully empty
  tail joins onto the upstream base without a trailing slash (`.../ph/`
  becomes the base path itself), and a vendor that answers that bare
  path with a redirect gets the redirect refused by the relay — a 502
  the SDK's `.catch` swallows. Tailed paths are fine, including
  trailing-slash ones (`/i/v0/e/` is forwarded verbatim); it is only
  the empty tail that has nothing to forward.
- **No auth middleware on the identity chain.** The endpoint reads
  `handler.GetUser`, which carries a value only when
  `auth.SessionMiddleware` (or your equivalent) ran. Without it every
  response is `{"id":null}` and you shipped an anonymous-only funnel.
- **A global `auth.CSRF()` 403s the beacon POSTs.** Vendor beacons are
  form-less POSTs with no token. Exempt the mount, deriving the prefix
  from the relay so a custom `Path` stays covered:
  `app.Use(auth.CSRF(auth.WithCSRFSkipPaths(r.Base() + "/")))`. This is
  safe because the relay strips cookies both directions: there is no
  credential in play to forge with.
- **`auth.RequireSession` app-wide gates the relay.** Beacons fire from
  logged-out pages and before login; a session gate turns your
  first-party numbers into "only logged-in users". Gate per-route if you
  must, never the app. (Same callout as [relay](relay.md); it bears
  repeating because analytics is where app-wide auth defaults bite.)
- **Expecting this to work from a static export.** `app.ExportStatic`
  renders HTML files; a reverse proxy is a live server. The relay mount,
  the bootstrap's identity fetch, and the beacons all need your Go
  process running. Static-export deployments get no first-party
  analytics; serve the app or skip the integration.
- **Ad-block optimism.** First-party serving defeats domain-based block
  lists only. Path-based rules still match `/analytics`-style paths under
  your mount, and visitors who block on behavior rather than origin still
  block. Keep the neutral default mount and prefixes that describe your
  integration, and accept the remainder. See "Ad-block honesty" in
  [relay](relay.md).
- **Raising the body cap without reading it as egress.** The 8 MiB
  default is a brake: every accepted byte leaves from your servers and is
  billed to your bandwidth. The 64 MB replay exception above is for a
  named, enabled feature on one route, not a posture.
