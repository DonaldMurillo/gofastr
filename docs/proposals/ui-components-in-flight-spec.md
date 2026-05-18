# UI Components In-Flight Spec

This document is the implementation contract for 11 new UI components landing
in `core-ui/html`, `core-ui/patterns/<dir>`, and `framework/ui/`. Each section
defines API, rendered shape, ARIA, keyboard, mobile, RPC, and e2e scenarios.

## Conventions errata (read first)

This spec was drafted as a starting contract. The following framework
conventions take precedence over any conflict in the per-component sections:

- **Class prefix**: `framework/ui` components use `ui-*` classes (matching
  existing `ui-tag`, `ui-card`, etc.) — not `fui-*`. Patterns use their own
  package-named class root (e.g. `accordion`, `pagination`).
- **No inline `style=` attributes** anywhere. Strict CSP strips them. Use
  classes + CSS custom properties or `data-*` hooks instead. The component's
  stylesheet is registered via `registry.RegisterStyle`.
- **Runtime attributes the spec sometimes invents** — use only what's documented
  in `core-ui/ARCHITECTURE.md` and `core-ui/runtime/runtime.js`. Specifically:
  - There is no `data-fui-close` or `data-fui-rpc-after-close`. Modal close
    is driven by `data-fui-rpc-close` (on a button inside a form/RPC),
    Escape (handled by the runtime for disclosures + widget overlays), or
    backdrop click (`data-fui-backdrop`).
  - For widget close on confirm-RPC success, the button gets
    `data-fui-rpc-close` and the runtime walks up to the containing widget.
- **Required-field panics**: every component panics at SSR time when a
  required config field is missing — matches the convention already used by
  `Tag`, `Heading`, etc.
- **Test conventions**: unit tests under `*_test.go`; chromedp e2e tests live
  under `examples/website/` and are gated by `TestE2E`. Each new component
  gets at least one rendered + one keyboard scenario.

Wherever the per-component section disagrees with the above, the above wins.

---

# Components

## 1. Kbd

- **Layer**: `core-ui/html`
- **File(s)**: `core-ui/html/text.go` (existing), `core-ui/html/html_test.go`
- **Purpose**: Renders the semantic `<kbd>` element representing user input or keyboard shortcuts.
- **Go API**:
  ```go
  // Kbd produces a <kbd> element. Use TextConfig (shared with Code, Pre,
  // Small, Mark, etc.).
  func Kbd(cfg TextConfig, children ...render.HTML) render.HTML
  ```
- **Rendered HTML shape**:
  ```html
  <kbd class="${Class}" id="${ID}">${children}</kbd>
  ```
- **ARIA / Accessibility**: Implicit `<kbd>` semantics. Not focusable.
  Contrast 4.5:1 against background when styled.
- **Keyboard / Mobile**: N/A (passive).
- **chromedp e2e**: Renders `<kbd>`; honors Class.
- **Edge cases**: empty children → empty `<kbd>` (no panic — matches Code/Pre).

