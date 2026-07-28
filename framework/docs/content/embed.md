# Embeddable surfaces

An app marks a screen embeddable and names the origins allowed to frame it.
Its customer pastes one `<script>` tag into their own website and gets a live,
themed, authenticated piece of the app.

```html
<div id="reports"></div>
<script src="https://app.example.com/__gofastr/embed.js"
        data-surface="reports"
        data-token="emb_…"
        data-target="#reports"></script>
```

## Declaring a surface

```go
import "github.com/DonaldMurillo/gofastr/framework/embed"

burn, err := embed.NewSQLBurnStore(db)
if err != nil { log.Fatal(err) }

embeds, err := embed.New(embed.Config{
    Surfaces: []embed.Surface{{
        Name:    "reports",
        Path:    "/embed/reports",
        Origins: []string{"https://acme.com", "https://shop.acme.com"},
        Scopes:  []string{"read"},
        Theme:   embed.ThemeConfig{AllowTokens: []string{"color-primary"}},
    }},
    BurnStore: burn,
    Resolve: func(ctx context.Context, subject string) (any, error) {
        return users.FindByID(ctx, subject)
    },
})
if err != nil { log.Fatal(err) }

site := uihost.New(application, uihost.WithEmbed(embeds))

// fwApp is the *framework.App. It is not called `app` because `app` is the
// core-ui/app PACKAGE, used further down for NewScreen and EmbedLayout — one
// name cannot be both.
fwApp.Mount(site)

// Island RPCs from inside a frame target ORDINARY app routes, which know
// nothing about embeds. This is what authenticates them. Install it FIRST,
// outside every authentication middleware — see below.
fwApp.Use(embeds.Middleware())

// Scopes are not enforced automatically. This is what makes them bind.
reports := fwApp.Group("/reports")
reports.Use(embeds.RequireScope("reports:read"))
```

### What a grant may reach

A grant reaches its surface's own `Path` subtree and the runtime's
`/__gofastr/*` endpoints. **Everything else answers 403** until the surface
says otherwise:

```go
Surface{
    Name: "reports",
    Path: "/embed/reports",
    // The surface's form posts here, outside its own subtree.
    Reach: []string{"/api/orders"},
}
```

Prefixes match on segment boundaries, so `/api/orders` admits `/api/orders/42`
and refuses `/api/orders-archive`. `Reach` is per-surface — a grant for one
surface never inherits another's.

The default is closed because a grant is not a session. It lives in a third
party's page, in JavaScript, where anyone with devtools can read it, and it
stays valid until its absolute deadline. The alternative — reach whatever the
subject can reach, unless the author remembers to gate it — is not a property
anyone can hold: the framework mounts `/mcp`, `{auth}/tokens` and `/admin/*`
for you, so the most dangerous routes are the ones the author never wrote.

`Reach` is validated at boot. `"/"` is refused, and so is any prefix covering a
framework-mounted route — a configuration that cannot be right should not
start. When a request is refused, the 403 names the surface, the path, and the
`Reach` entry that would allow it.

### Scopes narrow further, within reach

`Reach` decides which routes an embed may touch at all. Scopes decide what it
may do once there, and are not enforced automatically: `Middleware()` installs
the subject with that subject's full authority, including any role they hold.
Gate the routes that need it:

```go
reports := fwApp.Group("/reports")
reports.Use(embeds.RequireScope("reports:read"))

// or, as a group option:
fwApp.Group("/reports", routegroup.WithMiddleware(embeds.RequireScope("reports:read")))
```

`RequireScope` refuses a grant that does not carry the scope, and passes
ordinary first-party traffic straight through — it narrows what an *embed* may
do and nothing else.

To branch inside a handler or a screen rather than gate a whole group, read the
grant off the context:

```go
if g, ok := embed.GrantFromContext(ctx); ok && g.HasScope("comment") {
    body = append(body, CommentForm())
}
```

`ok` is false on an ordinary first-party request, so this is also how a screen
asks "am I being rendered inside someone else's page?".

### Install `Middleware()` outermost

It must run before any authentication middleware: session auth, bearer auth,
API-token auth, anything deriving a tenant from a credential.

```go
fwApp.Use(embeds.Middleware())   // first
fwApp.Use(auth.Session(...))     // then everything else
```

It discards the credentials themselves — `Cookie`, `Authorization`, `X-API-Key`
— so an authenticator running inside it finds nothing to authenticate and the
grant's subject stands alone. Installed the other way round, an authenticator
that already ran has written its own values onto the context and this middleware
cannot take them back: it does not know which keys another package used. The
observable failures are a bearer token overwriting the grant's identity, and an
API token's scopes surviving under the grant subject's name.

