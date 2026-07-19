# UI composition recipes

GoFastr's component catalog answers “what exists.” This guide answers “what
page grammar fits the user's task?” Choose a recipe after completing the
project's `DESIGN.md`, then compose it from framework primitives. Applications
ship no bespoke CSS and do not recreate structural markup.

These are decision recipes, not templates to copy unchanged. Preserve the
hierarchy and responsive intent; adapt the content and components to the
product.

---

## 1. Command center

**Use for:** incident response, monitoring, operations, fulfillment, or any
screen organized around an urgent current state and the next decision.

**Density:** compact. **Dominant element:** one `ui.RecordSummary` containing
the active state and its next decision. **Avoid:** an equal-weight stat-card
row, a separate Banner that repeats the same state, and a Card around every
fact.

Compose:

```go
summary := ui.RecordSummary(ui.RecordSummaryConfig{
    Eyebrow:     "INC-2841 · Payments",
    Title:       "Checkout latency is elevated",
    Description: "Card authorizations are slower while the team validates the mitigation.",
    Status:      ui.StatusBadge(ui.StatusBadgeConfig{Label: "SEV-1 active", Variant: ui.StatusDanger}),
    Highlight: ui.Callout(
        ui.CalloutConfig{Title: "Next decision · 14:30 UTC", Variant: ui.StatusWarning},
        render.Text("Rollback if authorization latency remains above 800 ms."),
    ),
    Metrics: ui.MetricBand(ui.MetricBandConfig{Items: []ui.MetricBandItem{
        {Label: "Impact", Value: "32%", Hint: "checkout requests"},
        {Label: "Started", Value: "13:42 UTC", Hint: "32 min ago"},
        {Label: "p95 latency", Value: "1.4 s", Hint: "down from 1.6 s"},
        {Label: "Services", Value: "3", Hint: "2 recovering"},
    }}),
    Aside: ui.Stack(ui.StackConfig{Gap: ui.GapSM},
        ui.Muted(render.Text("Live bridge · Mina leads")),
        ui.AvatarGroup(ui.AvatarGroupConfig{
            Avatars: responderAvatars,
            Max: 3, Label: "Responders on the live bridge", ShowNames: true,
        }),
    ),
    Footer:  ui.Muted(render.Text("Commander: Mina Chen · Updated 2 min ago")),
    Actions: ui.LinkButton(ui.LinkButtonConfig{Label: "Open incident", Href: "/incidents/2841"}),
    Tone:    ui.RecordSummaryToneDanger,
})

ui.Container(ui.ContainerConfig{Width: ui.ContainerWide},
    ui.Stack(ui.StackConfig{Gap: ui.GapLG},
        summary,
        ui.HeroSplit(ui.HeroSplitConfig{
            Ratio: ui.HeroSplitCopyWide,
            Copy:  ui.Timeline(/* current chronology */),
            Media: ui.DetailList(/* ownership and affected services */),
        }),
    ),
)
```

`RecordSummary` controls the heading scale on phones, places its natural-width
`Actions` in the lead region, and gives a concise `Aside` a purposeful support
rail on wide canvases. On phones the action leads that support region instead
of falling below the full summary. Keep `Description` to one or two sentences,
keep `Highlight` to one decision plus one short condition, and move the full
narrative later. `MetricBand` stays one compact row on wide viewports and
becomes a two-column signal band on phones; an odd final signal spans the row
instead of stranding an empty quadrant. Use `Hint` for a trend or qualifier
rather than repeating the value. If the SiteHeader identity is long, set
`SiteHeaderConfig.MobileBrand` to a concise product mark/name.

Let the status and primary path lead. The mobile opening should preserve status
→ concise impact → action → compact live context → next decision → signals
without a second mobile-only summary tree. Full responder lists and archival evidence belong later or in
`ui.Collapsible`; do not shrink them into micro text.

On a wide detail route, put related bounded modules—two `DetailList`s, a record
and live impact, or metadata and ownership—in `ui.Grid` when they have similar
weight. Do not stack a narrow table down the left half of a desktop canvas and
leave an accidental empty rail. Let the framework grid reflow the pair to a
single readable column on phones.

**Live proof:** [RecordSummary](/components/recordsummary), [MetricBand](/components/metricband), and [Timeline](/components/timeline) in the component gallery.

---

## 2. Investigation workspace

**Use for:** research, support triage, log exploration, review queues, and
source-plus-annotation tools.

**Density:** compact to balanced. **Dominant element:** the evidence currently
being examined. **Avoid:** independent floating cards for query, source,
excerpt, notes, and synthesis.

Compose the desktop work area with `ui.PaneHost`:

```go
ui.PaneHost(ui.PaneHostConfig{
    Primary:        evidenceReader,
    Secondary:      sourceNavigator,
    SecondaryOpen:  true,
    SecondaryLabel: "Sources",
    Tertiary:       annotationInspector,
    TertiaryOpen:   true,
    TertiaryLabel:  "Annotations",
})
```

