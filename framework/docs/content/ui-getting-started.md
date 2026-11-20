# UI getting started

GoFastr renders HTML on the server in plain Go and hydrates it in place with a small JS runtime—no React, Vue, or bundler required. This guide walks through scaffolding an app, building screens, customizing the theme, and composing real pages.

---

## 1. Scaffold and run

```bash
gofastr init myapp
cd myapp
go mod tidy
gofastr dev
```

`gofastr dev` watches your Go, CSS, JS, and HTML files, rebuilds on save, reloads the browser, and exposes dev MCP tools to your coding agent. (Plain `go run .` works for one-shot runs, but does not hot-reload.)

Open <http://localhost:8080> to see the scaffolded home screen from `screens.go`. If you kept the default sample entity, `/api/posts` returns `401` until you create an account at `/register` and sign in at `/login` (pass `--no-entity` to `gofastr init` to skip both).

What `gofastr init` writes:

```text
myapp/
├── main.go              # app wiring, theme, UI host, Start()
├── screens.go           # home screen
├── screens_auth.go      # /login and /register (skipped with --no-entity)
├── entities/entities.go # sample entity (skipped with --no-entity)
├── migrations/
├── DESIGN.md            # product hierarchy and layout notes
├── gofastr.settings.yml
├── .env
└── AGENTS.md, CLAUDE.md, .agents/   # coding-agent onboarding
```

Tip: browse every component offline with `gofastr docs ui-new-components`, or inspect exact struct fields directly from your pinned module with `go doc`:

```bash
go doc github.com/DonaldMurillo/gofastr/framework/ui CardConfig
go doc -all github.com/DonaldMurillo/gofastr/framework/ui Gap
```

---

## 2. Build a screen

A screen is a Go struct with a `Render()` method returning `render.HTML`, plus optional metadata methods (`ScreenTitle`, `ScreenDescription`, `ScreenType`).

Prefer the typed `core-ui/html` builders (`html.Div`, `html.Heading`, `html.Paragraph`) and `framework/ui` components over raw `render.Tag`—typos become compile errors instead of broken markup:

<!-- gofastr:compile
-->
```go
package screens

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type AboutScreen struct{}

func (a *AboutScreen) Render() render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow},
		ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("About")),
			html.Paragraph(html.TextConfig{}, render.Text("Built with GoFastr.")),
		),
	)
}

func (a *AboutScreen) ScreenTitle(ctx context.Context) string       { return "About" }
func (a *AboutScreen) ScreenDescription(ctx context.Context) string { return "" }
func (a *AboutScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }
```

Register it in `main.go`:

```go
site.Register("/about", &screens.AboutScreen{}, nil)
```

Cross-page links navigate client-side automatically with no full page reload once `runtime.js` is loaded.

### Reading the request context and database

When a screen needs the current user (`auth.SessionFrom(ctx)`) or database (`framework.DBFromContext(ctx)`), embed `component.ContextOnly` and implement `RenderCtx(ctx)` (and optionally `Load(ctx)` for data fetching):

```go
type DashboardScreen struct {
	component.ContextOnly
	items []string
}

func (s *DashboardScreen) Load(ctx context.Context) error {
	db, ok := framework.DBFromContext(ctx)
	if !ok {
		return errors.New("no DB on context")
	}
	// query db using ctx...
	return nil
}

func (s *DashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	sess, _ := auth.SessionFrom(ctx)
	return renderDashboard(sess, s.items)
}
```

When the app has a DB configured, `App.DBContextMiddleware()` attaches `*sql.DB` to every request automatically (`framework.WithDBContext` injects it in tests).

---

## 3. Customize the theme

Every scaffold starts with the adaptive light + dark default theme:

```go
import uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"

site := app.NewApp("myapp").WithTheme(uitheme.Default())
```

For quick brand tweaks, pass `uitheme.Overrides` (include `Dark` overrides for any brand color that should also adapt in dark mode):

```go
t := uitheme.Default(uitheme.Overrides{
	Primary: "oklch(0.55 0.18 250)",
	Dark:    &uitheme.Overrides{Primary: "oklch(0.75 0.15 250)"},
})
site := app.NewApp("myapp").WithTheme(t)
```

To own the entire token palette in plain Go, run:

```bash
gofastr theme init
```

This writes `theme/theme.go` with every light and dark token populated. **You own this file**—swap `uitheme.Default()` for `theme.App` in `main.go`, or run `gofastr theme edit` to tweak tokens live in your browser. Visit <http://localhost:8080/__gofastr/app.css> while `gofastr dev` is running to inspect the generated `:root` CSS variables.

---

## 4. Compose pages and navigation

Build screens from `framework/ui` primitives instead of writing custom CSS. For full desktop/mobile patterns, see [UI composition recipes](ui-composition-recipes.md).

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

For navigation shells:

- **Top bar (marketing / docs / app header):** run `gofastr generate package siteheader` to copy the canonical responsive header (brand, nav, CTA, theme toggle, accessible mobile drawer) into your repo as owned code.
- **App sidebar:** pair `ui.Sidebar` for desktop with `ui.MountSidebar` for the mobile drawer.

---

## Routing & path space

### Path space

