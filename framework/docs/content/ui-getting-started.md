# UI getting started

Take `gofastr init` → a themed, product-specific app composed from framework primitives. This doc is the linear path: do the steps in order; you'll have a working app after every one.

The module is published — `go mod tidy` resolves it from the Go module proxy. Pin a tagged version (e.g. `v0.4.0`) rather than tracking `main`.

---

## 1. Scaffold

```bash
gofastr init myapp
cd myapp

go mod tidy   # resolves github.com/DonaldMurillo/gofastr from the module proxy
go run .
```

Only add a `replace` directive pointing at a local checkout if you are
hacking on the framework itself and want your app to pick up unreleased
changes.

Visit <http://localhost:8080> — you should see a placeholder home page served by `screens/home.go`. The CRUD entity at `/posts` works too.

Scaffold layout:

```
myapp/
  main.go            # wires entities + adaptive framework theme + UI host
  screens/home.go    # the home page you'll edit
  DESIGN.md          # product intent, hierarchy, composition, and mobile direction
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

## 2. Customize the theme

A fresh scaffold already mounts the canonical adaptive theme:

```go
import uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"

site := app.NewApp("myapp").WithTheme(uitheme.Default())
```

That default includes complete light and dark semantic palettes, so
`ui.ThemeToggle` and the OS preference work without more setup. For small brand
changes, pass `uitheme.Overrides` to `Default`.
Set `Overrides.DarkColors` alongside any brand color that should also change in
dark mode; light values are not copied automatically because their contrast may
not be safe on dark surfaces.

An app whose own styling assumes light tokens can stay light-only while it
audits dark mode:

```go
t := uitheme.Default()
t.DarkColors = nil // opt out of the adaptive dark palette
site := app.NewApp("myapp").WithTheme(t)
```

When the app needs to own the whole palette, run:

```bash
gofastr theme init
```

This writes `theme/theme.go` — a typed `style.Theme` literal with the
framework's complete light and dark defaults inline. **You own this file
forever**; the framework never regenerates it.

Then replace the canonical theme in `main.go`:

```go
import "myapp/theme"

site := app.NewApp("myapp").WithTheme(theme.App)
```

`app.WithTheme` auto-derives token names from struct paths (`Colors.PrimaryFg`
→ `--color-primary-fg`) and validates that every required token has a value.
Keep the generated `DarkColors` complete when changing brand colors; rendering
`ui.ThemeToggle` with a light-only custom theme produces an incomplete scheme.
Booting with a half-populated typed theme panics at startup with a field path
naming the missing piece.

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
unprefixed namespace for UI: set `framework.AppConfig{APIPrefix: "/api"}` (or
the `framework.WithAPIPrefix("/api")` option) — see
[entity-declarations](entity-declarations.md) → "Mounting under a prefix".

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
    db, ok := framework.DBFromContext(ctx) // the app's *sql.DB, no global handle
    if !ok {
        return errors.New("no DB on context")
    }
    rows, err := db.QueryContext(ctx, "SELECT id, name FROM foods")
    ...
}
```

This keeps screens package-portable. (`framework.WithDBContext(ctx, db)` is the
manual injector behind the middleware, useful in tests.) The package-level
handle above still works and remains the simplest path for a single app.

---

## 4. Compose a product-specific screen

Open `DESIGN.md` before selecting components. Name the primary user and task,
the dominant element on each route, the intended density and hierarchy, and
what mobile keeps, condenses, or moves. Then choose the closest framework-native
composition:

```bash
gofastr docs ui-composition-recipes
```

For example, a focused operational page can lead with one compact decision
summary and a flat signal band, then continue into the primary work region
without host-owned CSS:

```go
func (s *OverviewScreen) Render() render.HTML {
    return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide},
        ui.Stack(ui.StackConfig{Gap: ui.GapLG},
            ui.RecordSummary(ui.RecordSummaryConfig{
                Eyebrow:     "Operations · East hub",
                Title:       "12 shipments need a decision",
                Description: "Resolve today's exceptions before the carrier cutoff.",
                Highlight: ui.Callout(
                    ui.CalloutConfig{Title: "Next decision · 15:20"},
                    render.Text("Approve the alternate carrier for ORD-1842."),
                ),
                Metrics: ui.MetricBand(ui.MetricBandConfig{Items: []ui.MetricBandItem{
                    {Label: "Ready", Value: "184", Hint: "up 12 today"},
                    {Label: "At risk", Value: "12", Hint: "carrier cutoff"},
                    {Label: "Blocked", Value: "3", Hint: "owner assigned"},
                }}),
                Aside: ui.Muted(render.Text("East hub · Mina leads")),
                Actions: ui.LinkButton(ui.LinkButtonConfig{
                    Label: "Review next exception", Href: "/exceptions/ord-1842",
                }),
                Tone: ui.RecordSummaryToneWarning,
            }),
            ui.Section(ui.SectionConfig{
                Heading:     "Exceptions requiring attention",
                Description: "Prioritized by promised ship time.",
            }, exceptionTable()),
        ),
    )
}
```