## 2. AvatarGroup

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/avatargroup.go`, `framework/ui/avatargroup_test.go`
- **Purpose**: Overlapping stack of avatars with a `+N` overflow indicator.
- **Go API**:
  ```go
  type AvatarGroupConfig struct {
      Avatars []AvatarConfig  // required
      Max     int             // default 5 — render up to Max, then "+N"
      Size    AvatarSize      // propagates to children (default reuses AvatarSizeMD)
      Label   string          // aria-label on the group; default "Avatars"
      ID      string
      Class   string
  }
  func AvatarGroup(cfg AvatarGroupConfig) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <div class="ui-avatar-group ui-avatar-group--md" role="group" aria-label="Avatars">
      <span class="ui-avatar-group__item">…Avatar…</span>
      <span class="ui-avatar-group__item">…Avatar…</span>
      <span class="ui-avatar-group__overflow" aria-label="2 more">+2</span>
  </div>
  ```
- **ARIA**: role=group, aria-label. Overflow has aria-label="N more" (number rendered as text too).
- **Keyboard**: Standard Tab order if any child Avatar is a link.
- **Mobile**: Stack uses CSS negative margin via class, no inline styles. ≥24px overlap touch tolerant.
- **chromedp e2e**:
  1. Renders exactly Max avatars when len(Avatars) > Max.
  2. Renders +(len-Max) overflow indicator with correct number.
  3. Renders no overflow when len(Avatars) ≤ Max.
  4. role="group" + aria-label present.
- **Edge cases**: 0 avatars → panic; 1 avatar → renders single avatar, no overflow.

## 3. CopyButton

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/copybutton.go`, `framework/ui/copybutton_test.go`
- **Purpose**: Copy-to-clipboard button driven by `data-fui-copy-text-from`.
- **Go API**:
  ```go
  type CopyButtonConfig struct {
      Target      string // required: CSS selector of the source element
      Label       string // visible button text; default "Copy"
      CopiedLabel string // post-success label; default "Copied"
      IconOnly    bool   // hide visible label, keep aria-label
      AriaLabel   string // when IconOnly=true; default "Copy to clipboard"
      ID          string
      Class       string
  }
  func CopyButton(cfg CopyButtonConfig) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <button type="button" class="ui-copy-btn"
          data-fui-copy-text-from="#code-1"
          data-fui-rpc-after-text="Copied"
          aria-live="polite">
      Copy
  </button>
  ```
  (Reuses existing `data-fui-rpc-after-text` mechanic; the runtime hook for
  copy already fires it as a synthetic "RPC" success once the clipboard API
  resolves. If the runtime doesn't yet do this, the implementation adds the
  hook with a runtime-test update — and updates `core-ui/ARCHITECTURE.md`.)
- **ARIA**: aria-live="polite" so SR announces the label change. role=button.
- **Keyboard**: Tab focus; Enter/Space to copy.
- **Mobile**: ≥44×44 tap target enforced by `ui-copy-btn` class.
- **chromedp e2e**:
  1. Clicking the button copies the target's text content.
  2. Label updates to "Copied" after success.
  3. aria-label present when IconOnly.
- **Edge cases**: Target selector returns nothing → no-op, button stays. SR not announced.

## 4. ShortcutHint

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/shortcuthint.go`, `framework/ui/shortcuthint_test.go`
- **Purpose**: Renders a keyboard chord (e.g. `⌘K`, `↵`, `Esc`) as a row of
  `<kbd>` chips. Optionally binds the chord to a target via the runtime's
  `data-fui-shortcut-click` / `data-fui-shortcut-focus` hooks.
- **Go API**:
  ```go
  type ShortcutHintConfig struct {
      Chord       string // required: "Meta+K", "Ctrl+/", "/", "Esc", etc.
      BindTarget  string // optional: target selector; needs BindAction
      BindAction  string // "click" | "focus"; default "click" when BindTarget set
      SROnlyLabel string // override SR text; default derived from Chord
      Class       string
      ID          string
  }
  func ShortcutHint(cfg ShortcutHintConfig) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <span class="ui-shortcut-hint"
        data-fui-shortcut-click="Meta+K"  <!-- on the hint OR on the target -->
        aria-hidden="false">
      <kbd class="ui-shortcut-hint__key">⌘</kbd>
      <kbd class="ui-shortcut-hint__key">K</kbd>
      <span class="ui-sr-only">Shortcut: Command-K</span>
  </span>
  ```
  When `BindTarget` is set, the data-fui-shortcut-* attribute goes on the
  TARGET (matches runtime semantics — the runtime looks at the element to
  click/focus, not the visual hint). The visual hint becomes a sibling.
- **ARIA**: SR text via `.ui-sr-only`. `<kbd>` chips are visual; hidden from
  SR via `aria-hidden="true"` on the `<kbd>` wrapper to avoid double-read.
- **Keyboard**: Visual only when not bound; global chord when bound.
- **Mobile**: Hidden via `@media (pointer: coarse) { .ui-shortcut-hint { display: none } }`
  to avoid showing keyboard hints to touch users.
- **chromedp e2e**:
  1. "Meta+K" renders two `<kbd>` chips.
  2. "/" renders one chip.
  3. With BindTarget, the target gets `data-fui-shortcut-click` and pressing
     the chord clicks the target.
  4. SR-only label present.
- **Edge cases**: empty Chord → panic; unknown modifier → render literal text.

## 5. SegmentedControl

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/segmented.go`, `framework/ui/segmented_test.go`
- **Purpose**: Radiogroup styled as a compact pill toggle bar with sliding
  indicator. Native radio inputs are the source of truth — CSS slides the
  indicator using `:has(input:checked)`.
