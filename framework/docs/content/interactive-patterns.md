# Interactive patterns

The runtime ships client-side interactive behavior through `data-fui-*`
attributes on regular HTML elements. No JavaScript is required from the
application author — the runtime's click delegation, IntersectionObserver,
and module system handle everything.

This doc catalogs every interactive pattern the framework provides, grouped
by whether the behavior is **client-only** (no server round-trip) or
**RPC-backed** (fires a fetch, updates the page).

---

## Client-only patterns

These run entirely in the browser. No network request is made.

### Signal state

Signals are named string values stored in the DOM. The runtime
provides three mutation primitives triggered by click:

| Attribute | Effect |
|---|---|
| `data-fui-signal-set="name:value"` | Sets signal `name` to `value` |
| `data-fui-signal-inc="name"` | Increments signal `name` by 1 (or by `data-fui-signal-delta`) |
| `data-fui-signal-toggle="name"` | Flips signal `name` between `"true"` and `"false"` |

Any element carrying a `data-fui-signal` attribute renders the current
value of that signal as its text content. The runtime updates it on
mutation and flashes a brief `.fui-flash` highlight (skipped when
`prefers-reduced-motion: reduce` is active).

Go helpers: `interactive.SetLocal()`, `interactive.IncLocal()`,
`interactive.ToggleLocal()`.

### Counter

`framework/ui.Counter` renders a numeric counter with +/− buttons.
Uses `data-fui-signal-inc` under the hood. Configurable `Step` for
non-unit increments.

### Tabs

`framework/ui.Tabs` renders a signal-driven tab strip. Clicking a tab
sets the signal to the tab's index; CSS attribute selectors show/hide
the matching panel. No JavaScript beyond the runtime's click delegation.

### Toggle Switch

`framework/ui.SignalToggle` renders a `role=switch` with
`aria-checked` bound to a boolean signal. Clicking toggles the signal
between `"true"` and `"false"`.

### Collapsible

`framework/ui.Collapsible` wraps native `<details>` with
`data-fui-disclosure` for keyboard support (Escape to close) and
`aria-expanded` mirroring. The browser handles open/close natively.

### Copy to clipboard

`framework/ui.CopyButton` renders a button that copies text to the
clipboard via `navigator.clipboard.writeText()`. The runtime module
(`copy.js`) shows a brief "Copied!" state and announces it to screen
readers. Works with a `document.execCommand('copy')` fallback.

### Password visibility toggle

`framework/ui.PasswordInput` renders a password field with an eye icon
that toggles between `type="password"` and `type="text"`. The runtime
module (`passwordinput.js`) handles the click and type switch.

### Textarea auto-resize

`framework/ui.TextArea` accepts `Autogrow: true`. The runtime module
(`textarea.js`) listens for input and resizes the textarea to fit its
content. Triggered by the `data-fui-autogrow` attribute.

### Toast notifications

`core-ui/widget/preset.ToastStack` renders a slide-in notification
stack. The runtime module (`toasts.js`) is pure client-side — toasts
auto-dismiss with a TTL, pause on hover/focus, and can be dismissed
by clicking the close button.

### Theme toggle

`framework/ui.ThemeToggle` renders a dark/light/auto switch. The
runtime (`themeswitch.js`) persists the preference in `localStorage`
and toggles the `color-scheme` meta + root attribute.

### Scroll spy

`core-ui/patterns/scrollspy` uses IntersectionObserver to track which
section is currently in the upper portion of the viewport and marks
the corresponding nav link as active. Triggered by
`data-fui-scrollspy`.

---

## RPC-backed patterns

These fire an HTTP request to the server and update the page based on
the response. The runtime handles `fetch()`, CSRF tokens, and DOM
updates.

### OnClick (button → server → signal)

`interactive.OnClick(html, action)` wraps any element so clicking it
fires an RPC. The `Action` specifies the HTTP method, path, and
optional effects (set signal, open widget, navigate).

Attributes injected: `data-fui-rpc`, `data-fui-rpc-method`,
`data-fui-rpc-signal`.

### OnSubmit (form → server → signal)

`interactive.OnSubmit(form, action)` wraps a `<form>` so submission
fires via `fetch()` instead of a full-page reload. The response body
writes into the named signal.

Attributes injected: `data-fui-rpc` (on the form element),
`data-fui-rpc-trigger="submit"`.

### Live Search (debounced input → RPC)

`interactive.LiveSearch(form, action, debounceMs)` wraps a search form
so typing fires debounced RPCs. The input event triggers the fetch
after the specified debounce interval (default 300ms).

Attributes injected: `data-fui-rpc-trigger="input"`,
`data-fui-rpc-debounce-ms` (milliseconds; default 300).

### Optimistic Update (immediate visual + background RPC)

`interactive.OptimisticUpdate(action, idle, success)` renders a button
that immediately flips to its success visual on click, then fires the
RPC in the background. On failure the button shakes and reverts.

Uses the `optimisticaction.js` runtime module.

### Toggle Action (three-state commit/untoggle)

`framework/ui` ships a `ToggleAction` component — a three-state button
(idle → committed → idle) with optional mutex groups. See
`toggleaction.js`.

### Inline Edit helpers

`interactive.EditToggle(html, signalName)` and
`interactive.CancelEdit(html, signalName)` provide semantic wrappers
for click-to-edit patterns. `EditToggle` uses `data-fui-signal-toggle`
to enter edit mode; `CancelEdit` uses `data-fui-signal-set="name:false"`
to close it. The actual save uses `interactive.OnSubmit`.

