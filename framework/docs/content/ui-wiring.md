# UI wiring: adding the UI system to a framework.App

`gofastr init` writes this wiring for you. This doc is for the other
case: you have a plain `framework.App` (entities, CRUD, batteries) and
want to add server-rendered screens to it by hand, or you're reading a
generated `main.go` and want to know what each line is for.

This doc covers three objects and the bridge between them:

| Object | Package | Owns |
|---|---|---|
| `framework.App` | `framework` | DB, entities, auto-CRUD, middleware, the HTTP router |
| `app.App` (the "site") | `core-ui/app` | Screens, layouts, theme: no HTTP knowledge |
| `uihost.UIHost` | `framework/uihost` | The bridge: mounts the site's page routes, `runtime.js`, and `/__gofastr/app.css` onto the framework router |

## The whole thing

This is a complete, runnable `main.go` (it compiles as-is against the
current module):

<!-- gofastr:compile
-->
```go
package main

import (
	"context"
	"log"
	"net/http"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// HomeScreen is a screen: any type whose Render returns render.HTML.
type HomeScreen struct{}

func (h *HomeScreen) Render() render.HTML {
	return render.Tag("h1", nil, render.Text("Hello"))
}

// routerMounter adapts the framework router to ui.WidgetMounter so
// framework/ui helpers (MountSidebar, ConfirmAction, …) can register
// their widget routes. Three lines, copy as-is.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) { widget.Mount(m.r, def) }

// The sidebar config is shared: the shell renders the desktop rail
// from it, MountSidebar registers the same items' mobile drawer.
var sbCfg = ui.SidebarConfig{Title: "myapp", Items: []ui.SidebarItem{{Label: "Home", Href: "/"}}}

// buildAppShell is the layout: the sidebar rail as static chrome
// beside l.Primary(), where each route's screen renders.
func buildAppShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(sbCfg))
	return html.Div(html.DivConfig{Class: "app-shell"},
		html.Nav(html.NavConfig{Label: "Main"}, nav),
		l.Primary(),
	)
}

func main() {
	// The framework app: DB, entities, auto-CRUD, middleware, router.
	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))

	// The UI app: screens, layouts, theme. It knows nothing about
	// HTTP until uihost bridges the two below.
	site := uiapp.NewApp("myapp")

	// Theme: start from the default, override token values.
	theme := style.DefaultTheme()
	theme.Colors.Primary.Value = "#0E7C86"
	site.WithTheme(theme)

	// Layout + screens: the app shell — a tree layout whose static
	// chrome is the sidebar rail around the primary slot.
	layout := uiapp.NewLayout("app", uiapp.LayoutSpec{}, buildAppShell)
	site.SetDefaultLayout(layout)
	site.Register("/", &HomeScreen{}, layout)
	ui.MountSidebar(routerMounter{fwApp.Router()}, sbCfg)

	// uihost is the bridge: mounts SSR page routes, runtime.js, and
	// /__gofastr/app.css (theme tokens) onto the framework app.
	fwApp.Mount(uihost.New(site))

	if err := fwApp.Start("localhost:8080"); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

Run it and you get `/` fully server-rendered, client-side navigation
between registered screens (no hard reloads), the theme emitted as
`:root` custom properties at `/__gofastr/app.css`, and every mounted
widget auto-bootstrapped by the runtime.

## Line by line

**`framework.NewApp`** builds the data/HTTP spine. Register entities
and batteries on it exactly as you would in an API-only app; UI
wiring doesn't change any of that. If screens and entity CRUD both
want the root namespace, move CRUD aside with
`framework.AppConfig{APIPrefix: "api"}` (see
[ui-getting-started](ui-getting-started.md) → "Path space").

**`uiapp.NewApp`** builds the site object. Everything about *what*
pages exist hangs off it: `Register` (path → screen → layout),
`SetDefaultLayout`, `WithTheme`. It holds no router and opens no
port.

**`WithTheme`** takes a `style.Theme`. Start from
`style.DefaultTheme()` (or `framework/ui/theme.Default(...)`) and
override token values; see [theming](theming.md). `WithTheme`
validates the theme and panics at startup on a missing token, naming
the field path.

**`NewLayout`** is the shared chrome: a build function over the
layout's placement points — static markup (`ui.Sidebar`, the app's
own header and footer packages, whatever the shell composes) around
`l.Primary()`, where each route's screen renders. The build receives
the live request context, so auth-aware chrome reads it directly (or
wrap a component with `app.NewContextComponent`). A layout is passed
per `Register` call, and `SetDefaultLayout` covers screens registered
with a nil layout. Layouts nest through `app.NewScreenGroup` — a
section keeps its own layer inside the app shell — and screens and
groups fill a layout's named outlets with `Screen.Fill` /
`ScreenGroup.Fill`. See [layouts](layouts.md) for outlets, fills, and
the chain model.

**`routerMounter`** is the one piece of glue you write yourself.
`framework/ui` helpers that need to register widget routes
(`MountSidebar`'s mobile drawer, `ConfirmAction`'s modal) take a
`ui.WidgetMounter` instead of importing the router, so the package
stays router-agnostic. The three-line shim above is the whole
adapter; `examples/meridian/app.go` ships the identical type.

**`uihost.New(site, opts...)`** wraps the site in a `Mountable`.
`fwApp.Mount` installs it: every registered screen becomes a GET
route rendering the full SSR page, and the host serves `runtime.js`,
the route manifest for client-side nav, and `/__gofastr/app.css`.
Once mounted, in-page dynamic behavior (sort, paginate, form-submit
without a reload) is built from islands; the cookbook is
[interactive-patterns](interactive-patterns.md).
Useful options:

- `uihost.WithStaticDir("static")`: serve a static asset directory.
  Reads are contained by an `os.Root` over the directory: a symlinked
  file or subdirectory pointing outside it is refused (the request
  falls through to page rendering) rather than served. Symlinks that
  stay inside the directory keep working. A path with a dot segment
  (`/.env`, `/.git/config`, `/assets/.htpasswd`) or a server config
  file name (`web.config`, `global.asax` and their kin, any case) is
  never served, the set `core/static` refuses; it falls through to
  page rendering. Files under a leading `/.well-known/` are served
  unless they are dotfiles themselves.
- `uihost.SetStaticFS(fsys)`: serve static files from an `fs.FS`
  instead of a directory (the embed route). A value that came from
  `os.DirFS(dir)` is re-rooted through `os.OpenRoot(dir).FS()` at
  installation, so reads are kernel-contained exactly like
  `WithStaticDir`'s: a symlink inside `dir` pointing outside it is
  refused (the request falls through to page rendering) rather than
  served. `embed.FS` values pass through unchanged. A custom `fs.FS`
  you write yourself must not follow symlinks out of its tree —
  `os.Root.FS()` is the spelling for a disk-backed one; `embed.FS`
  cannot carry symlinks at all. The dotfile and config-file refusal
  applies here too. Only files are served: a directory in the FS falls
  through to page rendering, as it does under `WithStaticDir`, so a
  `docs/` directory of images never shadows a `/docs` screen (and a
  directory with no screen answers 404, never a redirect or a listing).
- `host.AssetURL("/logo.svg")`: the URL of a static file with its
  content hash appended (`/logo.svg?v=3f2a9c01b7d4`). Use it wherever a
  page links a file from the static directory or static FS. A request
  whose `v` matches the file's current hash is served with
  `Cache-Control: public, max-age=31536000, immutable`, so browsers and
  CDNs keep the file until it changes, and a deploy that changes it
  changes the URL. A plain or stale URL keeps the default revalidating
  behavior, and `gofastr dev` still serves every static file `no-store`.
  The hash is cached per file and taken again when its modification
  time or size changes. A path that is not clean and rooted, or names
  no file, comes back unchanged.
- `uihost.WithCustomCSS(css)`: append site CSS (e.g. `@font-face`
  rules) to `app.css`; combine with
  `uihost.ReadCustomCSSFile("static/app.css")` for a file you edit
  without recompiling. Give URLs in it from the root (`/fonts/a.woff2`):
  a relative one keeps `app.css` linked, since inlined it would resolve
  against the page's address.
- `uihost.WithNoLiveChannel()`: omit the live-channel meta from every
  page, so the runtime never opens the shared SSE bus. For hosts (or
  test rigs waiting on network-idle) that want zero held connections;
  polling (`data-cui-poll`) and islands still work.
- `uihost.WithTrustedProxies("10.0.0.0/8", …)`: the reverse proxies
  (IPs or CIDRs) whose `X-Forwarded-For` hop the island stream's
  per-client cap reads. Without it, behind a proxy, every anonymous
  visitor counts as the proxy's address and shares its 256 streams. See
  `gofastr docs reactivity`.
- `uihost.WithNoInlineFirstPaint()`: keep the color-scheme bootstrap,
  the component bundle and app.css as links on every page. By default a
  first-load page whose head fits 16 KiB compressed carries them inline,
  or, when the whole bundle is too heavy, the rules its markup can match
  (the full sheet then loads after first paint), allowed by hashes the
  host adds to the response's CSP; set this when
  the CSP is written by a proxy, a CDN or a `<meta>` tag the host cannot
  patch, or when most full loads are return visits (a dashboard used all
  day): linked sheets come from the cache, inlined ones arrive again in
  every document. See `gofastr docs security`.
- `uihost.WithNotFoundScreen(c)`, `WithFavicon`, `WithDescription`,
  `WithOpenGraph`, `WithCanonicalURL`: 404 page and head metadata.
  Custom 404s go through `WithNotFoundScreen`, never
  `app.Router().NotFound(...)`: screens dispatch through the router's
  NotFound fall-through, so replacing it disables every page (the
  router now warns when a NotFound handler is replaced; compose with
  `Router.WrapNotFound` when both must run). The custom screen renders
  behind panic containment: if its render panics, the default 404 page
  is served instead. (A registered screen's render panic is answered
  differently: 500 plus a logged error line, because a broken screen
  is a server bug. The 404 screen only runs for paths no route owns,
  and those must stay 404s, not become incidents.)
  A custom screen may implement `uihost.NotFoundTitler`, whose
  `NotFoundTitle(ctx context.Context, path string) string` returns its
  request-specific title. The context carries the reader's locale; the
  path is empty for a missing outlet. Keep the shared screen stateless.
  An empty result falls back to `app.ScreenTitler.ScreenTitle(ctx)`,
  then the framework's generic not-found title. A panicking title provider
  also uses the generic title. Full pages, navigation partials and the
  bare page an app without a default layout serves all carry this title
  through the normal escaping and app-name suffix rules, rather than
  borrowing the failed route's title.
  An unmatched route retains its request path even if the root layout has
  a `FallbackNotFound` outlet; it is already rendering the not-found page.

**`fwApp.Start`** runs migrations, binds the port, serves. Nothing
UI-specific.

## Guards on dynamic screens

Middleware that guards a dynamic screen (`/session/{sessionId}`) needs
two things the plain wiring doesn't give it: the route parameters
before the screen renders, and a way to answer with a real page when
the guard fails.

**Route parameters: `host.RouteMatchMiddleware()`.** It resolves every
request path against the site's screen router and stores the result as
an `app.Match` on the request context. Register it *before* the
middleware whose guards read the parameters (`Router.Use` runs the
first-registered middleware outermost, and `fwApp.Use` appends):

```go
host := uihost.New(site)
fwApp.Use(host.RouteMatchMiddleware()) // before the guards
fwApp.Use(auth.Session(...))           // your auth middleware
fwApp.Mount(host)
```

The guard reads the match instead of re-parsing the path:

```go
match, ok := uiapp.MatchFromContext(r.Context())
if ok {
    sessionID := match.Param("sessionId")  // "abc" for /session/abc
    screen := match.ScreenID()             // "/session/:sessionId"
}
```

`Param` returns the same values the screen's `SetParams` receives,
trailing slash included (`/session/abc/` matches too). The match is
read-only: no accessor hands out the parameter map. A path no screen
matches carries no match — let the request fall through so unknown
routes keep their truthful 404. Screen policies see the same match
without any middleware: the host populates it before the policy chain
runs.

**Recovery screens: `host.RenderScreen`.** A guard that fails answers
with a branded page through the normal chrome (default layout,
runtime.js, theme) and the status you name, instead of middleware
plain text or a sentinel path:

```go
// SessionGoneScreen is a normal component; register nothing for it.
type SessionGoneScreen struct{}

