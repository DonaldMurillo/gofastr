# UI component roadmap

Tracks components that are known-missing from `core-ui/` and `framework/ui/`
but deferred — either by design (out of current scope), by scheduling (own
worktree), or because the shape isn't settled yet. Picked-up items should be
removed from this file when they land.

Source: gap audit on branch `worktree-staged-roaming-whale` (2026-05-17).

---

## Shipped

### Wave 4 follow-up (2026-05-18) — Lightbox split + Gallery + Carousel

- Lightbox — split into a STANDALONE zoom-overlay widget. Returns
  just `*widget.Builder`. New options: NavArrows (Prev/Next +
  ArrowLeft/Right), ShowCaption, AllowDownload. Image preloading
  for adjacent siblings happens on every open.
- Gallery — `framework/ui/` (new). Three variants: Grid
  (configurable Columns + Gap), Strip (horizontal scroll-snap),
  Masonry (CSS-columns flow). Set Lightbox: "<name>" to wire each
  item as a trigger; otherwise items are plain links.
- Carousel — `framework/ui/` (new). Horizontal scroll-snap slider
  with Prev/Next + dots + ArrowLeft/Right keyboard nav. Opt-in
  AutoRotateMs pauses on hover, focus, prefers-reduced-motion, and
  background-tab visibility. Loop + multi-slide VisiblePerView.

### Wave 4 (2026-05-18) — Tier 3 composite & navigation

- TableOfContents — `framework/ui/` (auto-built nav from h2/h3 + IntersectionObserver active-section tracking)
- Lightbox — see Wave 4 follow-up above (initial Lightbox shipped here was a Gallery+Lightbox composite; split + extended in the follow-up)
- NotificationBell — `framework/ui/` (bell + unread badge + paired preset.Popover)
- SortableList — `core-ui/patterns/sortablelist/` (HTML5 drag + keyboard Space-grab/Arrow-move/Esc-cancel)
- GlobalSearch — `framework/ui/` (sticky bar with `/`-shortcut focus + Combobox results)
- BottomSheet preset — `core-ui/widget/preset.BottomSheet` (bottom-anchored Drawer variant)

### Wave 3 (2026-05-18) — Tier 1 + Tier 2

- Container (max-width wrapper) — `framework/ui/`
- Disclosure (single styled `<details>`) — `core-ui/patterns/disclosure/`
- TimePicker — `framework/ui/`
- RangeSlider (dual thumb, cross-clamp) — `framework/ui/`
- TagInput (free-form chips, Enter/comma/Backspace) — `framework/ui/`
- Toolbar (`role=toolbar` + grouped buttons) — `framework/ui/`
- Sparkline (pure-SVG trend) — `framework/ui/`
- PieChart / DonutChart — `framework/ui/`
- BarChart — `framework/ui/`
- LineChart (multi-series + area + legend) — `framework/ui/`
- JSONViewer (collapsible tree) — `framework/ui/`
- DiffViewer (unified + split modes) — `framework/ui/`
- Markdown (themed wrapper over `core/markdown`) — `framework/ui/`
- AnimatedCounter (IntersectionObserver tick + reduced-motion guard) — `framework/ui/`

### Wave 2 (2026-05-17 / 2026-05-18)

- Banner / InlineAlert — `framework/ui/`
- Timeline — `framework/ui/`
- ProgressSteps — `framework/ui/`
- RatingInput (Size/Gap/Shape/Icon knobs) — `framework/ui/`
- ColorPicker (native `<input type=color>` wrapper) — `framework/ui/`
- Slider (single-thumb with live value mirror) — `framework/ui/`
- NumberInput (stepper with cross-platform +/− buttons) — `framework/ui/`
- TextArea (typed Autogrow) — `framework/ui/`
- MultiSelect (checkbox group in disclosure with chip rendering) — `core-ui/patterns/multiselect/`
- FileDropzone (hero file-drop, drag-drop hook, image preview) — `framework/ui/`

### Wave 1 (2026-05-17)

- Async Combobox / Typeahead — `core-ui/patterns/combobox/`
- Tree View — `core-ui/patterns/tree/`
- Infinite Scroll List — `core-ui/patterns/infinitescroll/`
- Confirm Action / DangerConfirm — `framework/ui/`
- Command Palette (⌘K) — `framework/ui/`
- Segmented Control — `framework/ui/`
- Copy-to-Clipboard button — `framework/ui/`
- AvatarGroup / Avatar Stack — `framework/ui/`
- FilterChipBar — `framework/ui/`
- `<kbd>` primitive + ShortcutHint — `core-ui/html/` + `framework/ui/`
- Button `Size` (ButtonSizeSmall / Large) + `framework/ui.Link` with
  LinkInline / LinkAction / LinkMuted variants — landed during the
  website-CSS audit (2026-05-18) to eliminate `.ui-button--small` /
  `.ui-link` leakage from `examples/website/theme.go`.

---

## Deferred — Wave 4 candidates (Tier 3 composite & navigation)

All Wave 4 / Tier-3 items shipped (see Wave 4 section above). Open
follow-ups:

