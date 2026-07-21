# UI components — index

This is a one-page index of every UI component the framework ships.
The canonical reference for any given component is its **live demo
page** at `/components/<slug>` plus the Go doc comments on the
component's `Config` / constructor — those stay current automatically
because each release lands the demo + the code together.

---

## Where to look for what

| You want…                            | Look at                                                          |
| ------------------------------------ | ---------------------------------------------------------------- |
| Live behavior (click it, drag it)    | Run the website: `./scripts/dev-watch.sh` → `/components/<slug>` |
| Constructor signature + every field  | `go doc github.com/DonaldMurillo/gofastr/framework/ui.<Name>`    |
| Full-page composition choice          | `gofastr docs ui-composition-recipes`                           |
| Pattern packages (Combobox, Tree, …) | `go doc github.com/DonaldMurillo/gofastr/core-ui/patterns/<pkg>` |
| Widget presets (Modal, Drawer, …)    | `go doc github.com/DonaldMurillo/gofastr/core-ui/widget/preset`  |
| Runtime data-fui-\* attributes       | [runtime-contract](runtime-contract.md)                          |
| What's coming / deferred             | [`ROADMAP.md` §2](../ROADMAP.md)                                  |

The website's components index page lists every component with a
one-line description ([`examples/site/components.go`](../examples/site/components.go)
is the source of truth). The `examples/site` test suite
keeps every registered `/components/<slug>` paired with:

- at least one chromedp e2e test
- zero axe-core violations against the default WCAG 2.0/2.1 A/AA rule set
- at least one `*_test.go` for the package that defines it

---

## Catalog

Live demos at `http://localhost:8082/components/<slug>` once the
dev server is up.

Variant-taking components panic on unknown variants; apps extend the
sets with `ui.RegisterButtonVariant` / `RegisterButtonSize` /
`RegisterCardVariant` / `RegisterStatusVariant` (one status
registration covers StatusBadge, Tag, Callout, and Notification). See
"Custom variants on framework components" in `ui-getting-started`.

### Primitives & semantic markup

- **kbd** — `core-ui/html.Kbd` — semantic `<kbd>` for keyboard input
- **shortcuthint** — `framework/ui.ShortcutHint` — OS-aware chord chips (⌘ on Mac / Ctrl elsewhere)
- **avatar** — `framework/ui.Avatar` — circular avatar with image → initials fallback (sm/md/lg/xl)
- **avatargroup** — `framework/ui.AvatarGroup` — readable 10% overlap, compact corner presence dots, and an adaptive-surface overflow chip
- **icon** — `framework/ui.Icon` — inline-SVG primitive backed by `RegisterIcon`; 10 built-ins, `currentColor` stroke, `AriaLabel` flips to `role="img"`
- **link** — `framework/ui.Link` — typed anchor with external-link affordances + unsafe-scheme href sanitizing
- **muted** — `framework/ui.Muted` — subdued inline `<span>` for secondary text

### Buttons & form controls

