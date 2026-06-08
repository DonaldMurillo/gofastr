# UI getting started

Take `gofastr init` → a themed app with a custom-styled component in roughly 15 minutes. This doc is the linear path: do the steps in order; you'll have a working app after every one.

The framework is pre-alpha and unpublished — see step 1 for the one-time `replace` directive you'll need.

---

## 1. Scaffold

```bash
gofastr init myapp
cd myapp

# gofastr is pre-alpha and unpublished. Point go.mod at your local clone:
go mod edit -replace github.com/DonaldMurillo/gofastr=/path/to/gofastr
go mod tidy

go run .
```

Visit <http://localhost:8080> — you should see a placeholder home page served by `screens/home.go`. The CRUD entity at `/posts` works too.

Scaffold layout:

```
myapp/
  main.go            # wires entities + UI host
  screens/home.go    # the home page you'll edit
  screens/styles.go  # CSS via theme tokens + StyleSheet builder
  entities/entities.go # Go-declared CRUD entities (try /posts)
  migrations/
  .env
  .gitignore
  CLAUDE.md          # AI-agent entry point (links to AGENTS.md + skill)
  AGENTS.md          # Framework feature TOC with trigger phrases
  agents/            # Per-battery detail files linked from AGENTS.md
  .claude/skills/    # Claude Code skill for framework conventions
```

`gofastr docs` lists every framework reference doc embedded in the
binary; `gofastr docs --grep <term>` searches them. No internet needed.

---

## 2. Add a theme

```bash
gofastr theme init
```

This writes `theme/theme.go` — a typed `style.Theme` literal with the framework's defaults inline. **You own this file forever**; the framework never regenerates it.

Then wire it into `main.go`:

```go
import "myapp/theme"

// inside main(), after `site := app.NewApp("myapp")`:
site.WithTheme(theme.App)
```

`app.WithTheme` auto-derives token names from struct paths (`Colors.PrimaryFg` → `--color-primary-fg`) and validates that every required token has a value. Booting with a half-populated theme panics at startup with a field path naming the missing piece.

`go run .` — visit <http://localhost:8080/__gofastr/app.css> to confirm your tokens are emitted as `:root` custom properties.

---

## 3. Add a second screen

A screen is just a Go type implementing `component.Component`. Add `screens/about.go`:

```go
package screens

import (
    "github.com/DonaldMurillo/gofastr/core-ui/app"
    "github.com/DonaldMurillo/gofastr/core/render"
)

type AboutScreen struct{}

func (a *AboutScreen) Render() render.HTML {
    return render.Tag("div", nil,
        render.Tag("h1", nil, render.Text("About")),
        render.Tag("p", nil, render.Text("Built with GoFastr.")),
    )
}

func (a *AboutScreen) ScreenTitle() string        { return "About" }
func (a *AboutScreen) ScreenDescription() string  { return "" }
func (a *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }
```

Register it in `main.go`:

```go
site.Register("/about", &screens.AboutScreen{}, nil)
```

The `ScreenTitle/Description/Type` triple is the optional `ScreenSpec` interface — `app.Register` reads metadata from it. Cross-page nav is client-side by default (no hard reload) once `runtime.js` is on the page.

### Reaching the request context from `Render`

If a screen needs the live request context (typical case: auth-aware
chrome that branches on `auth.SessionFrom(ctx)`), implement
`RenderCtx(ctx)` and embed `component.ContextOnly` so the type still
satisfies the `Component` interface:

```go
import (
    "context"
    "github.com/DonaldMurillo/gofastr/core-ui/component"
    "github.com/DonaldMurillo/gofastr/battery/auth"
)

type AboutScreen struct {
    component.ContextOnly  // provides a stub Render() so Component is satisfied
}

func (a *AboutScreen) RenderCtx(ctx context.Context) render.HTML {
    sess, _ := auth.SessionFrom(ctx)
    return Chrome(sess, ...)
}
```

The framework prefers `RenderCtx` whenever it's defined and never
calls the stub `Render` provided by `ContextOnly`. Existing
`Render()`-only screens keep working unchanged — `ContextOnly` is just
a convenience for the ctx-aware pattern.

### Path space — avoid colliding with entity CRUD

Every entity you register automatically claims its own URL space for
REST CRUD: an entity named `foods` mounts `GET/POST /foods`, `PUT/DELETE
/foods/:id`, plus `/foods/llm.md`. If you also register a screen at `/foods`,
the two routes collide and the app panics at startup (currently the panic
surfaces as a duplicate `/<entity>/llm.md` registration — friendlier
diagnostics are on the roadmap).

