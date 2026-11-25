# UI wiring: adding the UI system to a `framework.App`

`gofastr init` generates this wiring automatically. Use this guide when adding server-rendered screens to an existing `framework.App` by hand, or when customizing a generated `main.go`.

Three pieces connect the HTTP server to your UI:

| Object | Package | Owns |
|---|---|---|
| `framework.App` | `framework` | DB, entities, auto-CRUD, middleware, HTTP router |
| `app.App` (the "site") | `core-ui/app` | Screens, layouts, theme (no HTTP knowledge) |
| `uihost.UIHost` | `framework/uihost` | Bridge that mounts screens, `runtime.js`, manifest, and `/__gofastr/app.css` onto `framework.App` |

## Minimal runnable `main.go`

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

type HomeScreen struct{}

func (h *HomeScreen) Render() render.HTML {
	return render.Tag("h1", nil, render.Text("Hello"))
}

// Three-line shim adapting framework's router to ui.WidgetMounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) { widget.Mount(m.r, def) }

var sbCfg = ui.SidebarConfig{Title: "myapp", Items: []ui.SidebarItem{{Label: "Home", Href: "/"}}}

func buildAppShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(sbCfg))
	return html.Div(html.DivConfig{Class: "app-shell"},
		html.Nav(html.NavConfig{Label: "Main"}, nav),
		l.Primary(),
	)
}

func main() {
	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))

	site := uiapp.NewApp("myapp")
	theme := style.DefaultTheme()
	theme.Colors.Primary.Value = "#0E7C86"
	site.WithTheme(theme)

	layout := uiapp.NewLayout("app", uiapp.LayoutSpec{}, buildAppShell)
	site.SetDefaultLayout(layout)
	site.Register("/", &HomeScreen{}, layout)
	ui.MountSidebar(routerMounter{fwApp.Router()}, sbCfg)

	fwApp.Mount(uihost.New(site))

	if err := fwApp.Start("localhost:8080"); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

If entity CRUD routes and screens would collide on `/`, pass `framework.WithAPIPrefix("/api")` to `NewApp`.

## Useful `uihost.New` options

- **`uihost.WithStaticDir("static")` / `uihost.SetStaticFS(fsys)`:** Serves static files with `os.Root` symlink containment (escapes fall through rather than being served), dotfile/server-config refusal (`/.well-known/` allowed), and directory fall-through so an `assets/` or `docs/` folder never shadows a screen. Use `host.AssetURL("/logo.svg")` for immutable content-hashed URLs (`?v=<hash>`).
- **`uihost.WithCustomCSS(css)` / `uihost.ReadCustomCSSFile(path)`:** Appends site CSS (such as `style.FontFaceCSS`) to `app.css`; use root-relative `/fonts/...` URLs.
- **`uihost.WithNoLiveChannel()`:** Omits the `/__gofastr/sse` meta tag so pages hold zero SSE connections (polling and islands keep working).
- **`uihost.WithTrustedProxies("10.0.0.0/8", ...)`:** Overrides `AppConfig.TrustedProxies` for per-client SSE caps and `X-Forwarded-Proto` scheme detection.
- **`uihost.WithNoInlineFirstPaint()`:** Keeps head styles/scripts linked instead of inlined with CSP hashes (use when an external proxy/CDN overwrites CSP).
- **`uihost.WithNotFoundScreen(c)`**, **`WithErrorScreen(c)`**, **`WithFavicon`**, **`WithDescription`**, **`WithOpenGraph`**, **`WithCanonicalURL`:** Branded 404/500 pages and head metadata. Implement `uihost.NotFoundTitler` on a custom 404 screen for request-specific titles. Always configure 404s through `WithNotFoundScreen` (not `app.Router().NotFound`, which replaces screen dispatch).
- **Hiding restricted routes as 404:** Return `decide.NotFound()` from a screen/group policy (or call `host.ServeNotFound(w, r)` in middleware) to render the standard 404 page/partial/problem JSON without leaking the route's title or metadata. Set `NoSPA` on screens whose route pattern itself must stay out of `/__gofastr/manifest.js`.

## Guards & recovery screens on dynamic routes

When HTTP middleware needs a screen's route parameters before the screen renders—or needs to return a branded error/recovery page (401, 403, 410)—wire `host.RouteMatchMiddleware()` before your guard and call `host.RenderScreen`:

```go
host := uihost.New(site)
fwApp.Use(host.RouteMatchMiddleware()) // populates uiapp.MatchFromContext
fwApp.Use(auth.SessionMiddleware(mgr))
fwApp.Mount(host)
```

```go
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

- `RenderScreen` serves the branded layout on full loads (always `Cache-Control: no-store`, `Vary: Cookie`) and a bare error partial (`private, no-store`, never minting a session) on `X-Gofastr-Navigate` requests.
- Both responses use the recovery screen's trimmed `ScreenLang` override when it supplies one. Otherwise they use the host's document language for the route.
- Return the same recovery screen and status for missing and expired sessions so callers cannot probe session existence.

## Common mistakes

- **Registering screens after `fwApp.Mount(uihost.New(site))`.** Register all screens and layouts first, then mount `uihost` last so screen actions, `llm.md` routes, and the NotFound catch-all wire cleanly.
- **Posting a form directly to a screen path with no POST handler.** Screens answer `GET`/`HEAD` only; `POST`/`PUT`/`PATCH`/`DELETE` return `405 Method Not Allowed`. Mount a sibling `app.Router().Post("/contact", handleContact)` (GET still renders the screen), or use `host.PageHandler("/x")` / `host.RenderScreen` if a POST should re-render the screen with validation errors.
- **Forgetting `ui.MountSidebar` (or `ui.MountSidebarFunc` for per-request sidebars).** `ui.Sidebar` renders only the desktop rail; the mobile drawer widget won't open unless mounted once via `routerMounter`.
- **Colliding screen paths with entity CRUD paths.** Set `framework.WithAPIPrefix("/api")` when screens and entities share names like `/posts`.
- **Replacing `app.Router().NotFound` directly.** That bypasses `uihost` screen dispatch—use `uihost.WithNotFoundScreen` (or `Router.WrapNotFound`).