The optional `Aside` becomes a bounded support rail on wide screens. The
natural-width `Actions` slot lives in that lead region and precedes the compact
aside context on phones, keeping the primary path early. Keep the opening
description to one or two sentences, keep the highlight to one decision plus
one short condition, and move the full narrative below the summary.

Use realistic content while composing; placeholder repetition hides hierarchy
and density problems. Render the page at desktop and mobile widths, inspect the
pixels, and revise `DESIGN.md` or the composition if the primary task is not
obvious. If the framework lacks a reusable primitive, layout, token, or variant,
add it upstream to `framework/ui` or `core-ui`; a GoFastr host app does not add a
local stylesheet or hand-roll structural markup.

---

## 5. Add responsive navigation

Choose the navigation primitive that matches the product shell:

- `ui.SiteHeader` provides brand, desktop navigation, actions, and its mobile
  drawer as one component. Set `MobileBrand` when the desktop identity is too
  long for the phone row.
- `ui.Sidebar` provides the desktop rail; pair the same `SidebarConfig` with
  `ui.MountSidebar` for the framework-owned mobile drawer.
- `ui.Responsive` is for cases where mobile needs a genuinely different
  priority or order, not merely narrower columns.

See [UI wiring](ui-wiring.md) for the complete mount pattern and
[UI components](ui-new-components.md) for the navigation configs. Do not
hand-roll a `<details>` menu plus route-local media-query CSS: that bypasses the
framework's navigation, focus, and mobile-drawer contract.

---

## Next

- Section-level theme overrides (`framework/ui.Themed`), dark mode, and the token catalog — see [theming](theming.md)
- Islands (in-page state changes without a route change) — the cookbook is [interactive-patterns](interactive-patterns.md) (incl. "Writing a hand-written island, end to end"); the underlying model is [runtime-contract](runtime-contract.md) "The four scenarios"
- The full `data-fui-*` primitive table is in [runtime-contract](runtime-contract.md)
- Component catalog (Layout, Card, Tooltip, Toggle, Spinner, …) is in [ui-new-components](ui-new-components.md)

For a complete worked example, read `examples/site/main.go` (route registration + widget mounts) and `examples/site/components.go` (every framework/ui component showcased).

## Primitives reference

The `framework/ui` package ships ten small primitives that cover the
boring decisions every UI makes. Each emits one stylesheet, loads
on first appearance, and is dogfooded under `/components/<slug>` on
the example website:

- `Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box` — six
  spatial wrappers covering vertical stacking, horizontal flow, CSS
  grid, centring, flex filler, and padded surface. `Cluster` wraps by default;
  set `ClusterConfig.NoWrap` only for compact chrome guaranteed to fit.
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

## Common mistakes

- **Registering a screen at an entity's CRUD path.** An entity named
  `foods` already owns `/foods` (+ `/foods/:id`, `/foods/llm.md`); a
  screen at the same path is a route-conflict panic at startup. Set
  `framework.WithAPIPrefix("/api")` so data routes move aside, or give
  screens their own noun space (`/library`, not `/foods`).
- **Unscoped selectors in a `ComponentSheet`.** `body`, `:root`,
  `::backdrop` and friends can't be scoped to
  `[data-fui-comp="…"]` — `Build()` returns `style.ErrUnscopable` and
  `MustBuild` panics at startup. Component CSS styles the component;
  page-level rules belong in the host stylesheet or
  `style.Contribute`.
- **Booting with a half-populated theme.** `WithTheme` validates every
  required token and panics naming the missing field path. Start from
  `gofastr theme init` (which writes the complete default literal) and
  edit values, rather than building a `style.Theme` from scratch.
- **Adding a `replace` directive out of habit.** The module resolves
  from the proxy; pin a tagged version. A local `replace` is only for
  hacking on the framework itself — left in, it breaks everyone else's
  build of your app.
