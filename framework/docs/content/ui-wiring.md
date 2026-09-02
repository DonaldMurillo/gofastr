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

```go
package main

import (
	"log"
	"net/http"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
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

	// Layout + screens. The sidebar config is shared: Sidebar(cfg)
	// renders the desktop rail, MountSidebar registers the mobile
	// drawer widget for the same items.
	sbCfg := ui.SidebarConfig{Title: "myapp", Items: []ui.SidebarItem{{Label: "Home", Href: "/"}}}
	layout := uiapp.NewLayout("app").WithSidebar(ui.Sidebar(sbCfg))
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

**`NewLayout`** is the shared chrome. `WithSidebar` gives the
sidebar-rail app shell; `WithContainer` gives a centered column;
`WithHeader` / `WithFooter` accept components (use
`app.NewContextComponent` for auth-aware chrome that needs the
request context). A layout is passed per `Register` call, and
`SetDefaultLayout` covers screens registered with a nil layout.
Layouts nest through `app.NewScreenGroup`: a section keeps its own
sidebar inside the app shell, and navigation swaps only the layers
that change. See [layouts](layouts.md) for the chain model.

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
- `uihost.WithCustomCSS(css)`: append site CSS (e.g. `@font-face`
  rules) to `app.css`; combine with
  `uihost.ReadCustomCSSFile("static/app.css")` for a file you edit
  without recompiling.
- `uihost.WithNoLiveChannel()`: omit the live-channel meta from every
  page, so the runtime never opens the shared SSE bus. For hosts (or
  test rigs waiting on network-idle) that want zero held connections;
  polling (`data-fui-poll`) and islands still work.
- `uihost.WithNotFoundScreen(c)`, `WithFavicon`, `WithDescription`,
  `WithOpenGraph`, `WithCanonicalURL`: 404 page and head metadata.
  Custom 404s go through `WithNotFoundScreen`, never
  `app.Router().NotFound(...)`: screens dispatch through the router's
  NotFound fall-through, so replacing it disables every page (the
  router now warns when a NotFound handler is replaced; compose with
  `Router.WrapNotFound` when both must run).

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

`RenderScreen` renders; it authorizes nothing and mints no session.
The caller owns the decision, and with it the status:

| Situation | Answer |
|---|---|
| Authentication failed on a route that exists | 401 / 403 via `RenderScreen` |
| Route resolved, resource gone or not found for this caller | 410, or a 404 body with `ScreenStatusCode` on the screen itself |
| No route matches | nothing — the `WithNotFoundScreen` 404 stays truthful |

Render the same recovery screen for a missing and an expired session:
an unauthorized caller must not learn from the difference whether a
session exists.

Both full and client-side navigation (`X-Gofastr-Navigate`) requests
get the same status and the same `Cache-Control: private, no-store`
(the default; `ScreenResponse.CacheControl` overrides it). The partial
arm carries bare content — the runtime surfaces a non-2xx partial as a
navigation error and stays on the current page, a full load renders
the branded page. See [security](security.md) → "Recovery screens and
cache policy" for why the no-store default is not optional.


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
- **Skipping the `routerMounter` shim and wondering why the sidebar
  drawer never opens on mobile.** `ui.Sidebar(cfg)` only renders the
  desktop rail; the `< md` drawer is a widget that must be mounted
  once via `ui.MountSidebar(routerMounter{fwApp.Router()}, cfg)` with
  the *same* config value.
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
