# framework/ui — the component catalog

The design system ships roughly 100 ready-made components — layout
(Hero, Grid, Stack, Sidebar, PageHeader, RecordSummary), forms (Form, FormField,
Select, TagInput, step wizards), data (DataTable, MetricBand, StatCard, charts,
DetailList), chrome (SiteHeader, AuthCard, ThemeToggle, Card, Banner) —
plus layout shells (`core-ui/app`), composed patterns
(`core-ui/patterns`), overlay widgets (`core-ui/widget/preset`), and
theme tokens (`framework/ui/theme`). All styling and structural markup
lives in this system; apps ship zero bespoke CSS.

**Use this when** the prompt mentions: page, screen, layout, form,
table, list, card, button, modal, drawer, dropdown, toast, chart,
navigation, sidebar, hero, badge, avatar, theme, dark mode, styling,
CSS, "make it look …", or any other UI surface.

**Import:** `github.com/DonaldMurillo/gofastr/framework/ui` (theme
tokens: `github.com/DonaldMurillo/gofastr/framework/ui/theme`)

## Composition before component selection

For any new page or material redesign, read and complete the app's
`DESIGN.md` before choosing components. State the primary user task, intended
density, dominant element, composition model, mobile priority order, and two
familiar patterns to avoid. Then read:

```text
gofastr docs ui-composition-recipes
```

Choose the closest full-page grammar and adapt its information hierarchy. The
recipes compose existing framework surfaces such as `RecordSummary`,
`MetricBand`, `HeroSplit`, `PaneHost`,
`Responsive`, `AnchoredRail`, `FilterToolbar`, `Gallery`, `Timeline`,
`DetailList`, `Container`, `Stack`, and `Grid`; they are not permission to add
app CSS.

Composition requirements:

- Give every route one dominant element or decision.
- For a dominant record, incident, or operational state, use `RecordSummary`
  with `MetricBand`; do not repeat the same state in a separate Banner. Keep
  its description to one or two sentences and its highlight to one decision
  plus one short condition. Put compact owner/presence context in `Aside`, use
  `MetricBandItem.Hint` for trends or qualifiers, and move full narrative or
  rosters later.
- Put CTAs in a component `Actions` slot or `Cluster`. A direct child of the
  default `Stack` stretches, so it is not an action row. `RecordSummary`
  deliberately places `Actions` in its lead region so the primary path stays
  in the first useful phone viewport. `Cluster` wraps whole controls by
  default; reserve `ClusterConfig.NoWrap` for compact chrome known to fit.
- On wide detail routes, pair related bounded modules such as `DetailList`s in
  `Grid` instead of stacking them in a half-width column with an accidental
  empty rail. Reflow that authored desktop composition to one column on phones.
- If a `SiteHeader` wordmark or identity is long, supply `MobileBrand` rather
  than squeezing status, identity, and navigation into one phone row.
- Keep the scaffold's `WithTheme(theme.Default())`: it supplies a complete
  adaptive palette for `ThemeToggle` and OS dark preference. App-owned themes
  must define every semantic `DarkColors` value before rendering a toggle.
- Use `ui.Link` for visible text links and `ui.SiteFooter` for linked footer
  chrome; never depend on browser-default anchor colors. SiteHeader owns the
  appearance of a linked Brand slot.
- Group with typography, alignment, whitespace, and separators before adding
  another Card.
- Do not default to a stat-card row, three equal feature cards, a centered
  gradient hero, decorative pills, nested cards, or equal visual weight for
  every section.
- Use equal columns only for genuinely equal concepts. Vary width and density
  when the content has a clear priority.
- Design the 390px reading/action order explicitly. Mobile is not the desktop
  DOM mechanically stacked into one column.
- Use realistic domain content; placeholder copy prevents credible hierarchy.

Before finishing UI work, render the result near 390px and 1440px in light and
dark schemes, inspect the actual pixels, identify the three weakest visible
decisions, and revise them. Compilation and component compliance alone do not
complete a UI task.

**Before hand-rolling any markup or CSS**, check the catalog — the
component you want almost certainly exists:

1. `gofastr docs ui-new-components` — one-page index of every
   component, with its package and demo slug.
2. `go doc github.com/DonaldMurillo/gofastr/framework/ui` — the full
   inventory; `go doc .../framework/ui.<Name>` for one component's
   config and constructor.
3. Live demos at `/components/<slug>` on the docs site
   (`examples/site`) — run it and click around.

**Shape:**
```go
import "github.com/DonaldMurillo/gofastr/framework/ui"

header := ui.PageHeader(ui.PageHeaderConfig{
    Title:    "Customers",
    Subtitle: "1,283 active",
    Actions:  ui.Button(ui.ButtonConfig{Label: "New customer"}),
})
```

**Don't reinvent** a styled `<div>`, a bespoke stylesheet, or a
hand-rolled table/form/nav. Theming goes through the
`framework/ui/theme` tokens (`--color-*`, `--font-*`), never direct
CSS properties. If a component is genuinely missing, add it to the
design system (`framework/ui` or `core-ui`) and compose it — don't
patch around it locally.