- **button** — `framework/ui.Button` — semantic button with typed variants (primary / secondary / danger / ghost) + sizes
- **linkbutton** — `framework/ui.LinkButton` — anchor styled as a Button, for CTAs that navigate
- **toggle** — `framework/ui.Checkbox` / `Radio` / `Switch` — labelled native inputs, FieldErrors-aware
- **checkboxgroup** — `framework/ui.CheckboxGroup` / `RadioGroup` — `<fieldset>` of checkboxes / radios with shared label + errors
- **segmented** — `framework/ui.SegmentedControl` — radio-group styled as a sliding pill bar
- **counter** — `framework/ui.Counter` — signal-driven numeric counter with +/− buttons
- **signaltoggle** — `framework/ui.SignalToggle` — `role="switch"` button bound to a boolean signal
- **toggleaction** — `framework/ui.ToggleAction` — three-state commit/untoggle button (idle → pending → committed) with optional mutex groups
- **passwordinput** — `framework/ui.PasswordInput` — password field with show/hide toggle
- **searchinput** — `framework/ui.SearchInput` — search field with icon prefix + clear button
- **inputgroup** — `framework/ui.InputGroup` — input with prepend / append addons
- **rating** — `framework/ui.RatingInput` — 1-N star/heart with Size / Gap / Shape / Icon knobs
- **slider** — `framework/ui.Slider` — `<input type=range>` with optional live value mirror
- **rangeslider** — `framework/ui.RangeSlider` — dual-thumb range with cross-clamp
- **numberinput** — `framework/ui.NumberInput` — number field with explicit +/- buttons
- **textarea** — `framework/ui.TextArea` — multi-line input with typed Autogrow
- **colorpicker** — `framework/ui.ColorPicker` — styled native `<input type=color>`
- **timepicker** — `framework/ui.TimePicker` — styled native `<input type=time>`
- **select** — `framework/ui.Select` — labelled native `<select>` with help, error, placeholder, and required marker
- **taginput** — `framework/ui.TagInput` — free-form chips, Enter/comma to commit, Backspace to remove
- **multiselect** — `core-ui/patterns/multiselect` — checkbox group with chip display above
- **form** — `framework/ui.Form` — opinionated `<form>` wrapper with submit + error summary
- **formfield** — `framework/ui.FormField` — labelled input with required + help + error states
- **formsection** — `framework/ui.FormSection` — grouped fields with a shared heading + description
- **validationsummary** — `framework/ui.ValidationSummary` — inline summary of form validation errors
- **conditionalfield** — `framework/ui.ConditionalField` (+ `ConditionalFieldVisible` inverse) — form section shown/hidden by another field's value
- **formrepeater** — `framework/ui.FormRepeater` — dynamic list of repeating field groups (add / remove rows)
- **repeater** — `framework/ui.Repeater` — dynamic add / remove item list with min / max limits
- **stepwizard** — `framework/ui.StepWizard` — multi-step form with a progress indicator bar

### Selection & input composition

- **combobox** — `core-ui/patterns/combobox` — debounced search with RPC-driven listbox
- **commandpalette** — `framework/ui.CommandPalette` — ⌘K modal + combobox composition
- **globalsearch** — `framework/ui.GlobalSearch` — sticky inline `/`-shortcut search bar
- **dropzone** — `framework/ui.FileDropzone` — hero file-drop surface with image previews
- **fileupload** — `framework/ui.FileUpload` — drag-drop file picker over native `<input type=file>`

### Navigation

- **recordsummary** — `framework/ui.RecordSummary` — compact dominant record or event summary with status, next-decision, balanced phone metrics, a bounded support rail, ownership, and a lead-region natural-width action that stays early on phones

- **skiplink** — `framework/ui.SkipLink` — focus-visible bypass link for jumping to main content
- **pageheader** — `framework/ui.PageHeader` — top-of-page header with title / eyebrow / subtitle / actions
- **siteheader** — `framework/ui.SiteHeader` — top bar with brand + nav + actions + mobile drawer; `MobileBrand` swaps in a concise phone identity
- **sitefooter** — `framework/ui.SiteFooter` — multi-column footer grid + bottom strip
- **anchoredrail** — `framework/ui.AnchoredRail` — sticky in-page nav rail with scrollspy-tracked active state
- **doclayout** — `framework/ui.DocLayout` / `DocPrevNext` — documentation page skeleton (nav rail + article + prev/next pager)
- **tabs-signal** — `framework/ui.Tabs` — signal-driven tab strip (click sets the signal; CSS shows the panel)
- **breadcrumbs** — `core-ui/patterns/breadcrumbs` — `<nav aria-label=Breadcrumb>` trail
- **pagination** — `core-ui/patterns/pagination` — numeric page navigation
- **sidebar** — `framework/ui.Sidebar` — responsive primary nav (inline ≥ md, drawer < md)
- **menu** — `framework/ui.Menu` — keyboard-driven dropdown built on `<details>`
- **tabs** — `core-ui/patterns/tabs` — `<details>`-based tab strip, zero JS
- **tree** — `core-ui/patterns/tree` — recursive tree with roving tabindex + lazy-load
- **toc** — `framework/ui.TableOfContents` — auto-built sticky nav from `<h2>` / `<h3>`
- **steprail** — `framework/ui.StepRail` — vertical numbered step rail with an active step + anchor links
- **steps** — `framework/ui.ProgressSteps` — linear step indicator (horizontal + vertical)

### Disclosure / surface widgets

