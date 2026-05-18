# UI component roadmap

Tracks components that are known-missing from `core-ui/` and `framework/ui/`
but deferred — either by design (out of current scope), by scheduling (own
worktree), or because the shape isn't settled yet. Picked-up items should be
removed from this file when they land.

Source: gap audit on branch `worktree-staged-roaming-whale` (2026-05-17).

---

## In-flight (this worktree)

Tracked here for context only — these are the in-scope items for the
current series of PRs, not part of the deferred backlog.

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