- Lightbox: **pinch-to-zoom inside the open viewer** (touch event
  story is its own design pass; punted from the Wave-4 follow-up).
- BottomSheet drag-to-dismiss gesture (touch event story).
- Carousel: virtual-scroll for galleries with >50 slides (current
  render emits all slides upfront; fine for product reels, costly
  for image-heavy archive views).

## Deferred — Wave 5 candidates (Tier 4 form helpers / async)

### InlineValidationSummary

- **Layer:** `framework/ui/`
- **Shape sketch:** Top-of-form alert that lists every field error
  with an anchor link to the bad input. Server returns
  `{field: error}` map; component renders a Banner-variant `danger`
  block with `<a href="#field-id">label — error</a>` per row.
- **Pre-reqs:** Banner (shipped).

### ConditionalField

- **Layer:** `framework/ui/`
- **Shape sketch:** CSS-`:has()` driven show/hide wrapper that
  reveals a child field based on another field's value. Zero JS —
  pure selector logic. The wrapper takes `When` (selector chain like
  `[name=plan]:checked[value=pro]`) and a body slot.
- **Pre-reqs:** None.

### OptimisticAction

- **Layer:** `framework/ui/`
- **Shape sketch:** Wraps a trigger (Button, Link) with optimistic UI:
  declares the success-state DOM ("Following ✓"), flips immediately on
  click, fires the RPC underneath, rolls back if the response is non-2xx.
- **Pre-reqs:** Runtime RPC pipeline (shipped).

### NetworkRetryBanner

- **Layer:** `framework/ui/`
- **Shape sketch:** Persistent top banner that auto-shows when the SSE
  stream goes silent or N consecutive RPCs fail. Auto-dismisses when
  connectivity recovers. Renders a "Retry now" button that pings a
  health endpoint.
- **Pre-reqs:** Banner (shipped), runtime SSE hook (shipped).

### PollingIndicator

- **Layer:** `framework/ui/`
- **Shape sketch:** Tiny pulsing dot + "Live" label that confirms a
  polling RPC is firing. Pairs with `data-fui-rpc-trigger="input"`
  patterns to give users feedback that the live-search is actually
  searching.
- **Pre-reqs:** None.

---

## Deferred — Wave 6 candidates (Tier 5 Skeleton compositions)

### SkeletonCard / SkeletonRow / SkeletonAvatar

- **Layer:** `framework/ui/`
- **Shape sketch:** Preset compositions over the existing Skeleton
  primitives. SkeletonCard = title line + 2 body lines + footer line
  inside a Card surface; SkeletonRow = label + value + chevron;
  SkeletonAvatar = circle + 2 stacked lines. One-liner shortcuts so
  loading layouts don't reinvent the wheel.
- **Pre-reqs:** Skeleton primitives (shipped), Card (shipped).

---

## Deferred — pick up later

### Calendar Date Picker

- **Layer:** `core-ui/widget/preset/`
- **Why deferred:** Big surface (single date / range / time / locale / min-max
  / disabled-dates) and design needs to settle before we commit to an API.
- **Shape sketch:** anchored Popover preset with a server-rendered calendar
  island. RPC fetches month grids; selection submits via `Bind` to the
  underlying `<input>`. Must work with native `<input type="date">` as
  graceful fallback.
- **Pre-reqs:** Popover preset (already shipped).

### Dynamic Form Repeater

- **Layer:** `core-ui/patterns/`
- **Why deferred:** Form-array indexing and partial-island re-render contract
  need an explicit design pass — risk of leaking a half-baked array shape
  across the framework.
- **Shape sketch:** `Repeater(name, template)` pattern. Add/Remove buttons
  fire RPCs that re-render the list island; submission collects nested fields
  as `name[i].field`.
- **Pre-reqs:** May want a typed form-state helper in `framework/ui/form`
  before building this on top.

### Form Step Wizard

- **Layer:** `core-ui/patterns/`
- **Why deferred:** Needs a server-side step-state story (session? signed
  cookie? hidden cumulative form?) before we pick an API. Likely overlaps
  with the upcoming form-state helper.
- **Shape sketch:** `Wizard(steps...)` with per-step RPC validation and
  Next/Back actions; final submit posts the accumulated payload.
- **Pre-reqs:** Form-state helper; possibly Dynamic Form Repeater for steps
  that contain arrays.

### Inline Edit Field

- **Layer:** `framework/ui/`
- **Why deferred:** Focus-management contract between SSR swap and the new
  input needs care; we want the runtime to grow a "post-swap focus" hint
  first so every island-replacing component benefits.
- **Shape sketch:** `InlineEdit(cfg)` renders a span; click swaps to an
  input, Enter saves via RPC, Escape reverts, blur saves. Validation errors
  render inline below the input.
- **Pre-reqs:** Runtime post-swap focus directive (`data-fui-focus`-style).

### Sparkline

- **Layer:** `framework/ui/`
- **Why deferred:** User wants this in its **own worktree** — pure SVG renderer,
  zero JS, but the API surface (single line / multi-series / bars / area /
  baseline / threshold band) deserves a focused design pass and benchmark
  pass on its own.