`Path` is an ordinary app route. Register it with the `core-ui/app` package's
`EmbedLayout()` and it renders with no header, nav or footer — which is what
you want inside a 400px frame on someone else's page:

```go
application.RegisterScreen(app.NewScreen("/embed/reports", &Reports{}), app.EmbedLayout())
```

(`app` here is the imported `core-ui/app` package — which is why the framework
App value above is named `fwApp`.)

An island-only embed is a screen whose body is that island. There is no second
render path for islands, deliberately: one path means one set of security
decisions.

## Handing a customer their snippet

The app mints a nonce server-side, for one viewer, on one origin:

```go
nonce, err := embeds.MintNonce("reports", user.ID, "https://acme.com", nil)
```

Render `nonce` into the snippet you give the customer. Passing `nil` for scopes
grants the surface's declared set; passing a scope the surface does not declare
is an error rather than a silent drop.

## An app secret is required

`WithSecret` (or `GOFASTR_SECRET`) must be set. Unlike sessions there is no
per-boot fallback key: a session that fails to verify is re-minted on the next
render and nobody notices, but a nonce that fails to verify is gone — it is
single-use, it lives for a minute, and it was rendered into a page on someone
else's site that you cannot re-render. An app with embeddable surfaces and no
secret fails at boot.

## The credential

A single-use handshake nonce exchanged for a short-lived grant.

Single use means one nonce yields **one grant**. A captured nonce cannot mint a
second, independent credential, and a nonce that has been spent is refused.

It does **not** mean one nonce serves one person, and the difference matters:

> **Mint a fresh nonce on every page render, and do not cache the page that
> carries it.** The exchange is idempotent, so a nonce baked into a cached page
> hands the *same* grant — the same identity — to everyone who loads that copy,
> for as long as the grant lives. That is a wrong-tenant render with no error
> and no log line: visitor B simply sees visitor A's data.

`MintNonce` is called from the app's own backend while rendering the customer's
page, which is the point where the app knows which viewer this embed is for. If
that page is behind a CDN or any shared cache, the nonce must not be in the
cached body — render it per request, or fetch it from an uncached endpoint.

The framework helps where it can: repeated exchanges of one nonce are counted,
and a nonce exchanged more than once logs a warning naming the surface, because
the alternative is a failure nobody can see.

Minting is stateless — an HMAC over (surface, subject, scopes, origin, nonce id,
expiry). Only the exchange touches the store, and it claims the nonce id against
a unique constraint, so "already used" is decided by the database rather than by
a read followed by a write that two replicas could both win.

The exchange is POST-only and idempotent within the grant's lifetime: a repeat
returns the same grant. A browser has several ways to fire it twice — the
customer's page prefetches the iframe, a dev double-mounts the loader, a user
refreshes — and without idempotency the feature surfaces as "the embed randomly
doesn't load".

| Window | Default | Config |
|---|---|---|
| Nonce lifetime | 1 minute | `Config.NonceTTL` |
| Grant lifetime | 15 minutes | `Config.GrantTTL` |
| Total refresh window | 12 hours | `Config.GrantMaxAge` |

The frame refreshes its grant while it lives, so `GrantTTL` costs a background
request rather than a broken embed. `GrantMaxAge` is the absolute deadline the
grant has carried since it was issued; refreshes move the expiry and never the
deadline, so a frame left open in a tab does not hold a credential forever.

Use `NewSQLBurnStore` for anything beyond a single long-lived process.
`NewMemoryBurnStore` keeps its burns in a map: two replicas each keep their own,
so one nonce is spendable once per replica, and a restart forgets every burn —
so a nonce still inside its TTL becomes spendable again.

## Origins

Exact origins only. There is no wildcard and no "allow any" spelling; every
subdomain is listed separately.

Origins are compared normalized, not as strings — `https://acme.com`,
`https://acme.com/`, `https://acme.com:443` and `https://ACME.com` are one
origin written four ways, and comparing the raw strings means a customer's
trailing slash silently never matches.

The browser-enforced control is the embed document's CSP `frame-ancestors`
directive, which lists every allowed origin. It has to list them all: no
`Origin` header is sent on a navigation GET, so at the moment the header is
written the server does not know who is framing it. Listing ten origins does not
let an eleventh frame the page — the browser enforces against the real ancestor
chain.

Two consequences worth knowing before you sell this to your hundredth customer:

**Your customer list is public.** The allowlist ships in a response header on an
unauthenticated route, so anyone who fetches the embed URL can enumerate every
origin allowed to frame that surface. If who-your-customers-are is not public
information, put those customers on separate surfaces.

**The list is bounded.** All of it goes into one directive on every shell
response, so the customer count is encoded into response-header size. GoFastr
refuses at boot past 4 KiB of origins (a few hundred), because the alternative
is discovering the limit when a proxy starts truncating the header and the
surface breaks for *every* customer at once. Past that, split across surfaces.