- **Go API**:
  ```go
  type SegmentedOption struct {
      Label string  // required
      Value string  // required
  }
  type SegmentedControlConfig struct {
      Name     string             // required: form field name
      Options  []SegmentedOption  // required: min 2
      Selected string             // default Options[0].Value
      Label    string             // aria-label; required when no surrounding <label>
      RPCPath  string             // optional: if set, change fires RPC
      RPCSignal string            // optional: pairs with RPCPath
      ID       string
      Class    string
  }
  func SegmentedControl(cfg SegmentedControlConfig) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <div class="ui-segmented" role="radiogroup" aria-label="View mode">
      <label class="ui-segmented__option">
          <input type="radio" name="view" value="day" checked
                 data-fui-rpc="..." data-fui-rpc-method="POST" data-fui-rpc-signal="...">
          <span class="ui-segmented__label">Day</span>
      </label>
      <label class="ui-segmented__option">
          <input type="radio" name="view" value="week">
          <span class="ui-segmented__label">Week</span>
      </label>
      <span class="ui-segmented__indicator" aria-hidden="true"></span>
  </div>
  ```
  Native radios provide keyboard nav for free (browser handles arrows).
  The visual indicator is a CSS sibling positioned via `:has()` selector.
- **ARIA**: role=radiogroup, aria-label. Native radios get implicit radio role.
- **Keyboard**: Browser-native — Tab to focused option; Arrow left/right
  cycles selection; Space/Enter selects (already checked items, no-op).
- **Mobile**: Each `<label>` is a tap target; ≥44px height enforced.
- **chromedp e2e**:
  1. Tab → first focusable option gets focus.
  2. Arrow Right → next option becomes checked (browser native).
  3. With RPCPath, change fires RPC and signal broadcasts.
  4. role=radiogroup + aria-label present.
- **Edge cases**: <2 options → panic; Selected not in Options → default to first.