If you use auto-CRUD entities, each entity claims its own URL prefix (e.g. `foods` mounts `/foods`, `/foods/:id`, and `/foods/llm.md`). Registering a UI screen on the same path fails loudly at boot.

Either put CRUD behind an API prefix (`framework.WithAPIPrefix("/api")`—the `gofastr init` default) or give screens distinct paths (`/library` for the `foods` entity).

### Dynamic routes, catch-alls, and constraints

Parameters use `:name` or `{name}` and arrive decoded in `SetParams` (omitting `SetParams` on a dynamic screen panics at startup):

```go
site.Register("/products/{slug}", &ProductScreen{}, nil)
site.Register("/orders/{id:int}", &OrderScreen{}, nil)      // :int, :uuid, :alpha, :alnum
site.Register("/docs/{path...}", &DocScreen{}, nil)         // trailing catch-all (≥1 segment)

type ProductScreen struct{ slug string }
func (s *ProductScreen) SetParams(p map[string]string) { s.slug = p["slug"] }
```

Dynamic routes match in registration order (first match wins). For optional segments, register the same component on both routes (`/{locale}/pricing` and `/pricing`).

### Redirects

Declare permanent (`308`) redirects with `Redirect` or `RedirectPattern`:

```go
site.Redirect("/old-pricing", "/pricing")
site.RedirectPattern("/legacy/{id}", "/orders/{id}")
```

Query strings are preserved, chains collapse to one hop, and targets must be relative same-app paths (open-redirect guard).

Because redirects are consulted **before** screen resolution, a redirect whose `from` overlaps a registered screen, dynamic or exact, panics at registration (and vice versa). Param names are irrelevant, so `/users/{n:int}` overlaps `/users/{id}`, while disjoint constraints (`{n:int}` vs `{h:alpha}`) coexist. A pattern redirect overlaps an exact screen the same way: `/old/{x}` covers the literal path `/old/special`, so registering both panics in either order. An exact redirect may still shadow one concrete path of a dynamic screen.

### Intercepting routes (drawers & sheets)

Use `app.InterceptFrom` when a detail route should slide over its list as a drawer during in-app navigation, while staying a full shareable SSR page on direct load or refresh:

```go
site.Register("/products", &ProductList{}, nil)
site.Register("/products/{id}", &ProductScreen{}, nil,
	app.InterceptFrom("/products", app.ScreenDrawer)) // or app.ScreenSheet
```

One `RenderCtx` serves both views. Inside an overlay, `app.OverlayOriginFromContext(ctx)` returns the page underneath so a form save can navigate back and refresh it. Up to four drawers can stack, and setting `LeaveGuard: "You have unsaved changes."` on `ui.FormConfig` warns before discarding unsaved edits.

---

## Core primitives & `ExtraAttrs`

Ten small primitives in `framework/ui` cover everyday layout and controls (each loads its own CSS on demand):

- **Layout:** `Stack`, `Cluster`, `Grid`, `Center`, `Spacer`, `Box`
- **Surfaces & media:** `Card`, `Divider`, `OptimizedImage` (`Width` + `Height` required to prevent layout shift)
- **Controls & feedback:** `Checkbox`, `Radio`, `Switch`, `FileUpload`, `Tag`, `Tooltip`, `Spinner`
- **Sandboxed embeds:** `html.IFrame` requires `Src` and `Title` and defaults to `sandbox=""` (pass `Sandbox` tokens or `Unsandboxed: true` for trusted same-app pages)

### Attribute pass-through (`ExtraAttrs`)

Every component config includes `ExtraAttrs html.Attrs` for forwarding `data-*` hooks, analytics markers, or ARIA attributes to the component's root element:

```go
ui.Card(ui.CardConfig{
	Heading:    "Revenue",
	ExtraAttrs: html.Attrs{"data-test": "revenue-card"},
})
```

Attributes owned by the component (`class`, `id`, behavior attributes like `href`/`type`/`name`, and reserved `data-cui-*` keys) are stripped automatically. Two deliberate carriers allow wiring attributes through: `LinkConfig` keeps `data-cui-*` (except `data-cui-comp`) for action links, and `ButtonConfig` accepts the typed `interactive.Action.Attrs()` keys for click RPCs.

---

## Where to go next

- [UI capability map](ui-capability-map.md): pick the right pattern for dashboards, optimistic UI, workspaces, and forms.
- [Reactivity model](reactivity.md) and [Interactive patterns](interactive-patterns.md): client signals, island RPCs, polling, and SSE.
- [Theming](theming.md) and [UI components index](ui-new-components.md): full token catalog and every built-in component.
- [UI wiring](ui-wiring.md): mounting the UI host onto a plain `framework.App` by hand.

---

## Common mistakes

- **Registering a screen at an entity's CRUD path.** Use `framework.WithAPIPrefix("/api")` or give screens their own path prefix so they don't collide at boot.
- **Forgetting `SetParams` on a dynamic route.** Any route with `:param` or `{param}` panics at startup if its screen does not implement `SetParams`.
- **Booting with a partial custom theme.** `WithTheme` validates every required token at startup; run `gofastr theme init` to start from a complete theme literal instead of constructing `style.Theme` from scratch.
- **Adding a local `replace` directive in `go.mod` out of habit.** Pin a tagged GoFastr release; only use `replace` when hacking on the framework repo itself.