- **Shape sketch:** `Sparkline(cfg)` takes `[]float64` (or `[][]float64` for
  multi-series), renders inline SVG with `viewBox` auto-normalized. No JS,
  no hydration — pure render. Pairs with `StatCard`.
- **Pre-reqs:** None. Independent.

---

## Next wave — proposed

A second-wave gap list, prioritized by what blocks the most apps and
what unblocks the deferred items above. Every entry below is sized to
fit cleanly in `framework/ui/` (styled molecule) or
`core-ui/patterns/` (composed-but-themeable pattern), with sensible
defaults from theme tokens — no app-level CSS required to make them
look right out of the box.

### Tier 1 — basic primitive gaps

These are missing 1:1 with native HTML controls. Apps work around them
today by reaching into `core-ui/html/` and re-doing the same theme
work over and over.

1. **Switch / Toggle** (`framework/ui/`) — settings-row toggle paired
   with a label and optional helper text. Variant of Checkbox but with
   the on/off sliding affordance. Required because Checkbox is wrong
   for binary settings UX-wise. `data-fui-action="form-submit"`
   support so it can auto-save on change.
2. **Slider / Range** (`framework/ui/`) — `<input type="range">`
   wrapped with a styled track + thumb, optional min/max/step labels,
   optional value bubble. Used everywhere from volume controls to A/B
   percentage knobs.
3. **NumberInput / Stepper** (`framework/ui/`) — bound `<input
   type="number">` with explicit +/- buttons (touch-friendly), min/
   max/step, on-blur clamp. Date Picker needs this. Inventory forms
   need this.
4. **TextArea** (`framework/ui/`) — first-class component. The runtime
   already ships `data-fui-autogrow`; expose it as a typed
   `TextAreaConfig{Autogrow bool, …}` field. Pairs with FormField.

### Tier 2 — composite molecules

Small compositions that come up repeatedly and don't have a clean
spot in user code today.

5. **MultiSelect** (`core-ui/patterns/multiselect/`) — Combobox
   sibling. Checkbox-rows in the listbox, removable chips above the
   input. Server-driven option list via RPC like Combobox; selection
   bound to a comma-separated hidden input for plain form posts.
6. **FileDropzone** (`framework/ui/`) — already have `FileUpload`;
   FileDropzone wraps it with the drag-and-drop UX and a thumbnail
   strip for previews. Reuses the existing upload pipeline.
7. **Banner / InlineAlert** (`framework/ui/`) — persistent page-top
   status banner (maintenance notice, deprecation warning, billing
   alert). Distinct from Toast (transient) and Notification
   (record-bound). Variants: info / warn / danger / success.
   Optional Dismissible + persisted-dismiss-id.
8. **StatCard / KPI** (`framework/ui/`) — the `.demo-stat-card`
   pattern the website built ad-hoc; should be a real component with
   Label / Value / Delta / DeltaTrend and optional inline Sparkline
   slot. Used in every dashboard.
9. **Stepper / ProgressSteps** (`core-ui/patterns/steps/`) —
   horizontal/vertical step indicator showing current + completed +
   upcoming. Pairs with the deferred Form Step Wizard.
10. **Timeline** (`framework/ui/`) — vertical list of events on a
    rail, each event a slot (icon + label + meta + body). Common in
    audit logs, activity feeds, order history.

### Tier 3 — specialty

Lower-frequency but high-value once the basics are in.

11. **Drawer side-panel preset** (`core-ui/widget/preset/`) — modal
    preset exists; the drawer preset is the natural sibling. Anchored
    to a viewport edge (start / end / top / bottom), focus trap,
    backdrop click, ESC close. Used today via raw widget config; a
    typed preset closes the gap with `Modal`.
12. **ColorPicker** (`framework/ui/`) — `<input type="color">` with a
    styled swatch trigger + optional preset swatches grid. Hue/sat
    picker is out of scope (use native).
13. **RatingInput** (`framework/ui/`) — 1-N keyboard-accessible
    star/heart rating with `<input type="radio">` underneath for
    no-JS submit. Hover-preview via CSS only.
14. **Toast container preset** (`core-ui/widget/preset/`) — runtime
    already exposes `data-fui-comp="ui-toast-stack"`; a typed preset
    config (position / max-visible / dedupe-by-id) would let apps
    swap container shape without touching CSS.

### Notes on the contract

Every entry above must satisfy the same five rules the first wave
already meets:

- Owns its CSS — `RegisterStyle` in the component's own package, not
  in the example website or any app.
- Theme-token-only colors / radii / spacing — no hard-coded hex /
  px values except as `var(--…, fallback)`.
- WCAG 2.5.5 touch-target by default (44×44), with an explicit
  opt-out variant (like `ButtonSizeSmall`) for dense contexts.
- SSR-first; runtime hydration only for genuine interactivity
  (combobox keyboard, drag-drop, etc.) loaded via the demand-load
  module split.
- Chromedp interaction test (not just attribute test) before merge —
  per the component-build skill.