## 6. ConfirmAction

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/confirmaction.go`, `framework/ui/confirmaction_test.go`
- **Purpose**: Declarative trigger+modal pair for destructive confirmations.
  Eliminates the boilerplate of building a hidden Modal preset per delete button.
- **Go API**:
  ```go
  type ConfirmActionConfig struct {
      Name         string  // required: widget name (unique per page)
      TriggerLabel string  // required: button text
      TriggerVariant string // "danger" | "primary" | ...; default "danger"
      Title        string  // required: dialog title
      Body         string  // required: dialog body text
      ConfirmLabel string  // default "Confirm"
      CancelLabel  string  // default "Cancel"
      RPCPath      string  // required: POST endpoint
      RPCBody      string  // optional: static JSON body
      DangerStyle  bool    // when true, alertdialog + focus-on-cancel (default true)
  }
  // ConfirmAction returns the trigger button HTML AND registers a Modal
  // preset under cfg.Name. Caller mounts the preset once at app startup.
  func ConfirmAction(cfg ConfirmActionConfig) (trigger render.HTML, preset *widget.Builder)
  ```
- **Rendered HTML** (trigger + preset chrome):
  ```html
  <button class="ui-btn ui-btn--danger" data-fui-open="confirm-delete-42">Delete</button>

  <!-- preset-mounted -->
  <div class="ui-modal" data-fui-widget="confirm-delete-42" role="alertdialog"
       aria-modal="true" aria-labelledby="confirm-delete-42-title"
       aria-describedby="confirm-delete-42-body" hidden>
      <h2 id="confirm-delete-42-title">Delete user</h2>
      <p id="confirm-delete-42-body">This cannot be undone.</p>
      <div class="ui-modal__actions">
          <button type="button" data-fui-rpc-close autofocus>Cancel</button>
          <button type="button" class="ui-btn--danger"
                  data-fui-rpc="/users/42/delete" data-fui-rpc-method="POST"
                  data-fui-rpc-close>Confirm</button>
      </div>
  </div>
  ```
- **ARIA**: role="alertdialog" (vs plain "dialog" for non-destructive),
  aria-modal, aria-labelledby, aria-describedby.
- **Focus**: Opens with focus on Cancel (DangerStyle=true) to prevent
  accidental confirm. Returns to trigger on close (handled by preset.Modal).
- **Keyboard**: Tab cycles Cancel → Confirm (preset handles trap). Esc closes.
- **Mobile**: Inherits preset.Modal mobile chrome (full-screen on narrow).
- **chromedp e2e**:
  1. Trigger click opens dialog; focus lands on Cancel.
  2. Tab → focus moves to Confirm.
  3. Esc closes; focus returns to trigger.
  4. Click Confirm → RPC fires; dialog closes on 2xx.
  5. Backdrop click closes.
- **Edge cases**: RPC error → modal stays open, retry possible.

## 7. FilterChipBar

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/filterchipbar.go`, `framework/ui/filterchipbar_test.go`
- **Purpose**: Toolbar of active filters above a DataTable. Each chip is
  dismissible via island RPC. Optional "Clear all" trailing action.
- **Go API**:
  ```go
  type FilterChip struct {
      Label       string  // required: e.g. "Status: Active"
      DismissPath string  // required: POST endpoint that removes this filter
      DismissBody string  // optional: static JSON body
  }
  type FilterChipBarConfig struct {
      Filters       []FilterChip  // required: 0 ok (renders empty toolbar)
      ClearAllPath  string        // optional: when set, render Clear all button
      Label         string        // aria-label; default "Active filters"
      RPCSignal     string        // optional: shared signal name for re-render
      ID            string
      Class         string
  }
  func FilterChipBar(cfg FilterChipBarConfig) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <div class="ui-filter-bar" role="toolbar" aria-label="Active filters">
      <ui-tag …with Dismiss=DismissPath…/>  <!-- composes Tag with Dismiss -->
      <ui-tag …/>
      <button class="ui-filter-bar__clear" data-fui-rpc="/filters/clear"
              data-fui-rpc-method="POST" data-fui-rpc-signal="filter-bar">Clear all</button>
  </div>
  ```
- **ARIA**: role=toolbar, aria-label. Each chip's × button has aria-label="Remove <label>".
- **Keyboard**: Standard Tab between dismiss buttons. Enter/Space removes.
  Arrow Left/Right roving tabindex is a nice-to-have (out of scope for v1).
- **Mobile**: Wraps via flex-wrap; ≥44×44 dismiss targets.
- **chromedp e2e**:
  1. Renders N chips when len(Filters)=N.
  2. Click × on chip → RPC fires; chip removed from DOM.
  3. role=toolbar + aria-label present.
  4. "Clear all" appears only when ClearAllPath set.
- **Edge cases**: 0 filters → renders empty `<div>` (still valid; consumer can hide).

## 8. InfiniteScroll

