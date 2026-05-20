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
| Pattern packages (Combobox, Tree, …) | `go doc github.com/DonaldMurillo/gofastr/core-ui/patterns/<pkg>` |
| Widget presets (Modal, Drawer, …)    | `go doc github.com/DonaldMurillo/gofastr/core-ui/widget/preset`  |
| Runtime data-fui-\* attributes       | [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md)          |
| What's coming / deferred             | [`docs/proposals/ui-component-roadmap.md`](proposals/ui-component-roadmap.md) |

The website's components index page lists every component with a
one-line description ([`examples/website/screen_components.go`](../examples/website/screen_components.go)
is the source of truth). A drift test in `examples/website/drift_test.go`
keeps every registered `/components/<slug>` paired with:

- at least one chromedp e2e test
- zero axe-core violations against the default WCAG 2.0/2.1 A/AA rule set
- at least one `*_test.go` for the package that defines it

---

## Catalog

Live demos at `http://localhost:8082/components/<slug>` once the
dev server is up.

### Primitives & semantic markup

- **kbd** — `core-ui/html.Kbd` — semantic `<kbd>` for keyboard input
- **shortcuthint** — `framework/ui.ShortcutHint` — OS-aware chord chips (⌘ on Mac / Ctrl elsewhere)
- **avatargroup** — `framework/ui.AvatarGroup` — overlapping avatar stack with overflow chip
- **icon** — `framework/ui.Icon` — inline-SVG primitive backed by `RegisterIcon`; 10 built-ins, `currentColor` stroke, `AriaLabel` flips to `role="img"`

### Buttons & form controls

- **toggle** — `framework/ui.Checkbox` / `Radio` / `Switch` — labelled native inputs, FieldErrors-aware
- **segmented** — `framework/ui.Segmented` — radio-group styled as a sliding pill bar
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
- **forms** — `framework/ui` form module — fields, validation, conditional sections, step wizard, and repeaters

### Selection & input composition

- **combobox** — `core-ui/patterns/combobox` — debounced search with RPC-driven listbox
- **commandpalette** — `framework/ui.CommandPalette` — ⌘K modal + combobox composition
- **globalsearch** — `framework/ui.GlobalSearch` — sticky inline `/`-shortcut search bar
- **dropzone** — `framework/ui.FileDropzone` — hero file-drop surface with image previews
- **fileupload** — `framework/ui.FileUpload` — drag-drop file picker over native `<input type=file>`

### Navigation

- **skiplink** — `framework/ui.SkipLink` — focus-visible bypass link for jumping to main content
- **breadcrumbs** — `core-ui/patterns/breadcrumbs` — `<nav aria-label=Breadcrumb>` trail
- **pagination** — `core-ui/patterns/pagination` — numeric page navigation
- **sidebar** — `framework/ui.Sidebar` — responsive primary nav (inline ≥ md, drawer < md)
- **menu** — `framework/ui.Menu` — keyboard-driven dropdown built on `<details>`
- **tabs** — `core-ui/patterns/tabs` — `<details>`-based tab strip, zero JS
- **tree** — `core-ui/patterns/tree` — recursive tree with roving tabindex + lazy-load
- **toc** — `framework/ui.TableOfContents` — auto-built sticky nav from `<h2>` / `<h3>`
- **steps** — `framework/ui.ProgressSteps` — linear step indicator (horizontal + vertical)

### Disclosure / surface widgets

- **accordion** — `core-ui/patterns/accordion` — Group + Stack disclosure variants
- **disclosure** — `core-ui/patterns/disclosure` — single styled `<details>`
- **modal** — `core-ui/widget/preset.Modal` — focus-trapped dialog with deeplink
- **drawer** — `core-ui/widget/preset.Drawer` — edge-mounted sliding panel
- **bottomsheet** — `core-ui/widget/preset.BottomSheet` — bottom-anchored Drawer variant
- **popover** — `core-ui/widget/preset.Popover` — click-triggered floating surface
- **tooltip** — `framework/ui.Tooltip` — CSS-only hover/focus reveal
- **toast** — `core-ui/widget/preset.ToastStack` — SSE-pushed slide-in notifications
- **notificationbell** — `framework/ui.NotificationBell` — bell + unread badge + popover dropdown
- **confirmaction** — `framework/ui.ConfirmAction` — trigger + alertdialog Modal
- **commandpalette** — *(also under Selection — same component)*

### Layout & display

- **layout** — `framework/ui.Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box`
- **container** — `framework/ui.Container` — max-width page wrapper with breakpoint padding
- **card** — `framework/ui.Card` — labelled `<section>` with header/body/footer
- **sticky** — `framework/ui.Sticky` — theme-token sticky wrapper for top or bottom edge pinning
- **aspectratio** — `framework/ui.AspectRatio` — CLS-safe aspect-ratio wrapper for media and embeds
- **image** — `framework/ui.OptimizedImage` — responsive `<picture>` with CLS-safe Width/Height
- **divider** — `framework/ui.Divider` — semantic separator (horizontal, vertical, labelled)
- **gallery** — `framework/ui.Gallery` — Grid / Strip / Masonry thumbnail surface
- **lightbox** — `framework/ui.Lightbox` — zoom-overlay modal; pairs with Gallery
- **carousel** — `framework/ui.Carousel` — horizontal scroll-snap slider
- **infinitescroll** — `core-ui/patterns/infinitescroll` — IntersectionObserver-driven lazy feed
- **sortablelist** — `core-ui/patterns/sortablelist` — drag-and-drop + keyboard reorderable list
- **nestedlist** — `core-ui/patterns/nestedlist` — recursive `<ul>`/`<ol>` with native `<details>` collapse on branches
- **scrollspy** — `core-ui/patterns/scrollspy` — IntersectionObserver-based active-section tracking for any nav of in-page anchors
- **optimisticaction** — `framework/ui.OptimisticAction` — button that flips to its SSR-declared success state on click; the RPC fires underneath and rolls back with a shake on non-2xx
- **networkretrybanner** — `framework/ui.NetworkRetryBanner` — persistent banner that shows on RPC-failure threshold or SSE silence; retry button pings a health endpoint to recover