- **accordion** — `core-ui/patterns/accordion` — Group + Stack disclosure variants
- **disclosure** — `core-ui/patterns/disclosure` — single styled `<details>`
- **collapsible** — `framework/ui.Collapsible` — styled `<details>` with clickable summary + Escape-to-close
- **modal** — `core-ui/widget/preset.Modal` — focus-trapped dialog with deeplink
- **drawer** — `core-ui/widget/preset.Drawer` — edge-mounted sliding panel
- **bottomsheet** — `core-ui/widget/preset.BottomSheet` — bottom-anchored Drawer variant
- **popover** — `core-ui/widget/preset.Popover` — click-triggered floating surface
- **floatingpanel** — `core-ui/widget/preset.FloatingPanel` — corner-anchored persistent panel
- **tooltip** — `framework/ui.Tooltip` — CSS-only hover/focus reveal
- **toast** — `core-ui/widget/preset.ToastStack` — client-side slide-in notifications (no SSE, no server queue)
- **notificationbell** — `framework/ui.NotificationBell` — bell + unread badge + popover dropdown
- **confirmaction** — `framework/ui.ConfirmAction` — trigger + alertdialog Modal
- **commandpalette** — *(also under Selection — same component)*

### Layout & display

- **layout** — `framework/ui.Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box`; `Cluster` wraps by default and exposes the explicit `NoWrap` opt-out
- **container** — `framework/ui.Container` — max-width page wrapper with breakpoint padding
- **section** — `framework/ui.Section` — labelled content section with heading + description
- **responsive** — `framework/ui.Responsive` — viewport-swap pair (independent desktop / mobile variants)
- **panehost** — `framework/ui.PaneHost` — primary pane + openable secondary/tertiary side panes with a responsive overlay-drawer collapse
- **themed** — `framework/ui.Themed` — wraps a subtree in a registered section-level theme override
- **card** — `framework/ui.Card` — labelled `<section>` with header/body/footer
- **sticky** — `framework/ui.Sticky` — theme-token sticky wrapper for top or bottom edge pinning
- **aspectratio** — `framework/ui.AspectRatio` — CLS-safe aspect-ratio wrapper for media and embeds
- **image** — `framework/ui.OptimizedImage` — responsive `<picture>` with CLS-safe Width/Height
- **pipelineimage** — `framework/ui.PipelineImage` — multi-format `<picture>` consuming `framework/image` VariantSet output (typed sources + LQIP/BlurHash)
- **divider** — `framework/ui.Divider` — semantic separator (horizontal, vertical, labelled)
- **gallery** — `framework/ui.Gallery` — Grid / Strip / Masonry thumbnail surface
- **lightbox** — `framework/ui.Lightbox` — zoom-overlay modal; pairs with Gallery
- **carousel** — `framework/ui.Carousel` — horizontal scroll-snap slider
- **infinitescroll** — `core-ui/patterns/infinitescroll` — IntersectionObserver-driven lazy feed
- **sortablelist** — `core-ui/patterns/sortablelist` — drag-and-drop + keyboard reorderable list
- **nestedlist** — `core-ui/patterns/nestedlist` — recursive `<ul>`/`<ol>` with native `<details>` collapse on branches
- **scrollspy** — `core-ui/patterns/scrollspy` — IntersectionObserver-based active-section tracking for any nav of in-page anchors
- **optimisticaction** — `framework/ui.OptimisticAction` — button that flips to its SSR-declared success state on click; the RPC fires underneath and rolls back with a shake on non-2xx
- **toggleaction** — `framework/ui.ToggleAction` — OptimisticAction's three-state cousin: idle ↔ committed with optional untoggle endpoint and `Group` mutex (committing one reverts its siblings)
- **networkretrybanner** — `framework/ui.NetworkRetryBanner` — persistent banner that shows on RPC-failure threshold or SSE silence; retry button pings a health endpoint to recover

### Data display

- **metricband** — `framework/ui.MetricBand` — flat semantic signal band (one row wide, two columns on phones) for related facts that should not become a wall of cards; `Hint` adds a trend or qualifier