- **Layer**: `core-ui/patterns/infinitescroll`
- **File(s)**: `core-ui/patterns/infinitescroll/{infinitescroll.go, config.go, css.go, doc.go, infinitescroll_test.go}`
- **Purpose**: Sentinel-based infinite scroll: as user scrolls near the
  bottom, the runtime fires an RPC and appends the returned HTML to a
  container. Includes a `<noscript>` fallback "Load more" button.
- **Go API**:
  ```go
  type Config struct {
      ID          string         // required
      RPCPath     string         // required: GET endpoint; returns next page HTML
      Items       []render.HTML  // required: first page (SSR)
      ItemsClass  string         // class for the items container
      Cursor      string         // initial server-side cursor (next-page token)
      RootMargin  string         // IntersectionObserver rootMargin; default "200px"
      Class       string
      AriaLabel   string         // default "Feed"
  }
  func InfiniteScroll(cfg Config) render.HTML
  ```
- **New runtime attribute**: `data-fui-infinite-scroll="<rpcPath>"` —
  on the wrapper. Pair with `data-fui-infinite-cursor="<token>"` (initial
  cursor) and `data-fui-infinite-root-margin="<px>"`. Sentinel element
  carries `data-fui-infinite-sentinel`. Document in ARCHITECTURE.md; add
  runtime test.
- **Rendered HTML**:
  ```html
  <div class="infinitescroll" data-fui-infinite-scroll="/feed/page"
       data-fui-infinite-cursor="abc123" role="feed" aria-busy="false"
       aria-label="Feed">
      <div class="infinitescroll__items">
          …items…
      </div>
      <div class="infinitescroll__sentinel" data-fui-infinite-sentinel aria-hidden="true"></div>
      <noscript>
          <form action="/feed/page" method="get">
              <input type="hidden" name="cursor" value="abc123">
              <button class="infinitescroll__loadmore" type="submit">Load more</button>
          </form>
      </noscript>
  </div>
  ```
- **ARIA**: role=feed, aria-busy toggles during fetch, aria-label.
- **Keyboard**: No-JS fallback `<form>` is keyboard-operable.
- **Mobile**: rootMargin tuned for touch scroll; `aria-busy` flips a CSS class for a subtle loading bar.
- **State / RPC**: Runtime POSTs cursor; response carries new HTML and an
  `X-Gofastr-Infinite-Cursor` response header. Empty cursor signals end —
  runtime removes the sentinel.
- **chromedp e2e**:
  1. Renders Items on first paint.
  2. Programmatically scroll past sentinel → RPC fires; new items appended.
  3. aria-busy toggles true → false across the fetch.
  4. End-of-feed (empty cursor header) removes sentinel.
  5. With JS disabled (`<noscript>`), "Load more" form is present.
- **Edge cases**: Fast scrolls fire only one in-flight request (runtime guards). Error → retry.

## 9. Combobox

- **Layer**: `core-ui/patterns/combobox`
- **File(s)**: `core-ui/patterns/combobox/{combobox.go, config.go, css.go, doc.go, combobox_test.go}`
- **Purpose**: Async typeahead — debounced input + RPC dropdown listbox.
  Follows WAI-ARIA Combobox 1.2 pattern (with `aria-controls` listbox).
- **Go API**:
  ```go
  type Config struct {
      ID          string  // required
      Name        string  // required: form field name
      Label       string  // required: visible <label> text
      RPCPath     string  // required: server returns <li role=option> fragments
      DebounceMs  int     // default 250
      Placeholder string
      EmptyText   string  // shown when 0 results; default "No results"
      Class       string
  }
  func Combobox(cfg Config) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <div class="combobox" id="city-combo">
      <label for="city-combo-input">City</label>
      <input id="city-combo-input" name="city" type="text"
             role="combobox" aria-autocomplete="list"
             aria-controls="city-combo-listbox" aria-expanded="false"
             aria-activedescendant=""
             data-fui-rpc="/cities/search" data-fui-rpc-method="POST"
             data-fui-rpc-trigger="input" data-fui-rpc-debounce-ms="250"
             data-fui-rpc-signal="city-combo-options"
             autocomplete="off">
      <ul id="city-combo-listbox" role="listbox"
          data-fui-signal="city-combo-options" data-fui-signal-mode="html"
          hidden></ul>
  </div>
  ```