Origins are fixed at boot. Adding a customer is a config change and a deploy;
there is no runtime API for it.

## Cookies

Inside the frame the session cookie is never sent, even though the frame is
same-origin with the app: SameSite is computed against the top-level browsing
context and the full ancestor chain, and the top level is the customer's site.

That makes CSRF against embed routes structurally impossible — identity can only
arrive explicitly. Embed routes go further and discard cookies before any
handler reads one, because there is a case where a cookie really does arrive: an
app at `app.acme.com` framed by `www.acme.com` is *same-site*, so a Strict
cookie rides along. Honouring it would hand a signed-in user's full session to a
third party's frame.

## What ships to the browser

Two files, budgeted separately because they land in different places.

`/__gofastr/embed.js` is the loader, on the customer's critical path. It creates
the iframe, hands the nonce over by postMessage, and resizes. Nothing else — it
has the tightest budget in the repo.

`/__gofastr/embed-runtime.js` is the runtime inside the frame: kernel, island
RPC, signals, widgets, hydration. It **omits the nav fragment**, so the SPA
navigator does not exist inside a frame — by absence, not by a flag something
could flip back.

What that buys is precise: no client-side route changes, no screen cache, no
`<a>` hijack. It does not make the frame un-navigable. A redirect followed by a
form intercept, or a component that falls back to `location.href` when the
navigator is missing, still moves the frame to an ordinary app page — whose CSP
carries the app default `frame-ancestors 'none'`, so the browser refuses to
render it and the panel goes blank. Keep destinations inside an embeddable
surface, and prefer island RPC over anything that navigates.

Two things are neutralised inside a frame rather than merely absent, because
absence alone was not enough.

`history.pushState` and `replaceState` are replaced with no-ops. They are not
navigation, so removing the nav fragment did not remove them, and called inside
a frame they append to the **top-level** page's session history — the
customer's back button, not the frame's. Widget and pane deep links and any
island answering `X-Gofastr-Push-State` all call them. The practical
consequence now is that **deep links are inert in an embed**: a modal opens and
closes without touching the URL, and a pane deep link does not restore on
reload. That is a deliberate trade against silently breaking a stranger's back
button.

`__gofastr.navigate` is installed as a no-op for the same reason. Modules that
fall back to `location.href` when the navigator is missing — `combobox` does —
would otherwise move the frame to an ordinary app route whose CSP refuses to be
framed, leaving a blank panel with the runtime and the grant gone and nothing
left able to report it.

Every request the frame makes carries the grant, because the frame attaches it
to every same-origin fetch rather than to one header builder. That matters more
than it sounds: island RPC, polling, toggles, optimistic actions, infinite
scroll and sortable lists each assemble their own headers, and an approach that
hooked only the RPC path left all the others fetching anonymously.

If the frame never reports in — the customer's origin is missing from
`Origins`, their own CSP blocks the iframe, or the surface no longer exists —
the browser blocks the document before any of our code runs, so nothing inside
the frame can report it. The loader waits 20 seconds, then calls `onError` and
logs which origin was refused. Until then the customer sees an empty panel, so
that is the first thing to check when an integration "does nothing".

The nonce never appears in the frame URL, where Referer, history, logs and the
customer's analytics would all see it. It arrives by postMessage, addressed to
the exact app origin the loader read from its own `src`.

## Theming

A customer's brand tokens go in the frame URL, base64url-encoded, so the shell
links the right stylesheet in its first response rather than swapping it after
paint. They are not secret — they are brand colours.

Three things bound what a customer can set:

- `ThemeConfig.AllowTokens` names the tokens that may be overridden at all. A
  surface that names none is not re-themable, which is the default.
- `style.ApplyTokens` rejects unknown keys and validates values, so a value
  cannot escape its CSS declaration.
- `ThemeConfig.MaxVariants` (default 32) caps distinct themes per surface. Every
  distinct theme is a fresh render plus a component-CSS cache miss, so an
  uncapped registry is cheap amplification. At the cap the surface **evicts the
  least recently used variant** and serves the new one. It does not refuse: the
  shell route is unauthenticated by necessity, so refusing would let a stranger
  fill the slots and lock a customer out of their own branding for the life of
  the process. The customer's real theme arrives on the next page load and
  reclaims a slot.

A malformed or stale brand config degrades to the app theme rather than breaking
the embed.

## Programmatic mounting

For hosts that render their page after the loader runs:

```js
GoFastrEmbed.mount({
  origin: 'https://app.example.com',
  surface: 'reports',
  token: 'emb_…',
  target: '#reports',
  onError: (e) => console.warn('embed', e.type),
});
```

Same code path as the `data-*` form. The loader mounts once per target element,
so a double-mount does not create a second frame.