The cleanest convention is to give screens their own noun space, even when
the screen *describes* an entity:

| Entity (auto CRUD)     | Matching UI screens                          |
|------------------------|----------------------------------------------|
| `foods`                | `/library`, `/library/:slug`                 |
| `triggers`             | `/my-triggers`, `/my-triggers/:id`           |
| `journal_entries`      | `/journal`, `/journal/:date`                 |

Or scope all of CRUD under an API prefix (e.g. `/api/foods`) and reserve the
unprefixed namespace for UI. A configurable `framework.AppConfig{APIPrefix:
"/api"}` is on the roadmap; until then, route groups (`App.Group("/api", …)`)
are the way.

### Accessing the database from a screen

A screen's `Render(ctx)` / `Load(ctx)` needs a way to reach the same `*sql.DB`
the framework already holds. The current idiom is a **package-level handle
captured at `main()` time**:

```go
// in package screens (or wherever your screens live)
var dbHandle *sql.DB

func Init(db *sql.DB) { dbHandle = db }
```

```go
// main.go
db := openDB()
site := app.NewApp("myapp")
site.WithDB(db)               // framework also holds it for auto-CRUD
screens.Init(db)              // hand the same handle to your screens
```

```go
// screens/library.go
func (s *LibraryScreen) Load(ctx context.Context) error {
    rows, err := dbHandle.QueryContext(ctx, "SELECT id, name FROM foods")
    ...
}
```

This is deliberately simple — a single shared handle, no DI container, no
reflection — and fine for one app, but it couples screens to that package-level
handle, which is awkward for shared screens.

**Preferred: pull the DB from the request context.** When the app has a DB
configured, the framework auto-installs `App.DBContextMiddleware()` into the
default chain, so any screen can reach the same `*sql.DB` without a global:

```go
// screens/library.go
func (s *LibraryScreen) Load(ctx context.Context) error {
    db := framework.DBFromContext(ctx)   // the app's *sql.DB, no global handle
    rows, err := db.QueryContext(ctx, "SELECT id, name FROM foods")
    ...
}
```

This keeps screens package-portable. (`framework.WithDBContext(ctx, db)` is the
manual injector behind the middleware, useful in tests.) The package-level
handle above still works and remains the simplest path for a single app.

---

## 4. Custom-styled component

A component with its own scoped CSS, lazy-loaded on first use. Add `components/statcard.go`:

```go
package components

import (
    "github.com/DonaldMurillo/gofastr/core-ui/registry"
    "github.com/DonaldMurillo/gofastr/core-ui/style"
    "github.com/DonaldMurillo/gofastr/core/render"
)

var statCardStyle = registry.RegisterStyle("stat-card", statCardCSS)

func statCardCSS(t style.Theme) string {
    return style.NewComponentSheet("stat-card", t).
        Rule("&").
            Set("display", "flex", "flex-direction", "column", "gap", "{spacing.xs}",
                "padding", "{spacing.lg}",
                "background", "{colors.surface}",
                "border", "1px solid {colors.border}",
                "border-radius", "{radii.md}").
            End().
        Rule(".label").Set("color", "{colors.text-muted}", "font-size", "0.875rem").End().
        Rule(".value").Set("color", "{colors.text}", "font-size", "1.5rem", "font-weight", "700").End().
        MustBuild()
}

type StatCardConfig struct {
    Label string
    Value string
}

func StatCard(cfg StatCardConfig) render.HTML {
    return statCardStyle.WrapHTML(render.Tag("div", nil,
        render.Tag("div", map[string]string{"class": "label"}, render.Text(cfg.Label)),
        render.Tag("div", map[string]string{"class": "value"}, render.Text(cfg.Value)),
    ))
}
```

Use it in a screen:

```go
import "myapp/components"

// inside Render():
components.StatCard(components.StatCardConfig{Label: "Users", Value: "1,247"})
```

What happened:

- `registry.RegisterStyle` returned a `*Style` handle. The CSS function only runs once per theme (cached by content hash).
- `WrapHTML` injected `data-fui-comp="stat-card"` onto the outermost `<div>`. The runtime scans the DOM after every paint/swap and inserts `<link rel="stylesheet" href="/__gofastr/comp/stat-card.css">` exactly once — even across SPA navigations.
- The CSS body is scoped to `[data-fui-comp="stat-card"]` automatically by `ComponentSheet`. Unscoped selectors (`body`, `:root`, `::backdrop`, etc.) cause `Build()` to return an error wrapping `style.ErrUnscopable` — `MustBuild` panics on it at startup.