- **ARIA**: role=combobox on input, role=listbox on `<ul>`, role=option on
  each `<li>`. `aria-expanded` toggles when results render.
  `aria-activedescendant` reflects the highlighted option's id.
- **Keyboard**: Arrow Down opens listbox + highlights first; Arrow Up/Down
  move; Enter selects (fills input + closes); Esc closes (keeps text);
  Tab closes + moves focus naturally.
- **Mobile**: Listbox is positioned absolutely under input; on narrow viewports,
  collapses to a stacked dropdown with full-width.
- **State / RPC**: Input fires debounced POST `{query: "..."}`; server returns
  `<li role="option" id="...">…</li>` HTML which the runtime swaps into the listbox.
- **chromedp e2e**:
  1. Type 2 chars → debounced RPC fires once.
  2. Listbox shows on response; aria-expanded=true.
  3. Arrow Down → first option gets highlight class; aria-activedescendant updates.
  4. Enter → input value updated; listbox hidden; aria-expanded=false.
  5. Esc closes without selecting.
  6. Empty results → renders EmptyText with role=option aria-disabled.
- **Edge cases**: Slow network — debounce coalesces; only latest response renders. Stale-response guard via request sequence id.

## 10. TreeView

- **Layer**: `core-ui/patterns/tree`
- **File(s)**: `core-ui/patterns/tree/{tree.go, config.go, css.go, doc.go, tree_test.go}`
- **Purpose**: Recursive tree of items with optional lazy-load on expand.
  Follows WAI-ARIA Tree pattern (single selection, roving tabindex).
- **Go API**:
  ```go
  type Node struct {
      ID       string  // required: unique within tree
      Label    string  // required
      Children []Node  // optional: empty + LazyPath set → lazy load
      LazyPath string  // optional: GET endpoint returning child <li> nodes
      Selected bool
      Expanded bool
  }
  type Config struct {
      ID    string  // required
      Label string  // required: aria-label
      Nodes []Node  // required
      Class string
  }
  func TreeView(cfg Config) render.HTML
  ```
- **Rendered HTML**:
  ```html
  <ul class="tree" role="tree" aria-label="File system" id="files">
      <li role="treeitem" id="files-node-src" aria-expanded="false"
          aria-level="1" aria-posinset="1" aria-setsize="2" tabindex="0">
          <span class="tree__row">
              <button class="tree__toggle" aria-hidden="true"
                      data-fui-rpc="/tree/lazy?id=src" data-fui-rpc-method="GET"
                      data-fui-rpc-signal="tree-src">▶</button>
              <span class="tree__label">src</span>
          </span>
          <ul role="group" data-fui-signal="tree-src" data-fui-signal-mode="html" hidden></ul>
      </li>
  </ul>
  ```
- **ARIA**: role=tree, treeitem, group. aria-level, aria-posinset, aria-setsize,
  aria-expanded, aria-selected (when selectable).
- **Keyboard** (roving tabindex — only one treeitem has tabindex=0 at a time):
  - Arrow Down/Up → next/previous visible item
  - Arrow Right → expand if collapsed; else focus first child
  - Arrow Left → collapse if expanded; else focus parent
  - Home/End → first/last visible item
  - Enter/Space → toggle expand (and select if Selectable)
  - Type-ahead (a-z) → focus next item whose label starts with the typed prefix
- **Mobile**: Toggle button is the tap target (≥44×44).
- **State / RPC**: Expand on a node with LazyPath fires GET; response is HTML
  for the `<ul role=group>` content; runtime swaps via signal.