### Data display

- **datatable** — `framework/ui.DataTable` — sortable / paginated / island-swappable rows
- **statcard** — `framework/ui.StatCard` — metric card with label/value/trend
- **animatedcounter** — `framework/ui.AnimatedCounter` — IntersectionObserver-driven tick
- **timeline** — `framework/ui.Timeline` — vertical event rail
- **sparkline** — `framework/ui.Sparkline` — pure-SVG inline trend chart
- **piechart** — `framework/ui.PieChart` — SVG ratio chart (donut variant via InnerRadius)
- **barchart** — `framework/ui.BarChart` — categorical SVG bar chart
- **linechart** — `framework/ui.LineChart` — multi-series time-series chart with area + legend
- **jsonviewer** — `framework/ui.JSONViewer` — collapsible tree of arbitrary values
- **diffviewer** — `framework/ui.DiffViewer` — unified or split diff renderer
- **markdown** — `framework/ui.Markdown` — themed wrapper over `core/markdown`
- **progress** — `core-ui/patterns/progress` — native `<progress>` with theme styling
- **skeleton** — `core-ui/patterns/skeleton` — pure-CSS shimmer placeholders
- **spinner** — `framework/ui.Spinner` — inline CSS loading indicator

### Tags, badges, filters

- **tag** — `framework/ui.Tag` — interactive pill (linked / removable / status-variant)
- **filterchipbar** — `framework/ui.FilterChipBar` — `role=toolbar` of removable filter chips
- **copybutton** — `framework/ui.CopyButton` — clipboard button with SR-announced confirmation
- **toolbar** — `framework/ui.Toolbar` — `role=toolbar` wrapper for grouped actions

### Status & banners

- **themetoggle** — `framework/ui.ThemeToggle` — dark/light/auto toggle that persists color-scheme mode
- **backtotop** — `framework/ui.BackToTop` — fixed scroll affordance that appears after a threshold
- **banner** — `framework/ui.Banner` — page-level persistent status strip
- **pollingindicator** — `framework/ui.PollingIndicator` — pulsing dot + label confirming a polling RPC is firing
- **seo** — `core-ui/seo` + `uihost.WithSitemap` / `WithRobots` + `ScreenCanonical` / `ScreenHreflangs` / `ScreenSchema` — per-page SEO + sitewide sitemap.xml / robots.txt

---

## Adding a new component — checklist

The framework's drift tests catch most of these; this list is a
helpful pre-flight read for human reviewers.

1. **Implementation**: `framework/ui/<name>.go` (or `core-ui/patterns/<name>/`).
2. **Theme-token CSS only**: register your own `RegisterStyle`; use
   `var(--color-*, fallback)` etc. No top-level `.ui-*` rules in
   `examples/website/theme.go` — the website chrome is page-only.
3. **Unit tests**: `<name>_test.go` exercising panic paths + emitted
   markup + variant classes.
4. **`/components/<slug>` screen** in `examples/website/`:
   register in `main.go`, add an entry to `componentEntries` in
   `screen_components.go`. The `TestDrift_EveryComponentPageHasE2ETest`
   gate fails until you also add at least one chromedp test.
5. **Chromedp e2e** in `examples/website/e2e_new_components_test.go`
   or `e2e_new_components_interactions_test.go` — ARIA shape for
   static components, real interaction (click / type / drag) for
   runtime-driven ones.
6. **`core-ui/ARCHITECTURE.md`**: any new `data-fui-*` attribute the
   runtime reads must land in the table here OR in the drift-test
   whitelist (with a justification comment). The
   `TestDrift_RuntimeDataFuiAttributesDocumented` gate enforces it.
7. **Axe**: `TestAxe_AllComponentPagesAreClean` runs axe-core against
   every page and fails on any violation. The most common authoring
   mistakes it catches: missing tap target floor (44×44),
   role/`aria-allowed-role` mismatches, color-contrast on tinted
   backgrounds, scrollable regions without `tabindex="0"`.
8. **Composition first**: before writing a new runtime module, see if
   `preset.Modal` / `preset.Popover` / `preset.Drawer` +
   `data-fui-open` + `data-fui-deeplink` + signal-binding already
   covers the case. Lightbox and NotificationBell each ship without
   a runtime module by composing existing primitives.

---

## See also

- [`docs/widgets.md`](widgets.md) — widget framework (mount, deeplink, signal lifecycle).
- [`docs/ui-getting-started.md`](ui-getting-started.md) — first-time setup for the UI layer.
- [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) — runtime contract + `data-fui-*` attribute reference.
- [`framework/ARCHITECTURE.md`](../framework/ARCHITECTURE.md) — package layout + extraction rules.
- [`docs/proposals/ui-component-roadmap.md`](proposals/ui-component-roadmap.md) — shipped + deferred components.
