# Interactive patterns

The runtime ships client-side interactive behavior through `data-fui-*`
attributes on regular HTML elements. No JavaScript is required from the
application author — the runtime's click delegation, IntersectionObserver,
and module system handle everything.

This doc catalogs every interactive pattern the framework provides, grouped
by whether the behavior is **client-only** (no server round-trip) or
**RPC-backed** (fires a fetch, updates the page).

Writing the `data-fui-*` attributes by hand instead of using a wrapper
helper? Jump to [Writing a hand-written island, end to end](#writing-a-hand-written-island-end-to-end)
— it walks a complete example and calls out the four things that trip up
almost everyone the first time (endpoint registration, the `name`→JSON-key
rule, select triggers, and the two route-param syntaxes).

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

`Action.WithConfirm(message)` gates the action behind a **pre-flight**
`window.confirm` dialog. The gate runs *before* the request is dispatched —
cancelling aborts it, so the RPC never fires. Use for destructive actions
(delete, revoke, bulk operations).

```go
interactive.OnClick(deleteBtn,
    interactive.Delete("/api/items/42").
        WithConfirm("Delete this item? This cannot be undone."),
)
```

Attribute injected: `data-fui-confirm="message"`.

> **Why a method, not an `OnSuccess` effect?** The confirm fires *before*
> the request, not after it succeeds. The older spelling —
> `interactive.Confirm(message)` passed to `OnSuccess(...)` — reads as if it
> runs on the response and has misled readers; it is deprecated but still
> emits the identical `data-fui-confirm` attribute, so existing code keeps
> working. Prefer `WithConfirm`, which reads in the order it executes.

`window.confirm` is native, unthemed, and **blocks browser automation**
(headless tests can't dismiss it without a dialog handler). For a
design-system-styled confirmation that matches the rest of your app and is
drivable by tests, use [`ui.ConfirmAction`](#themed-confirmation-uiconfirmaction)
instead. Native confirm remains the lightweight default; the themed dialog is
the opt-in upgrade.

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

## Writing a hand-written island, end to end

The wrapper helpers above cover the common cases, but sometimes you write the
`data-fui-*` attributes by hand — a bespoke widget, a generated screen, a
one-off control. The runtime is happy to drive raw attributes, but four
things trip up almost everyone the first time. This section walks a complete
example and calls each one out.

The example: a product list with a category `<select>` that swaps the list
via an RPC (an in-page state change — an island, **not** a route).

### 1. Register the endpoint yourself

`data-fui-rpc` is just a string the runtime POSTs to. **Nothing registers
that route for you.** The auto-wiring you may have seen belongs to
`widget.Mount` / `widget.MountBuilder`, which register a widget's
`/style.css`, `/state`, and `/chrome` routes — a *hand-written* `data-fui-rpc`
path has no widget behind it, so you add the handler on the app router:

```go
app.Router().Post("/islands/products/filter", http.HandlerFunc(filterProducts))
```

Forget this and the click fires a request that 404s — with no compile error
and nothing in the page to hint at the missing route. If a `data-fui-rpc`
button "does nothing", check the server log for a 404 first.

### 2. The JSON key is the input's `name`, not its `id`

When the runtime submits a form-backed RPC, it serializes the form to JSON
using each control's **`name`** attribute as the key (non-multipart forms go
out as `application/json`; see the forms note in
[`core-ui/ARCHITECTURE.md`](../../core-ui/ARCHITECTURE.md)). The `id` is for
CSS/labels and never appears in the body.

```html
<form data-fui-rpc="/islands/products/filter" data-fui-rpc-method="POST"
      data-fui-rpc-trigger="input" data-fui-rpc-debounce-ms="1">
  <select id="cat" name="category">   <!-- key is "category" (name), not "cat" (id) -->
    <option value="all">All</option>
    <option value="tools">Tools</option>
    <option value="parts">Parts</option>
  </select>
</form>
```

```go
func filterProducts(w http.ResponseWriter, r *http.Request) {
    var body struct{ Category string `json:"category"` } // matches name=, not id=
    _ = json.NewDecoder(r.Body).Decode(&body)
    // …render the filtered rows, write them back…
}
```

**Why this bites:** `curl -d 'category=tools'` hits the handler fine, so the
server looks correct in isolation. The mismatch only surfaces in a real
browser, where the runtime keys the body off `name`. Test the wired-up form
in a browser, not just the endpoint in isolation.

### 3. A `<select>` (or checkbox/radio) needs no `change` trigger

There is deliberately **no `data-fui-rpc-trigger="change"`.** Selects,
checkboxes, and radios all emit an `input` event on commit in every modern
browser, so `data-fui-rpc-trigger="input"` already fires for them — wrap the
control in a `<form data-fui-rpc … data-fui-rpc-trigger="input">` (as above)
and you're done. Adding a second `change` trigger would be redundant behavior
for a control the `input` trigger already covers, and the core runtime is
gzip-budget-locked, so the framework does not ship one.

For a `<select>` the `input`/`change` distinction doesn't matter (both fire
once, on selection), so set a small debounce — `data-fui-rpc-debounce-ms="1"`
— to fire promptly instead of waiting out the 250 ms default meant for
keystroke typeahead. This recipe is covered end-to-end by
`TestInputTrigger_SelectFiresRPC` in `core-ui/runtime`.

### 4. Route-param syntax: `{id}` on the HTTP router, `:id` on the screen router

Two routers, two placeholder syntaxes — crossing them is a **silent 404**:

| Router | Where you use it | Placeholder syntax |
|---|---|---|
| Framework HTTP router (`app.Router()`) | RPC endpoints, API routes, anything you `.Get`/`.Post` | Go 1.22 style: `/islands/products/{id}/filter` |
| UI screen router (`app.Register` / screen groups) | Page routes whose params reach `SetParams` | `:id` style: `/products/:id` |

So an RPC handler is registered with braces:

```go
app.Router().Post("/islands/products/{id}/filter", http.HandlerFunc(filterProducts))
// read it with r.PathValue("id")
```

…while a page route uses the colon form:

```go
app.Register("/products/:id", &ProductScreen{}, layout) // param arrives via ParamSetter.SetParams
```

Reach for `:id` on an `app.Router().Post(...)` path (or `{id}` on a screen
route) and the pattern simply never matches the request — no error, just a
404. When a route "isn't hit", check the placeholder syntax matches the
router you registered it on.

---

## Themed confirmation (`ui.ConfirmAction`)

`Action.WithConfirm` (above) uses native `window.confirm` — fine for
admin/internal tools, but unthemed and unautomatable. For a destructive
action in a user-facing surface, `framework/ui.ConfirmAction` renders a
design-system **alertdialog** instead: a modal that matches your theme
(light + dark, via tokens — zero bespoke CSS), traps focus, closes on Escape,
and is drivable by tests.

It's the same island contract you already know, composed from two existing
primitives — the trigger carries `data-fui-open` (open the modal), and the
modal's Confirm button carries the real `data-fui-rpc`. Cancel just closes;
only Confirm dispatches. No new runtime attributes, no core JS.

```go
trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:         "delete-user-42",   // unique per action; qualify per row
    TriggerLabel: "Delete",
    Title:        "Delete user?",
    Body:         "This permanently removes the user and their data.",
    ConfirmLabel: "Delete it",        // defaults to "Confirm"
    CancelLabel:  "Keep it",          // defaults to "Cancel"
    RPCPath:      "/users/42/delete", // the Confirm button POSTs here
})

// Mount the modal ONCE at app startup (registers its widget routes):
def := modal.Build()
widget.Mount(app.Router(), &def)

// Render `trigger` wherever the destructive button belongs; register the
// RPC handler yourself (rule 1 above still applies):
app.Router().Post("/users/42/delete", http.HandlerFunc(deleteUser))
```

Cancel is the initially-focused button by default (safer for destructive
flows — a stray Enter can't fire the action); set
`AutofocusConfirm: true` for benign "Apply changes?" confirmations. The
returned pair is deliberately two values: the trigger renders inline, the
modal mounts once — forgetting the `widget.Mount` is the usual "the dialog
never opens" cause.

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

An `Action` is built with the method constructors (`Post`, `Get`, `Put`,
`Delete`, `Patch`) and refined with chained methods (`.OnSuccess(...)`,
`.WithConfirm(...)`) — its fields are unexported, so there is no struct
literal.

```go
import "github.com/DonaldMurillo/gofastr/core-ui/interactive"

// Button that increments a client-side counter (delta 1)
btn := interactive.IncLocal(
    html.Button(html.ButtonConfig{Label: "+1"}),
    "my-counter", 1,
)

// Form that submits via RPC without page reload
form := interactive.OnSubmit(
    myForm,
    interactive.Post("/api/save"),
)

// Live search with 300ms debounce
search := interactive.LiveSearch(
    searchForm,
    interactive.Post("/api/search"),
    300,
)
```

---

## Common mistakes

- **Assuming a hand-written `data-fui-rpc` route is auto-registered.**
  Only `widget.Mount` wires routes automatically (for a widget's own
  style/state/chrome). A raw `data-fui-rpc="/x"` you write by hand needs
  its own `app.Router().Post("/x", …)` — otherwise the click 404s silently.
  See [Writing a hand-written island, end to end](#writing-a-hand-written-island-end-to-end).
- **Decoding the RPC body by the input's `id`.** The runtime keys the JSON
  body off each control's **`name`**, not its `id`. `curl` testing hides the
  mismatch; only a real browser exposes it. Same section, rule 2.
- **Turning in-page state changes into routes.** Sort, paginate,
  expand, tab-switch — these are islands (RPC swaps one fragment), not
  navigations. Adding a route (or `location.href = …`) for them is the
  architecture's named failure mode #1.
- **Re-implementing pagination/sort/filter math in JS.** The server
  owns that logic; the client's job is to fire the RPC and swap the
  returned HTML. Duplicated math drifts from the server's the first
  time either changes.
- **Treating signals as typed values.** Signals are strings stored in
  the DOM: `data-fui-signal-toggle` flips between the strings `"true"`
  and `"false"`, and `data-fui-signal-inc` parses-then-stringifies.
  Compare against string values (in CSS attribute selectors too), not
  booleans or numbers.
- **Using SSE to deliver an action's response.** SSE is push-only —
  background events for *other* clients. The result of a user action
  arrives in the RPC response itself (`data-fui-rpc-signal`, island
  swap), never via the event stream.
- **Inventing a new `data-fui-*` attribute without updating the
  contract.** Every attribute the runtime reads must land in
  `core-ui/ARCHITECTURE.md` and the runtime test suite — undocumented
  attributes are drift the next author can't discover.

---

## See also

- [`docs/ui-new-components.md`](ui-new-components.md) — full component catalog.
- [`docs/widgets.md`](widgets.md) — widget framework (Modal, Drawer, Popover mounts).
- [`core-ui/ARCHITECTURE.md`](../core-ui/ARCHITECTURE.md) — runtime contract + attribute reference.
- [`docs/ui-getting-started.md`](ui-getting-started.md) — first-time UI setup.