- **chromedp e2e**:
  1. Tab → first treeitem gets focus.
  2. Arrow Down → focus next sibling.
  3. Arrow Right on lazy node → RPC fires; children swap; aria-expanded=true.
  4. Arrow Right on expanded → focus moves to first child.
  5. Arrow Left → collapses.
  6. Type-ahead jumps to matching node.
- **Edge cases**: Empty tree → empty `<ul>`; lazy-load error → keep aria-expanded=false, show inline error.

## 11. CommandPalette

- **Layer**: `framework/ui`
- **File(s)**: `framework/ui/commandpalette.go`, `framework/ui/commandpalette_test.go`
- **Purpose**: ⌘K-triggered overlay combining Modal + Combobox for fuzzy
  command/nav search.
- **Go API**:
  ```go
  type CommandPaletteConfig struct {
      Name        string  // required: widget name (typically "command-palette")
      RPCPath     string  // required: POST /commands/search
      Placeholder string  // default "Type a command or search…"
      Shortcut    string  // default "Meta+K"
      EmptyText   string  // default "No commands match"
  }
  // Returns:
  //   trigger: a screen-reader button that opens the palette (also bound to Shortcut)
  //   preset:  Modal preset to mount once at startup
  func CommandPalette(cfg CommandPaletteConfig) (trigger render.HTML, preset *widget.Builder)
  ```
- **Rendered HTML**:
  ```html
  <button class="ui-sr-only" data-fui-open="command-palette"
          data-fui-shortcut-click="Meta+K">Open command palette</button>

  <!-- preset chrome (Modal preset, role=dialog) -->
  <div class="ui-cmd-palette" data-fui-widget="command-palette"
       role="dialog" aria-modal="true" aria-label="Command palette" hidden>
      <!-- internal combobox per Combobox spec, but listbox is always-visible -->
      <input role="combobox" aria-expanded="true" …>
      <ul role="listbox" …></ul>
  </div>
  ```
- **ARIA**: role=dialog + aria-modal on the overlay; combobox+listbox inside.
- **Focus**: Opens with focus on input; Esc closes; focus returns to trigger.
  Tab cycles within palette (handled by preset.Modal trap).
- **Keyboard**: Meta+K opens; Esc closes; Arrows + Enter as in Combobox.
- **Mobile**: Full-screen on narrow viewports (modal preset).
- **State / RPC**: Search RPC returns `<li role=option>` HTML. Selecting an
  option either triggers another RPC (e.g. "Navigate to /settings") via the
  option's `data-fui-rpc` attribute, or pushes a URL via `data-fui-push-state`.
- **chromedp e2e**:
  1. Meta+K opens palette; focus in input.
  2. Type → RPC fires; options render.
  3. Arrow + Enter → option's RPC/push-state fires.
  4. Esc closes; focus returns to body.
  5. Click outside (backdrop) closes.
- **Edge cases**: Empty results → EmptyText option (disabled).

---

# Done means

Each component is "done" when ALL of these are true:

1. Unit tests green (`go test ./<pkg>/...`).
2. chromedp e2e tests green (`go test ./examples/website/ -run TestE2E`) for
   every scenario listed above.
3. Manual SR smoke-test (VoiceOver Mac OS or NVDA Windows) confirms the
   ARIA labels announce correctly.
4. Focus indicators visible, focus order logical, no keyboard traps outside
   intentional dialog/alertdialog traps.
5. WCAG 2.2 AA color contrast on every styled state.
6. If a new `data-fui-*` attribute is added (InfiniteScroll only):
   `core-ui/ARCHITECTURE.md` table updated AND a runtime test added.
7. `docs/<component>.md` exists with at least one usage example.
8. The component is mounted in `examples/website` so the chromedp tests have
   a real surface to drive.
9. `./scripts/test-all.sh` passes end-to-end.