### Navigate (button → server → SPA page change)

`interactive.OnClick` with a `Navigate` effect replaces the page
content via the runtime's SPA navigation — no full browser reload.

### Confirm (pre-flight confirmation dialog)

`interactive.Confirm(message)` shows a `window.confirm` dialog before the
RPC fires. If the user cancels, the request is aborted. Use for destructive
actions (delete, revoke, bulk operations).

```go
interactive.OnClick(deleteBtn,
    interactive.Delete("/api/items/42").OnSuccess(
        interactive.Confirm("Delete this item? This cannot be undone."),
    ),
)
```

Attribute injected: `data-fui-confirm="message"`.

### AfterText (one-shot button label swap on success)

`interactive.AfterText(text)` replaces the trigger element's text content
with `text` after a 2xx response. One-shot — re-clicks are idempotent. Pair
with `AfterDisable` for "Saved ✓" feedback.

```go
interactive.OnClick(saveBtn,
    interactive.Post("/api/save").OnSuccess(
        interactive.AfterText("Saved ✓"),
        interactive.AfterDisable(),
    ),
)
```

Attribute injected: `data-fui-rpc-after-text="text"`.

### AfterDisable (permanently disable trigger on success)

`interactive.AfterDisable()` sets `aria-disabled="true"` and `disabled` on
the trigger after a 2xx response. Use with `AfterText` to prevent re-submission.

Attribute injected: `data-fui-rpc-after-disable` (boolean).

### ScrollTo (scroll to newly-added content on success)

`interactive.ScrollTo(selector)` smooth-scrolls the element matching
`selector` into view after a 2xx response. Use to direct the user's eye at
content that the RPC just inserted.

```go
interactive.OnClick(addBtn,
    interactive.Post("/api/items").OnSuccess(
        interactive.ScrollTo("#items-list"),
    ),
)
```

Attribute injected: `data-fui-rpc-scroll-to="selector"`.

### PushState (update URL without re-fetch on success)

`interactive.PushState(path)` applies a URL change via `history.pushState`
after a 2xx response without triggering a navigation fetch. Use for actions
that know the canonical URL ahead of time (e.g. pagination).

The server-supplied `X-Gofastr-Push-State` response header takes precedence
when both are present.

```go
interactive.OnClick(page2Btn,
    interactive.Post("/islands/items/page").OnSuccess(
        interactive.PushState("?p=2"),
    ),
)
```

Attribute injected: `data-fui-push-state="path"`.

---

## Complex interactive components

These are full components (not wrapper functions) that ship with their
own runtime modules for rich client-side behavior.

| Component | Runtime module | Behavior |
|---|---|---|
| Carousel | `carousel.js` | Prev/next navigation, pagination dots, keyboard, auto-rotation |
| Combobox | `combobox.js` | Debounced search RPC, listbox navigation, type-ahead |
| Command Palette | (uses Modal + Combobox) | ⌘K overlay with search |
| Conditional Field | `conditionalfield.js` | Show/hide form sections based on field values |
| Drag Sortable List | `sortablelist.js` | Native drag-and-drop + keyboard reorder, RPC commit |
| File Dropzone | `dropzone.js` | Drag-and-drop file handling with previews |
| Gallery + Lightbox | `lightbox.js` | Image zoom overlay, prev/next, keyboard |
| Infinite Scroll | `infinitescroll.js` | IntersectionObserver-driven lazy loading |
| Menu | `menu.js` | Keyboard navigation (arrows, Home/End, type-ahead) |
| Multi-select | `multiselect.js` | Checkbox group with chip display |
| Notification Bell | (uses Popover) | Bell + unread badge + dropdown |
| Popover | `popover.js` | Anchored positioning, auto-flip, arrow drawing |
| Range Slider | `rangeslider.js` | Dual-thumb with cross-clamp |
| Slider | `slider.js` | Live value mirror |
| Tag Input | `taginput.js` | Free-form chips, Enter/comma to commit |
| Tree | `tree.js` | WAI-ARIA tree pattern, roving tabindex, expand/collapse |
| Network Retry Banner | `networkretrybanner.js` | Auto-show on RPC failure threshold, retry button |
| Animated Counter | `animatedcounter.js` | IntersectionObserver-driven number tick animation |
| Banner | `banner.js` | Dismissible with optional persistence |

---

## Using the interactive package

```go
import "github.com/DonaldMurillo/gofastr/core-ui/interactive"

// Button that increments a client-side counter
btn := interactive.IncLocal(
    html.Button(html.ButtonConfig{Label: "+1"}),
    "my-counter",
)

// Form that submits via RPC without page reload
form := interactive.OnSubmit(
    myForm,
    interactive.Action{Path: "/api/save", Method: "POST"},
)

// Live search with 300ms debounce
search := interactive.LiveSearch(
    searchForm,
    interactive.Action{Path: "/api/search"},
    300,
)
```

---

## See also

- [`docs/ui-new-components.md`](ui-new-components.md) — full component catalog.
- [`docs/widgets.md`](widgets.md) — widget framework (Modal, Drawer, Popover mounts).
- [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) — runtime contract + attribute reference.
- [`docs/ui-getting-started.md`](ui-getting-started.md) — first-time UI setup.
