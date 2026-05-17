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
  entities/          # CRUD entities (try /posts)
  migrations/
  .env
```

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

Pair with media-query CSS that hides the toggle and shows the nav inline above your mobile breakpoint. See `examples/website/theme.go` (`.site-nav` rules) for the canonical pattern.

---

## Next

- Section-level theme overrides — see `framework/ui.Themed` (and ARCHITECTURE.md "Themed sections")
- Islands (in-page state changes without a route change) — see [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) "In-page state change"
- The full `data-fui-*` primitive table is in [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md)
- Component primitive cheat sheet (Layout, Card, Tooltip, Toggle, Spinner, …) is in [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) "UI primitive cheat sheet"

For a complete worked example, read `examples/website/main.go` (45 lines of wiring) and `examples/website/screen_framework_ui.go` (one screen using every framework/ui component).

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