- **datatable** — `framework/ui.DataTable` — sortable / paginated / island-swappable rows
- **statcard** — `framework/ui.StatCard` — metric card with label/value/trend. A 4-card dashboard row lives in a `ui.Grid`; the Grid default `Min: "16rem"` wraps 3+1 inside a sidebar-narrowed content column (~900px). For a 4-up row that fits (and degrades to 2+2 on tablet), pass `Grid(GridConfig{Min: "13rem"}, …)` — the `Min` knob is the intended control, not a Grid default change (16rem stays right for general content cards).
- **animatedcounter** — `framework/ui.AnimatedCounter` — IntersectionObserver-driven tick
- **timeline** — `framework/ui.Timeline` — vertical event rail
- **sparkline** — `framework/ui.Sparkline` — pure-SVG inline trend chart
- **piechart** — `framework/ui.PieChart` — SVG ratio chart (donut variant via InnerRadius)
- **barchart** — `framework/ui.BarChart` — categorical SVG bar chart. Legible by default: value labels ride above every bar cap (opt out with `HideValues`), the y-scale rounds up to a clean maximum so uniform / near-equal data keeps visible headroom (no wall of full-height slabs), a hairline baseline grounds the bars, and long `ShowLabels` category labels wrap onto two lines (a single over-long word ellipsizes, full text preserved in the bar's `<title>`). `ShowAxis` adds left value-axis ticks + gridlines. Per-bar `Color` accepts a palette token (primary/info/success/warning/danger), a registered status variant name, or a hex/rgb/hsl/oklch/var() CSS color; any other value falls back to the theme primary.
- **linechart** — `framework/ui.LineChart` — multi-series time-series chart with area + legend. Edge x-axis labels anchor inward so the first/last tick don't clip against the SVG boundary.
- **codeblock** — `framework/ui.CodeBlock` — styled `<pre><code>` sample block; `HighlightLines` pre-tokenizes lines for syntax highlighting
- **codetabs** — `framework/ui.CodeTabs` — the same snippet in several languages (Go / TypeScript / curl …) behind a zero-JS tab strip; pure composition of `patterns/tabs` + `CodeBlock` with copy buttons. Selection is per-tabset, not a page-wide language preference. The SDK docs site (`framework/sdkdocs`) is the flagship consumer.
- **counter** — `framework/ui.Counter` — numeric counter with +/− buttons mutating a client-side signal
- **jsonviewer** — `framework/ui.JSONViewer` — collapsible tree of arbitrary values
- **diffviewer** — `framework/ui.DiffViewer` — unified or split diff renderer
- **markdown** — `framework/ui.Markdown` — themed wrapper over `core/markdown`
- **detaillist** — `framework/ui.DetailList` — label/value description list for record detail views
- **factbox** — `framework/ui.FactBox` — single labelled fact (compact label + value pair; label-first or value-first)
- **terminalblock** — `framework/ui.TerminalBlock` — terminal transcript with a labelled header and `TerminalOut` / `TerminalOK` lines
- **progress** — `core-ui/patterns/progress` — native `<progress>` with theme styling
- **skeleton** — `core-ui/patterns/skeleton` — pure-CSS shimmer placeholders
- **spinner** — `framework/ui.Spinner` — inline CSS loading indicator

### Tags, badges, filters

- **tag** — `framework/ui.Tag` — interactive pill (linked / removable / status-variant)
- **statuspill** — `framework/ui.StatusPill` — compact status pill with optional leading dot (neutral / accent tone)
- **statusbadge** — `framework/ui.StatusBadge` — small inline pill conveying state (success / warning / danger / info / neutral)
- **filtertoolbar** — `framework/ui.FilterToolbar` — the filter/sort control strip above a list (facet `<select>` or radio-pill groups + search + sort + Apply/Reset), a single URL-driven GET form; wraps → stacks responsively so nothing clips on mobile
- **filterchipbar** — `framework/ui.FilterChipBar` — `role=toolbar` of removable filter chips
- **copybutton** — `framework/ui.CopyButton` — clipboard button with SR-announced confirmation
- **toolbar** — `framework/ui.Toolbar` — `role=toolbar` wrapper for grouped actions

### Status & banners

- **themetoggle** — `framework/ui.ThemeToggle` — dark/light/auto toggle that persists color-scheme mode; fresh scaffolds mount the adaptive `framework/ui/theme.Default()` palette, while app-owned themes must keep `DarkColors` complete
- **backtotop** — `framework/ui.BackToTop` — fixed scroll affordance that appears after a threshold
- **banner** — `framework/ui.Banner` — page-level persistent status strip
- **callout** — `framework/ui.Callout` — persistent inline info / warning / danger / neutral block
- **notification** — `framework/ui.Notification` — toast-styled inline notification (variant + dismiss)
- **emptystate** — `framework/ui.EmptyState` — centered title + description + optional CTA for no-data screens
- **signout** — `framework/ui.SignOut` — logout control: minimal form POSTing to the auth sign-out endpoint
- **pollingindicator** — `framework/ui.PollingIndicator` — pulsing dot + label confirming a polling RPC is firing
- **seo** — `core-ui/seo` + `uihost.WithSitemap` / `WithRobots` + `ScreenCanonical` / `ScreenHreflangs` / `ScreenSchema` — per-page SEO + sitewide sitemap.xml / robots.txt
- **seo-bundle** — `ScreenSEO()` returning an `SEO` struct — per-screen bundle of description + canonical + hreflangs + robots + OG + Twitter Card + JSON-LD in one declaration; alternative to the per-method calls above

### Marketing & page sections

- **hero** — `framework/ui.Hero` — centered landing hero (eyebrow + title + subtitle + actions + optional media)
- **herosplit** — `framework/ui.HeroSplit` — two-column hero (copy + media) with equal / copy-wide / media-wide ratios
- **pricingcard** — `framework/ui.PricingCard` — plan tile (price + period + feature list + CTA), optional featured highlight
- **authcard** — `framework/ui.AuthCard` — centered card shell for login / register / reset forms (title + alert + body + footer)

---

## Filter toolbars — the URL-driven pattern

`ui.FilterToolbar` is the control strip that sits above a `DataTable` or
card grid on a list screen. It renders **one `<form method="GET">`** whose
controls carry the current filter/sort/search state. Submitting it (Apply)
navigates to `<action>?facet=value&sort=…&q=…`; the screen's `Load(ctx)`
reads those params and renders the filtered list server-side. Refresh,
share, and back-button all reduce to "same URL → same view" with no client
state — the "URL params are the source of truth" contract from
`core-ui/ARCHITECTURE.md`. Reset is a plain link back to the bare action, so
it clears every param with zero JavaScript. It works with the runtime
disabled; the runtime just makes the Reset link a soft SPA nav.

Facets render as a native `<select>` (default) or, per `Kind: FacetPills`,
a wrapping radio-pill group (short, glanceable choices). The toolbar is
responsive by construction: it declares itself a container and lays its
controls out with flex-wrap, degrading row → wrapped rows → single-column
stack as *its own* width shrinks (correct even inside a slim sidebar on a
wide screen). Every control — including Apply/Reset — stays on-screen and
tappable; nothing overflows a narrow ancestor, and pill labels never wrap
mid-label ("Waiting On Customer" stays one line).

```go
// Screen.Render — the toolbar reflects the current URL state.
func (s *CustomersScreen) Render() render.HTML {
    return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
        ui.FilterToolbar(ui.FilterToolbarConfig{
            Action: "/customers", // the list route (the form GETs here)
            Facets: []ui.Facet{
                {Name: "status", Label: "Status", Value: s.status, Options: []ui.FacetOption{
                    {Label: "Open", Value: "open"}, {Label: "Closed", Value: "closed"},
                }},
                {Name: "plan", Label: "Plan", Kind: ui.FacetPills, Value: s.plan, Options: []ui.FacetOption{
                    {Label: "Free", Value: "free"}, {Label: "Pro", Value: "pro"},
                }},
            },
            Search:    &ui.FilterSearch{Name: "q", Value: s.query, Placeholder: "Search customers…"},
            Sort:      []ui.SortOption{{Label: "Newest", Value: "created_desc"}, {Label: "Name A–Z", Value: "name_asc"}},
            SortValue: s.sort,
        }),
        ui.DataTable(/* … rows filtered per s.status / s.plan / s.query / s.sort … */),
    )
}

// Screen.Load — read the URL params the toolbar submits.
func (s *CustomersScreen) Load(ctx context.Context) error {
    q := app.QueryFromContext(ctx)
    s.status, s.plan = q.Get("status"), q.Get("plan")
    s.query, s.sort = q.Get("q"), q.Get("sort")
    return nil // fetch + filter rows from s.* here
}
```

An empty facet value (the auto-prepended "All" choice) submits `status=`;
the server treats an empty param as "no filter". Pair with `FilterChipBar`
below the toolbar to show the *active* filters as removable chips.

> **`SearchFields` integration.** When the entity declares
> `SearchFields`, `FilterSearch{Name: "q"}` maps 1:1 onto the list
> endpoint — the `?q=` param the toolbar emits is exactly what the
> auto-CRUD List handler free-text-searches. No manual wiring needed:
> the server-side search is automatic. Without `SearchFields`, the
> `?q=` param is ignored by the CRUD layer and the screen must filter
> rows itself (the pre-existing "wired manually" behaviour).

---

## Adding a new component — checklist

The framework's drift tests catch most of these; this list is a
helpful pre-flight read for human reviewers.

1. **Implementation**: `framework/ui/<name>.go` (or `core-ui/patterns/<name>/`).
2. **Theme-token CSS only**: register your own `RegisterStyle`; use
   `var(--color-*, fallback)` etc. No top-level `.ui-*` rules in
   `examples/site/styles.go` — the site chrome is page-only.
3. **Unit tests**: `<name>_test.go` exercising panic paths + emitted
   markup + variant classes.
4. **`/components/<slug>` screen** in `examples/site/`:
   register in `main.go`, add an entry to `componentCatalog` in
   `components.go`. The catalog drives the site's page-level test
   loops (axe, single-`<main>`, …), so an unregistered page is an
   untested page.
5. **Chromedp e2e** in `examples/site/e2e_new_components_test.go`
   or `e2e_new_components_interactions_test.go` — ARIA shape for
   static components, real interaction (click / type / drag) for
   runtime-driven ones.
6. **`core-ui/ARCHITECTURE.md`**: any new `data-fui-*` attribute the
   runtime reads must land in the table here OR in the drift-test
   whitelist (with a justification comment). The
   `TestRuntimeAttrsAreDocumented` gate in
   `core-ui/runtime/attrdoc_test.go` enforces it.
7. **Axe**: `TestAxe_AllPagesAreClean` runs axe-core against
   every catalog page and fails on any violation. The most common authoring
   mistakes it catches: missing tap target floor (44×44),
   role/`aria-allowed-role` mismatches, color-contrast on tinted
   backgrounds, scrollable regions without `tabindex="0"`.
8. **Composition first**: before writing a new runtime module, see if
   `preset.Modal` / `preset.Popover` / `preset.Drawer` +
   `data-fui-open` + `data-fui-deeplink` + signal-binding already
   covers the case. Lightbox and NotificationBell each ship without
   a runtime module by composing existing primitives.

---

## Common mistakes

- **Writing a new runtime module when composition already covers it.**
  Check `preset.Modal` / `preset.Popover` / `preset.Drawer` +
  `data-fui-open` + `data-fui-deeplink` + signal binding first —
  Lightbox and NotificationBell ship with zero new JS by composing
  them. New modules are the expensive path (budget, tests, docs).
- **Styling a component from `examples/site/styles.go`.** Top-level
  `.ui-*` rules in the site stylesheet are forbidden — the site chrome
  is page-only. A component owns its CSS via `registry.RegisterStyle`
  with theme tokens, so it works in every host, not just the demo
  site.
- **Hardcoding colors/spacing instead of theme tokens.** Use
  `{colors.*}` / `{spacing.*}` / `var(--color-*, fallback)` so themed
  hosts and dark mode don't break your component.
- **Skipping the demo page + e2e pairing.** Every `/components/<slug>`
  page ships with at least one chromedp test and a package unit test
  (suite convention), and `TestAxe_AllPagesAreClean` automatically
  fails the build on any axe-core violation for every catalog page —
  register the page and you've signed up for all three.
- **Adding a `data-fui-*` attribute without documenting it.** The
  runtime contract lives in `core-ui/ARCHITECTURE.md`; every attribute
  the runtime reads must be in its table (or an explicitly justified
  whitelist) before the change lands.

---

## See also

- [`docs/widgets.md`](widgets.md) — widget framework (mount, deeplink, signal lifecycle).
- [`docs/ui-getting-started.md`](ui-getting-started.md) — first-time setup for the UI layer.
- [runtime-contract](runtime-contract.md) — the SSR/hydration/island/SSE model + `data-fui-*` attribute reference (embedded extract of `core-ui/ARCHITECTURE.md`).
- [`framework/ARCHITECTURE.md`](../framework/ARCHITECTURE.md) — package layout + extraction rules.
- [`ROADMAP.md` §2](../ROADMAP.md) — deferred UI components.