## Destructive actions

The allowlist makes a compromised customer, not the open internet, the
clickjacking threat model — but it does not make it zero. A destructive action
inside an embed wants an in-frame confirmation the host page cannot pre-click.

## Sizing

The frame reports its content height to the host page, which resizes the iframe
to match. The measurement is of the embed root's own extent, deliberately not of
the document's scroll height — the document's height is the frame's height, so
measuring it feeds each report into the next one.

That means **an embeddable surface must not size itself to the viewport.** A
`100vh` rule inside the frame is circular by construction: the viewport IS the
thing being sized from the measurement. `app.EmbedLayout` already neutralises
the shared full-height layout rule, but a component that asks for viewport
height — `ui.Center` with `MinHeight: "viewport"`, or your own CSS — reopens it,
and the panel grows on each report until it hits the loader's clamp. Size
embeddable surfaces by their content.

## What to give your customer

Everything above is yours. Your customer gets a `<script>` tag and no view of
your logs, your console, or the inside of the frame — that document is
cross-origin to them, so its errors are invisible on their side of the
boundary. Hand them this much along with the snippet:

- **A blank panel means the frame never loaded.** The usual cause is that
  their page's origin is not on your surface's list, and the fix is on your
  side, not theirs. Ask them for the exact origin, scheme and port included.
- **How to see the error.** The loader reports on their page, so give them
  the programmatic form rather than the bare snippet when they are debugging:

  ```html
  <script src="https://app.example.com/__gofastr/embed.js"></script>
  <script>
    GoFastrEmbed.mount({
      surface: 'reports',
      token: '…',                  // minted by you, per page load
      target: '#reports',
      onError: (e) => myErrorReporter.log('gofastr embed', e),
    });
  </script>
  ```

  `onError` fires for a refused handshake, an expired credential, and the
  20-second no-load watchdog. It is the only signal that crosses the origin
  boundary into their monitoring.
- **The token is per page load, not per site.** If they cache the page that
  carries it, every visitor served that cached copy arrives as the same
  identity for as long as the nonce lives.
- **Their own CSP has to allow the frame.** `frame-src` must include your
  origin. This one is invisible from your side entirely.

## Common mistakes

- **Reusing one nonce for every visitor.** Mint per viewer, at render time. A
  nonce hardcoded into a template is spent by the first visitor and every
  visitor after them arrives as the same identity — which is the exact failure
  single-use exists to make impossible.
- **Listing an origin with a trailing slash and expecting a subdomain to
  match.** `https://acme.com` does not cover `https://shop.acme.com`. There is
  no wildcard; list every subdomain.
- **Using `NewMemoryBurnStore` on more than one replica.** Each replica keeps
  its own map, so one nonce is spendable once per replica. Use
  `NewSQLBurnStore`.
- **Registering the embeddable screen with the site layout.** It renders with
  the header, nav and footer inside the customer's frame. Use
  `app.EmbedLayout()`.
- **Expecting the frame to see the viewer's session.** It never does, and the
  routes discard the cookie even in the same-site case. Identity comes from the
  grant, by way of `Config.Resolve`.
- **Treating the CSP allowlist as private.** Anyone who fetches the embed URL
  reads it. It is a permission list, not a secret.
- **Forgetting `embeds.Middleware()`.** The embed routes verify the grant
  themselves, but every island RPC the surface fires afterwards targets an
  ordinary app route. Without the middleware those routes see no cookie (none is
  sent) and ignore the grant, so the surface paints as its viewer and then acts
  as nobody.
- **Sizing an embeddable surface to the viewport.** See above.

## What embeds do not do yet

- **Multi-tenant entities.** An embed request carries no tenant: the middleware
  installs the grant's subject and deliberately clears every other identity
  value, including the tenant. `framework/crud` refuses a multi-tenant entity
  with no tenant on the context, so a surface over one answers an error rather
  than leaking across tenants. That is the right failure, but it means embeds
  and `tenant.WithMultiTenant` do not compose today. Resolve the tenant from
  the grant's subject in your own middleware, inside `embeds.Middleware()`, if
  you need both.
- **Server actions.** `G.serverAction` does not work inside a frame. The action
  registry is app-global with no relationship to any surface, so accepting a
  grant there would let a credential minted for one surface invoke any action
  in the app. Islands, forms and polling all work normally; only the
  `serverAction` escape hatch is closed.
- **Runtime origin management.** See [Origins](#origins) — adding a customer is
  a deploy.

## Related

- [Reactivity](reactivity.md) — the pull-first ladder island RPCs sit on.
- [Theming](theming.md) — the token set `AllowTokens` draws from.
- [Semantic search](semantic-search.md) — the `battery/semantic` package, which
  was called `battery/embed` before this feature took the name.
