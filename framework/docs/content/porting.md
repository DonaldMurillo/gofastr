# Porting an app with an existing frontend

Sometimes the job is not "build a GoFastr app" but "move an existing app
onto GoFastr without breaking its DOM contract" — a frontend whose tests
pin attributes like `data-slot="card-title"`, whose CSS is a compiled
design system, whose markup the framework's components will never emit.

That is possible today, and this page shows the pieces. It is also
**deliberately not a supported use case**: the framework has one styling
surface (`framework/ui` + the theme tokens), and everything on this page
steps outside it. The contracts pipeline, the design system, and the
upgrade notes make no promises about markup and stylesheets you bring
yourself. You own the drift.

## A screen is anything that returns render.HTML

There is no special template feature to enable. A screen component's
render is a `render.HTML` string; executing an `html/template` into one
is plain Go:

<!-- gofastr:compile
stmt: _ = render.HTML(buf.String())
import "bytes"
import "html/template"
import "github.com/DonaldMurillo/gofastr/core/render"
-->
```go
var page = template.Must(template.New("invoice").Parse(
	`<article data-slot="invoice"><h1>{{.Title}}</h1></article>`))

var buf bytes.Buffer
if err := page.Execute(&buf, struct{ Title string }{"March"}); err != nil {
	panic(err)
}
// buf.String() is the screen's markup, verbatim.
```

Embed `.tmpl` files instead of inline strings — the markup stays
reviewable as markup, and designers can edit it without touching Go:

```go
import (
	"embed"
	"html/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

var pages = template.Must(template.ParseFS(templatesFS, "templates/*.tmpl"))
// pages.ExecuteTemplate(&buf, "invoice.tmpl", data) as above.
```

The executed HTML renders inside the App layout like any other screen,
so navigation, sessions, and auth all still work.

`html/template` escapes interpolated data contextually; that part of the
safety story survives the port. What does not survive: none of the
framework's UI guarantees (theming, dark mode, a11y patterns, the
runtime's island wiring) apply to markup it did not emit.

## Serving the foreign design system

A compiled stylesheet, fonts, and static JS mount without adopting any
of `uihost`:

```go
static.Mount(app.Router(), static.Config{FS: assets, Prefix: "/static"})
```

Reference them from your templates like any static asset. See
[core packages](core-packages.md) for `core/static`'s cache headers and
fingerprinting.

## Where the line is

- Data layer, auth, routing, jobs, CRUD APIs: fully supported — nothing
  on this page changes how the backend works.
- Attributes on framework components: supported. If you only need extra
  `data-*` / ARIA attributes on components you otherwise adopt, use
  `ExtraAttrs` (see [UI getting started](ui-getting-started.md)) — that
  path IS covered by the contract and its tests.
- Bespoke markup + bespoke CSS via this page: possible, unsupported.
  `gofastr verify` will flag bespoke-CSS and inline-style patterns in
  app code; keep ported surfaces in clearly separated packages so the
  findings stay legible, and expect to re-verify the frontend yourself
  on every framework upgrade.

If a port later migrates surfaces onto `framework/ui`, each migrated
screen rejoins the supported world one at a time — the two styles can
coexist during the transition because both are just `render.HTML`.

## Keeping a page's own scripts alive

A ported page whose behavior is built at script load breaks under soft
navigation: the runtime swaps the content without re-running the
destination's scripts, so every handler the old page bound is gone. Two
opt-outs, at different grains.

`Screen.NoSPA` excludes a route from the client route manifest, so the
runtime treats it as unknown and *every* link to it is a full document
load:

```go
legacy := app.NewScreen("/reports", &ReportsScreen{})
legacy.NoSPA = true
site.RegisterScreen(legacy, appLayout)
```

`data-fui-nav="off"` does the same for one link, when the destination is
otherwise a normal SPA route:

```go
ui.Link(ui.LinkConfig{Href: "/reports", Text: "Reports",
    ExtraAttrs: html.Attrs{"data-fui-nav": "off"}})
```

Reach for the screen-level form when the page can never be soft-loaded;
the per-link form when one entry point is special.

## Rendering a screen from a route you own

`UIHost.PageHandler(path)` returns an `http.HandlerFunc` that renders a
registered screen as a full page, chrome included, from a route mounted
on the framework router:

```go
r.Get("/legacy/{path...}", site.PageHandler("/legacy"))
```

Use it when a wildcard subtree claims a path the normal screen dispatch
never resolves — the mux redirects the bare path to a trailing-slash
form, and the NotFound screen answers instead of the screen you
registered. A dynamic pattern (`/thing/{id}`) is passed through rather
than forced, so param capture still reads the real request path.

## Answering with a status other than 200

A screen whose route resolved but whose record is gone should render a
body *and* say 404. Implement `ScreenStatusCode`:

```go
func (s *ThingScreen) ScreenStatusCode() int {
    if s.thing == nil {
        return http.StatusNotFound
    }
    return 0 // 0 or 200 keeps the default
}
```

The body still renders through the layout, so the user gets the real
not-found page while crawlers and clients get the real status.

## Common mistakes

- **Inline `template.Must` strings scattered through Go files.** Embed
  `.tmpl` files with `go:embed` + `template.ParseFS`. One real port
  accumulated 40 files of inline strings; the markup became invisible
  to every markup tool.
- **Expecting framework/ui components to emit a foreign DOM.** They
  emit their own DOM and always will; `ExtraAttrs` attaches attributes,
  it does not reshape markup. If the test suite pins someone else's
  structure, render that structure from templates instead.
- **Adopting half of `uihost` for the styling.** If the port brings its
  own design system, serve it with `static.Mount` and skip the
  framework's styling surface entirely on those screens — mixing the
  two on one surface means neither owns the result.
- **Treating this page as the recommended path.** It is the escape
  hatch. A new app, or a port free to change its DOM, should build on
  `framework/ui` and get theming, dark mode, and a11y for free.