Use `ui.Responsive` when the mobile task needs a materially different order.
On a phone, render the reader first and make source/annotation access explicit
with normal routes or framework drawers; do not serialize three desktop panes
into one endless page.

**Live proof:** the full [/examples/workspace](/examples/workspace) route and the [PaneHost](/components/panehost) gallery demonstration.

---

## 3. Split narrative

**Use for:** technical product pages, documentation entry pages, launches, and
content where a concrete artifact should explain the product beside the copy.

**Density:** balanced to spacious. **Dominant element:** the artifact—code,
terminal output, diagram, or product image—not a decorative gradient.
**Avoid:** centered hero → three equal feature cards → centered CTA.

Start with `ui.HeroSplit`; pair concise copy with `ui.CodeBlock`,
`ui.TerminalBlock`, `ui.OptimizedImage`, or a meaningful chart. Continue with
alternating full-width `ui.Section` blocks, changing which side carries the
artifact. Use `ui.Grid` only when the concepts are genuinely peers.

On mobile, put the explanatory copy before the artifact when it establishes
context; put the artifact first when recognition is the primary task. Check
long code lines and actions at 390px rather than assuming the two columns will
collapse cleanly.

**Live proof:** [HeroSplit](/components/herosplit), [CodeBlock](/components/codeblock), and [TerminalBlock](/components/terminalblock) in the component gallery.

---

## 4. Marketplace browse and detail

**Use for:** products, media libraries, creator work, property, or any domain
where visual comparison and trust details drive selection.

**Density:** balanced. **Dominant element:** real item media on browse; the
purchase/selection decision on detail. **Avoid:** forcing every item into the
same text-heavy SaaS Card.

Browse with `ui.FilterToolbar` followed by `ui.Gallery` (grid or masonry) and
use captions for the minimum comparison facts. On detail, use
`ui.HeroSplit{Ratio: ui.HeroSplitMediaWide}` for gallery versus identity,
price, availability, and action; follow it with `ui.DetailList` for provenance
and fulfillment facts.

Mobile detail should keep identity, price, availability, and the primary
action close to the first useful image. Do not make the user traverse a full
desktop gallery before reaching the action.

**Live proof:** [Gallery](/components/gallery), [Lightbox](/components/lightbox), and [FilterToolbar](/components/filtertoolbar) in the component gallery.

---

## 5. Transactional field flow

**Use for:** inspections, setup, checkout, approvals, intake, and tasks that
must be completed accurately under time pressure.

**Density:** compact. **Dominant element:** the current step and its action.
**Avoid:** a dashboard summary above the actual work and a desktop sidebar
that becomes a long preamble on mobile.

Use `ui.ProgressSteps` or `ui.StepRail` for orientation, `ui.Form` and
`ui.FormField` for the active step, `ui.Sticky` for the primary action when the
form is long, and `ui.Callout` only for actionable warnings. Render a separate
mobile composition with `ui.Responsive` when the desktop route/context view
would bury the next job or form.

Keep the first mobile viewport focused: route/job identity, state, required
readings, issue capture, and completion. Secondary history can follow after the
active controls.

**Live proof:** [ProgressSteps](/components/progresssteps), [Form](/components/form), and [Sticky](/components/sticky) in the component gallery.

---

## Choosing without overfitting

1. State the primary user task and the first decision.
2. Pick the closest recipe by task—not by visual fashion.
3. Name the dominant element and the content that can remain secondary.
4. Decide desktop regions and the mobile priority order before implementation.
5. Survey `framework/ui`, `core-ui/app`, and `core-ui/patterns` for the named
   primitives.
6. Render at about 390px and 1440px in light and dark schemes.
7. Identify the three weakest visible decisions and revise them.

If the recipe cannot be expressed without local structural markup or CSS,
record the missing reusable capability and add it to the design system. The
application is the completeness test; it is not an exception to the styling
contract.

## See also

- [UI capability map](ui-capability-map.md) chooses the state, mutation, delivery, and scaling boundaries before a page recipe.
- [UI components index](ui-new-components.md) lists every constructor and live gallery route.
- [Runtime contract](runtime-contract.md) defines SSR, RPC islands, and SSE.
- [Signal store](signal-store.md) covers typed client projection and derived state.
- [Optimistic UI](optimistic-ui.md) for the mutation lifecycle and how optimistic recipes compose into each page grammar.

## Common mistakes

- Choosing a recipe after already assembling a familiar card grid.
- Treating every region as equally important.
- Using Cards, pills, or elevation as decoration rather than semantics.
- Repeating desktop content in a second “mobile” tree without changing its
  priority.
- Calling a component-valid DOM “visually verified” without screenshots.
- Adding app CSS to approximate a recipe instead of filling an upstream gap.