### `Render(c)` vs `WrapHTML(html)`

- `Style.Render(c)` — pass a `component.Component`; the framework calls `c.Render()` and injects the marker. Use for components built as Go types.
- `Style.WrapHTML(html)` — pass already-built `render.HTML`. Use for components built as functions (`StatCard(cfg)`), which is the common case in `framework/ui/`.

### Co-located screen styles (`style.Contribute`)

For one-off screen styles that don't deserve a reusable component, use
`style.Contribute` to declare CSS next to the Go render code. The host's
`createStyleSheet` fans every contribution into the main theme stylesheet
at startup via `style.Apply`:

```go
// screen_home.go
var _ = style.Contribute(func(ss *style.StyleSheet) {
    ss.Rule(".home-hero").
        Set("padding", "{spacing.lg}", "background", "{colors.surface}").
        End()
    ss.Rule(".home-card").
        Set("border-radius", "{radii.md}").
        End()
})

func (s *HomeScreen) Render() render.HTML { /* uses .home-hero, .home-card */ }
```

In the host's `theme.go`:

```go
func createStyleSheet(t style.Theme) string {
    ss := style.NewStyleSheet(t)
    // ...base rules: resets, layout primitives, page chrome...
    style.Apply(ss)
    return ss.CSS() + ui.BaseCSS()
}
```

`Apply` runs after the base rules so co-located declarations can override
them by re-declaring the same selector. Final CSS is identical between
dev and prod — no nonces, no inline `<style>`, no CSP relaxation.

**When to reach for what:**

| Use case                      | Tool                              |
|-------------------------------|-----------------------------------|
| Reusable component with CSS   | `registry.RegisterStyle` + `Style.WrapHTML` (scoped, lazy-loaded per-component sheet) |
| One-off screen / page styles  | `style.Contribute` (this section — fragment added to the host's global theme stylesheet) |
| Site-wide tokens & primitives | Host `createStyleSheet` directly  |

---

## 5. Mobile hamburger nav (optional)

The runtime understands `data-fui-disclosure` on a `<details>` element — closes on SPA navigation and on Escape automatically. Use the `html.Details` shortcut:

```go
import "github.com/DonaldMurillo/gofastr/core-ui/html"

html.Details(html.DetailsConfig{Disclosure: true},
    html.Summary(html.SummaryConfig{Class: "site-nav__toggle"}, render.Text("☰ Menu")),
    html.Nav(html.NavConfig{Label: "Main"}, /* links */),
)
```

Pair with media-query CSS that hides the toggle and shows the nav inline above your mobile breakpoint. See `examples/site/styles.go` (`.site-nav` rules) for the canonical pattern.

---

## Next

- Section-level theme overrides — see `framework/ui.Themed` (and ARCHITECTURE.md "Themed sections")
- Islands (in-page state changes without a route change) — see [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) "In-page state change"
- The full `data-fui-*` primitive table is in [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md)
- Component primitive cheat sheet (Layout, Card, Tooltip, Toggle, Spinner, …) is in [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) "UI primitive cheat sheet"

For a complete worked example, read `examples/site/main.go` (route registration + widget mounts) and `examples/site/components.go` (every framework/ui component showcased).

## Primitives reference

The `framework/ui` package ships ten small primitives that cover the
boring decisions every UI makes. Each emits one stylesheet, loads
on first appearance, and is dogfooded under `/components/<slug>` on
the example website:

- `Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box` — six
  spatial wrappers covering vertical stacking, horizontal flow, CSS
  grid, centring, flex filler, and padded surface.
- `Card` — labelled `<section>` with header / body / footer slots
  and elevated / outlined / flat / interactive variants.
- `OptimizedImage` — responsive `<picture>` with `srcset`, lazy
  loading, and mandatory `Width`+`Height` (no silent CLS).
- `Checkbox` / `Radio` / `Switch` — labelled, FieldErrors-aware
  native inputs.
- `Tooltip` — CSS-only hover/focus reveal with auto-wired
  `aria-describedby`.
- `Tag` — interactive pill (filter-link or × dismiss), status-coded.
- `Spinner` — inline `role="status"` indicator with ring / dots
  variants and reduced-motion fallback.
- `Divider` — native `<hr>` for plain horizontal; `role="separator"`
  for vertical / labelled (e.g. "OR" between options).
- `FileUpload` — drag-drop zone over a native `<input type="file">`
  via `data-fui-fileupload`.

The matching widget preset is `preset.Popover` — a click-triggered
anchored surface without backdrop dim or focus trap. Closes on ESC
and click-outside.