func (s *SessionGoneScreen) Render() render.HTML {
    return render.Tag("h1", nil, render.Text("This session has ended"))
}

func guard(host *uihost.UIHost, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        match, ok := uiapp.MatchFromContext(r.Context())
        if ok && sessions.Expired(match.Param("sessionId")) {
            host.RenderScreen(w, r, &SessionGoneScreen{}, uihost.ScreenResponse{
                Status: http.StatusGone,
            })
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

`RenderScreen` renders; it authorizes nothing. The caller owns the
decision, and with it the status:

| Situation | Answer |
|---|---|
| Authentication failed on a route that exists | 401 / 403 via `RenderScreen` |
| Route resolved, resource gone or not found for this caller | 410, or a 404 body with `ScreenStatusCode` on the screen itself, or `app.SetStatus` from the render that found it missing |
| No route matches | nothing — the `WithNotFoundScreen` 404 stays truthful |

Render the same recovery screen for a missing and an expired session:
an unauthorized caller must not learn from the difference whether a
session exists.

Both full and client-side navigation (`X-Gofastr-Navigate`) requests
get the same status. The two arms differ in what else they carry:

| Arm | Session | Cache policy | Body |
|---|---|---|---|
| Full load | Verified, or minted when the cookie is missing or dead (the page tail every screen runs: a direct load is a real page whose islands need a live id) | Always `no-store` + `Vary: Cookie`; `ScreenResponse.CacheControl` does not change it | The branded page in the default layout, with the signal seed |
| Navigation partial | Never minted: a bare auth-failure body hands out no grant | `private, no-store` by default; `ScreenResponse.CacheControl` overrides it | The bare component, its title (`X-Gofastr-Title`), head carrier and `#gofastr-signals-partial` seed, as a page partial carries them |

The runtime surfaces a non-2xx partial as a navigation error and stays
on the current page; a full load renders the branded page. See [security](security.md) → "Recovery screens and
cache policy" for why the no-store default is not optional. Every
partial-shaped response carries it, including the early exits: the
dead-session prefetch 204 and a policy redirect's 303 leave the server
marked no-store before any branch can return.


## Common mistakes

- **Registering screens after `fwApp.Mount(uihost.New(site))`.**
  Pages still render (the host resolves screens at request time), but
  work the host does once at mount, compiling screen actions and
  per-screen `llm.md` routes, misses late registrations. Register
  everything on `site` first, then `Mount`; the framework's own
  `Mount` doc says the same: mount the UI host last, since it claims
  the router's NotFound catch-all.

> **405 trap (fixed).** Before the `MethodNotAllowed` hook, registering a
> non-GET route at a path that also had a screen (e.g. `POST /shipments`
> from an entity action) made `GET /shipments` return a bare text 405
> instead of rendering the screen: the router's method-mismatch path
> fired *before* the NotFound catch-all. The uihost now installs a
> `Router.MethodNotAllowed` handler that delegates to `serveOrRender`
> for safe methods (GET/HEAD) when a static file or screen resolves at
> the path, so screens survive POST-only siblings. Genuinely
> unsupported methods get a styled 405 page preserving the Allow
> header. `Router.MethodNotAllowed` works just like `Router.NotFound`:
> the middleware chain wraps the handler at request time, and the
> router sets the RFC-compliant Allow header (filtered to exclude
> gated methods) before dispatching.
- **Posting a form to a screen's own path.** A screen answers GET and
  HEAD only. A POST, PUT, PATCH or DELETE to a screen path that no
  registered route takes answers **405** with `Allow: GET, HEAD` and the
  kit's 405 document (`no-store`; an RFC 9457 problem document for an
  `application/problem+json` Accept), and OPTIONS answers **204** with
  the same `Allow`. Nothing of the screen's runs: no policy, no `Load`,
  and no session is minted (a live cookie is read, so the document's
  chrome keeps the caller's stream; without one the chrome is
  session-less, like a static export). Before this a POST rendered the
  page with a 200, ran its `Load` and minted a session. A form posts to
  a handler: `r.Post("/contact", handleContact)` beside the `/contact`
  screen wins its method while GET still renders the screen, and the
  405 for a third method lists both sets
  (`Allow: GET, HEAD, POST`). A host that wants the page rendered for a
  POST registers it explicitly, `r.Post("/x", host.PageHandler("/x"))`,
  or answers from its handler with `RenderScreen` (the
  re-render-with-errors shape). A path that is no screen keeps its
  404 for every method; a declared redirect keeps its 308, method and
  all, and the trailing-slash 301 still answers first.
- **Skipping the `routerMounter` shim and wondering why the sidebar
  drawer never opens on mobile.** `ui.Sidebar(cfg)` only renders the
  desktop rail; the `< md` drawer is a widget that must be mounted
  once via `ui.MountSidebar(routerMounter{fwApp.Router()}, cfg)` with
  the *same* config value. When the layout builds the sidebar per
  request (`ui.Sidebar(sidebarConfig(ctx))`, because the footer shows
  Sign out to a signed-in user), mount the drawer with the same builder:
  `ui.MountSidebarFunc(routerMounter{fwApp.Router()}, sidebarConfig)`.
  A config built once with `context.Background()` renders the
  anonymous footer in the phone drawer for everyone.
- **Registering a screen at an entity's CRUD path.** An entity named
  `posts` already owns `/posts`; a screen at the same path is a
  route-conflict panic at startup. Set an `APIPrefix` or give screens
  their own noun space.
- **Writing page chrome by hand instead of using a layout.** Headers,
  footers, sidebars, and the centered container are layout concerns:
  `app.NewLayout` + `framework/ui` components cover them with zero
  bespoke CSS. An app that ships its own shell markup ends up
  duplicating the design system (see the styling rules in
  [ui-getting-started](ui-getting-started.md)).
