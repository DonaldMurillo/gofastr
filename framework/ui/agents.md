# framework/ui — the component catalog

The design system ships roughly 100 ready-made components — layout
(Hero, Grid, Stack, Sidebar, PageHeader), forms (Form, FormField,
Select, TagInput, step wizards), data (DataTable, StatCard, charts,
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
