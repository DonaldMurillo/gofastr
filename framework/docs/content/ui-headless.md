# Headless components

`framework/headless` is the structure half of a design system. A
component there renders tags, roles, labelling relationships, state
attributes and the hooks a runtime binds to, and nothing else: no
classes at a nil Classes, no CSS, no script. A class map dresses it; a runtime
module binds it; a harness proves every registered component against
one contract.

It exists because structure and styling have different lifetimes.
Whether a field's error is tied to its input by `aria-describedby` does
not change when the palette does, and it is testable without rendering
a pixel. Once the two are separate, a class map can be redrawn or replaced
without putting a single accessibility guarantee back at risk.

This is the same SSR-first, incrementally hydrated model as the rest of
the framework ([core-ui/ARCHITECTURE.md](../../../core-ui/ARCHITECTURE.md)):
first paint is the full markup, behaviour arms by hook on arrival, and
an in-page state change is an island RPC, never a route.

**Import:** `github.com/DonaldMurillo/gofastr/framework/headless`

---

## The vocabulary

A component is a pure function from its props and a `Classes` value to HTML.
Seven things are named in its contract, and the harness checks each in
both directions.

- A **part** (`headless.Part`) names an element the component draws.
  The class map styles those and only those; a part declared and never
  rendered fails a test, and so does a rendered part nobody declared.
- A **class map** (`headless.Classes`) is a flat map from part to class. Nil is
  valid and renders unstyled. Variants are looked up as
  `<part>--<variant>`, so the headless layer passes a variant name
  through without knowing what any of them mean.
- A **hook** is a `data-hui-*` attribute a runtime binds to. A component
  declares the hooks it publishes in its `Spec`; a runtime that binds
  to an undeclared hook is bound to nothing.
What a caller may set on a component's parts travels as one `Parts`
value on the props, with three maps keyed by part:

- **Slots** replace the content of the parts a component lists as
  fillable. Most list none: a slot exists only where the component
  composes something no prop can express.
- **Attrs** add attributes to a part, through a sanitiser that
  refuses `id`, `style`, every `data-hui-*` hook, every `data-fui-*`
  key and the runtime's privileged unprefixed keys (`data-behavior`,
  `data-island`, `data-action` and their family), stores names folded
  the way the browser reads them, refuses one name under two
  spellings, and never beats an attribute the component owns.
  `ExtraAttrs` goes through the same refusal.
- **Binds** keep a part in step with a client signal (text, html or one
  attribute), refusing reserved signal names and attributes outside the
  runtime's own allow-list. Two ownership rules on top: a `text` or
  `html` Bind replaces a part's content, so it is allowed only on a
  part the component lists as fillable — the same set a Slot may fill —
  and an `attr` Bind may not name an attribute the runtime rewrites as
  state moves (`aria-pressed`, `aria-busy`, `aria-live`, `aria-invalid`,
  `aria-expanded`, `aria-current`, `hidden`, `disabled`, `data-state`,
  and every `data-hui-*` hook). Both refuse at render, with the
  reason.

**Strings** are the strings a component says, a prop of its own: one
typed struct, nil for English, for a layer above to resolve once per
request from the framework's `i18nui` keys. That layer is
`ui.StringsFor(ctx)` (`framework/ui/strings.go`): one table maps every
field to its `i18nui` key, and the call fills the struct through the
request's translator — pass `Strings: ui.StringsFor(r.Context())` and
the component says its words in the reader's locale. No translator on
the ctx (or a per-key catalog miss) yields the English defaults, the
same words nil yields. A field left empty falls back to its English
default at runtime — a partial translation is safe, and the one that
misses shows as a stray English word on the translated page, not as a
compile error; the probe golden (`spec_golden_strings.txt`) is what
catches a component saying a word no `Strings` field carries. The list
of parts a component draws is its `Spec.Anatomy`, the word Ark UI,
Chakra and Radix use for the same list.

Two more are not per part. A `Button` takes an **Action**, the
framework's request contract, on itself; `ExtraAttrs` cannot carry one.
A component that changes in-page state takes an **Island**: the
endpoint that renders the region again and the signal the region is
bound to.

```go
classes := headless.Classes{
    headless.PartRoot:  "field",
    headless.PartLabel: "field__label",
    headless.PartHint:  "field__hint",
    headless.PartError: "field__error",
}

headless.Field(headless.FieldProps{
    Label: "Port", For: "port", Hint: "1024 to 65535", Required: true,
}, classes, func(c headless.FieldControl) render.HTML {
    return headless.Input(headless.InputProps{
        Name: "port", ID: c.ID, Required: c.Required,
        DescribedBy: c.DescribedBy, Invalid: c.Invalid,
    }, inputClasses)
})
```

The control is built from the wiring the field passes down, so its id,
its `aria-describedby` and its invalid state cannot disagree with the
label, the hint and the error beside it.

## Islands: hard rule 1, at render time

The framework's first hard rule draws one line: state that is the
document's renders as the URL, and state that is not the document's
requires an Island. A list's page, sort and URL-owned filters are the
document's state — a list screen's pager and sort headers are plain
anchors (`?p=2`, `?sort=name`) that work with no script and that the
client router intercepts when script is present, because changing the
address bar IS changing the list's state; an Island enters only for a
table or pager embedded in a region whose page turn must not
navigate the document, and then the SAME anchors carry the framework's
RPC contract (`data-fui-rpc`, `data-fui-rpc-method`, `data-fui-rpc-signal`,
`data-fui-push-state`) beside their hrefs. First paint is the page;
hydration makes it the island; the region is swapped in place and the
address bar still follows, written by the runtime through
`pushState` rather than by a navigation.
Everything else — expand, reveal, a dismiss, a filter applied in
place, a search — is in-page state: `ToolbarSearch`, a `Tag` with a
dismiss and an `Alert` with a dismiss refuse to render without an
Island, so the link-only render cannot be built. The carve-out is the
list's alone, and it cannot be stretched: an accordion or a reveal is
not list state, and a plain-link accordion is exactly as wrong under
this rule as it was before the carve-out was written.

```go
headless.Pagination(headless.PaginationProps{
    Page: 2, Pages: 9, Path: "/apps",
    Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"},
}, classes)
```

On a `Form` the Island is optional too, for a page that is the
form — and an island form answers a failed validation with
**200 and the region's HTML**: the errors are the answer, the runtime
swaps them into the signal-bound region, and the arrival pass focuses
the summary. A non-2xx lands in the signal as `{ok:false, status, text}`
and renders nothing, so it is for transport and server errors, never
for validation.

What a reader is told after an island sort is composed on the server,
never in script. `TableProps.Summary` (and `ui.DataTableConfig.Summary`,
which passes it through) is the caller's sentence about the result
window, "Showing 8 of 10", because only the caller knows the total.
The primitive prefixes the sort when `SortBy` names a column, from
three `Strings` fields: `TableSortedBy` ("Sorted by {column},
{direction}", the column named by its `Header` or its `Key`),
`SortAscending` ("ascending") and `SortDescending` ("descending"),
bridged by `ui.StringsFor` to `i18nui.KeyTableSortedBy`,
`KeyTableDirAscending` and `KeyTableDirDescending`. The result lands in
`data-hui-table-announcement` on the root, and the behaviour module
copies it into the table's status after the swap; a table with no sort
and no `Summary` renders no announcement at all.

Every href a component writes goes through the framework's anchor
policy, `urlsafe.CleanAnchor`: a `Button` whose href is rejected
renders the disabled-link posture; a form action, a dismiss href and
a pager's Path that are rejected are refused at render; and a
summary's field link the policy refuses falls back to plain text.

The endpoint keeps the href's query, merged pair by pair onto its own,
so the page and the island answer the same question. State keys (page,
sort, filter) belong in the href and nowhere else: when the endpoint
carries a key the href also carries, both values survive, the endpoint's
first, and a handler that reads `Query().Get` sees the endpoint's stale
one. `data-fui-push-state` is rendered only for a GET
with a href to write; a mutation's URL is the server's to set through
`X-Gofastr-Push-State`.

## Headless via framework/ui

Two callers, two surfaces, one package. Pick your scenario by what you
want to own:

- **Bare headless.** You render `headless.Button(props, nil)` (or with a
  `Classes` of your own) and style it with your own stylesheet. You see
  parts, hooks, `Parts{Attrs, Slots, Binds}`, `Strings`, `Island`,
  `Action`, `Classes`. You never see a theme. The `fui-` class prefix is
  reserved for framework/ui: writing `fui-button` on your own markup gets
  the framework's styling whenever that sheet is on the page.
- **framework/ui.** You render `ui.Button(ui.ButtonConfig{...})` and
  customise through the theme: tokens for palette and scale, and the
  typed component options (`theme.Overrides.Components`) for how the
  component family draws — density, button treatment, button radius. You
  never see `Classes`, parts, or a class name.

`Classes` is internal to the styled layer: `ui.Button` dresses the
headless structure with this package's own `fui-button` class map,
and the class names are the same under every theme — a theme never
picks classes, it declares option variables that the component's
stylesheet consumes (see [theming](theming.md) → "Component
options"). Discovery and styling stay separate there too: the
`data-fui-comp="ui-button"` marker is what fetches the sheet; the
classes are what the sheet matches. A bare headless button beside a
styled one on the same page stays unstyled — that pair is one of the
fixtures below.

The plain-markup form family renders through headless the same way:
`ui.Form`, `ui.FormField` (whose `Input` is a builder receiving the
field's wiring — the id, the described-by chain, the invalid state,
the required flag), `ui.FormSection`, the typed fields, `ui.Select`
(which carries its own `data-fui-comp="ui-select"` marker on the
control beside the field's marker, so both sheets load wherever it
renders), `ui.ValidationSummary` and `ui.InputGroup`. `ui.Control` is
the styled native input for the types the typed fields do not name,
built inside a FormField builder from the wiring the field hands it.
`ui.Form` routes its island wiring through `headless.FormProps.Request`,
the typed request seam (what Button's `Action` is to a click): the
`data-fui-rpc` contract, a method that may differ from the native
one, the success effects, and the generator's `data-action-mount`
hook, each checked for what it deserves.

The choice family and the affix shells render through headless too:
`ui.Checkbox`, `ui.Radio` and `ui.Switch` dress `headless.Choice` and
`headless.Switch` (one inline run — the label wraps the control —
which keeps its own structure and ignores the field sheet's columns
variables); `ui.RadioGroup` and `ui.CheckboxGroup` hand their rendered
leaves to `headless.Group` with the group's own message paragraph, a
group's error belonging on the group and never on each leaf;
`ui.PasswordInput` is `headless.Password` (the reveal button's
`data-hui-reveal` is bound by the headless module, and its words come
from `ui.StringsFor`); `ui.ColorField` is `headless.Color` (the
swatch out of the tab order, the hex text the source of truth, and
the headless module's colour sync keeping the two one value for
every value the picker can show — the short `#abc` form included,
expanded to `#aabbcc` for the swatch and never an error; a value the
picker cannot show stays verbatim in the text while the swatch falls
back to black and the shell is marked `data-invalid` — the theme
editor's own sync was deleted for it).

The bespoke-behaviour family renders through headless too:
`ui.FileUpload` is `headless.FileUpload` (the drop zone a label for
the real input, the chosen names a role="list" the module fills, the
pick announced by a role="status" span saying the `FileSelected` /
`FilesSelected` sentences the strings bridge resolved); `ui.FileDropzone`
carries the same `data-hui-drop` hooks around its hero surface and
keeps only the thumbnail strip for itself — a styling concern with no
headless counterpart, bound by framework/ui's own `filedropzone`
module; `ui.ConditionalField` is `headless.ConditionalField`, a region
rendered VISIBLE and hidden by the module until the watched field
matches (a field only a script can reveal is a field a scriptless
reader never reaches — the module also disables what it hides, so
nothing hidden submits); `ui.TextArea` is `headless.Field` +
`headless.Textarea` the way `ui.Select` is, with `Autogrow` reaching
the control through the prop that survives the data-fui-* refusal.
`ui.SearchInput` has no headless counterpart (the icon, the clear
button and the role="search" wrap are its own) and keeps its own
module; the `searchinput` and `shortcut` runtime modules stay with it
until the Batch 3 Combobox decision — retention, not a gap.

The stateful family renders through this package the same way, and
its Island rules are the same rule: a `TagInput` owns its chips
client-side and needs no Island; a `Repeater` whose add/remove
re-renders the region needs `Action` plus a complete `Island`, and
the same buttons stay named submit controls so the surrounding form
is the no-script path; a `Toast` with a `DismissHref` needs a
complete `Island` exactly as an `Alert`'s dismiss does; a
`StepWizard`'s Island is optional and, when set, the form keeps its
plain POST shape and the module focuses the new step's heading or
the error summary after the swap. A `ToastStack` is mounted once by
the layout: it carries the framework's `data-fui-toast-stack` name
beside its own `data-hui-toast-stack` (the kernel's response-header
toast path resolves the first, the component module the second), the
SSR rows inside it are visible with no script, and it is the live
region toasts announce by arriving in.

The action buttons (`OptimisticAction`, `ToggleAction`, `Button`
with an `Action`) keep the button contract: with no script they do
nothing on their own. A caller that needs the no-script submit puts
them inside a form whose action and method say where the request
goes — the component never wraps itself in a generated form, because
nested forms are invalid HTML and every caller that already composes
a form would break.

**See it live:** the product site ships a landing page under each of five
boot-registered themes, `/examples/headless/{theme}/landing` for
`default`, `dense`, `soft`, `editorial` and `contrast`, with a theme
switcher, every variant and size, the
same palette under two option sets, an A → B → A nest, explicit scheme
controls, bare headless beside styled ui, a newsletter form whose
no-script POST is answered with a 303 back to the page (the outcome
rides the route's query and the answer renders inside the site's
chrome; the submitted address never travels in the URL), and a cold
`LoadAuto` insertion. The browser proofs live in
`examples/site/e2e_headless_landing_test.go`.

A basic dashboard lives under the same themes,
`/examples/headless/{theme}/dashboard`: a `RecordSummary` with a
`MetricBand`, a `LineChart` with its values as text, an invoice
`DataTable` whose sort works as an island swap and as a plain link,
and last a settings form that submits both ways (island RPC with the runtime, plain POST without),
validates on the server, moves focus to the summary on a failed
submit, carries a password field and an upload, and nests conditional
regions two deep. Its browser proofs live in
`examples/site/e2e_headless_dashboard_test.go`.

## The spec and the harness

Every component registers a `Spec` beside its code: its name, the
parts it draws, the parts a slot may fill, the hooks it publishes, and
its cases, each with a `Why`. One fixture drives everything that must
hold for every component:

- every case says why it exists and renders something;
- at the nil Classes no case renders a `class`, an empty `aria-*`, a
  `style`, a `<button>` with no type, or a duplicate id;
- rendering is deterministic;
- every declared part is drawn and every drawn part is declared;
- every declared hook survives rendering;
- every `aria-*` reference resolves inside the fixture, and every
  control has a name;
- a filled slot and a hostile part attribute cannot break the contract;
- every exported component has a spec.

Two goldens pin the corpus: `testdata/spec_golden.txt` at the English
strings and `spec_golden_strings.txt` with every string replaced by its
own probe token, which proves that every string on the page came
through `Strings`. `strings_test.go` also walks the source and refuses an
English phrase in the sinks it knows: text and HTML literals, format
strings, defaults, and the attribute values a reader is told.

```sh
go test ./framework/headless/                    # every gate
GOFASTR_UPDATE_GOLDEN=1 go test ./framework/headless/ # after an intended markup change
```

Read the golden diff. Every line of it is a change to what assistive
technology will be told.

## What ships in this package

Button, Field, FieldRow, ConditionalField, Input, Textarea, Select,
Password, Color, Choice, Switch, Group, Form, InputGroup, FileUpload,
Fieldset, ValidationSummary, Card, Stack, Cluster, Grid, Container,
Section, Divider, Spacer, Spinner, Skeleton, Alert, SystemBanner,
Badge, Tag, Toolbar, ToolbarGroup, ToolbarSpacer, ToolbarSearch,
Pagination, Table, Steps, Timeline, PageHeader, EmptyState, StatCard,
DetailList, OptimisticAction and ToggleAction, plus the stateful
family: Counter, NumberInput, Slider, RangeSlider, Rating, TagInput,
Repeater, Toast, ToastStack, NotificationBell, StepWizard and
BackToTop. The navigation-behaviour members: Rail, TableOfContents,
Disclosure, Menu, Combobox, Tabs, Carousel, PaneHost, Sidebar and
SidebarRegion, and the two pure-render trees JSONTree and Gallery.

The parts the navigation members draw beyond the shared vocabulary:

- **Carousel**: `carousel-stage` (the overflow scroller the slides sit
  in), `carousel-track`, `carousel-slide`, `carousel-dot`,
  `carousel-prev`, `carousel-next`.
- **Combobox**: `combobox-form` (the no-script GET form),
  `combobox-input`, `combobox-listbox`, `combobox-option`,
  `combobox-status`.
- **JSONTree**: under each value's shared parts, one part per JSON
  scalar kind so a class map can colour a string without colouring a
  number: `json-colon`, `json-type`, `json-count`, `json-str`,
  `json-num`, `json-bool`, `json-null`, `json-empty`.
- **Sidebar** draws the shell (`sidebar-drawer`, `sidebar-inline`,
  `sidebar-toggle`) and, with `SidebarRegion`, the slot-in version of
  the same content with no shell hooks; items carry `sidebar-item`
  (with a `--sub` variant), `sidebar-nav`, `sidebar-prepend`,
  `sidebar-group`, `sidebar-group-toggle` (the button dialect),
  `sidebar-group-list`, and `icon` with a `--fallback` variant (the
  label's initial, for rail states). `Gallery` takes a
  `GalleryLightbox` wiring: the typed prop that renders the widget
  runtime's `data-fui-open` / `data-fui-deeplink` /
  `data-fui-lightbox-group` trigger family on every item anchor.

## The behaviour module

The runtime module that binds the `data-hui-*` hooks ships in this
package, registered by `behavior.go` through the same seam a
stylesheet uses (`registry.RegisterBehavior`). The host serves it as
the module `headless` at `/__gofastr/runtime/headless.js`, and the
kernel loads it when one of its markers is on the page. The markers
are `[data-hui-reveal]`, `[data-hui-color]`, `[data-hui-when]`,
`[data-hui-form-errors]`, `[data-hui-action]`, `[data-hui-drop]`,
`[data-hui-system]` and `[data-hui-table]`: one per behaviour, the
root hook of each.

What it does, one line per behaviour:

- **reveal** retypes the password input, swaps the button's text and
  accessible name from the `data-hui-show-*` and `data-hui-hide-*`
  attributes, and keeps focus and the caret where the reader left
  them.
- **color** keeps the swatch and the hex text one value in both
  directions, and marks the shell `data-invalid` when the text holds a
  non-empty value that is neither `#rgb` nor `#rrggbb`.
- **when** hides a `data-hui-when` region whose watched field does not
  carry `data-hui-when-value`, disabling its controls under the
  runtime-owned `data-hui-when-off` mark so only those re-enable.
  The watched control is looked up in the region's own form first,
  and only then in the document — preferring controls no form owns,
  else the first in document order — so two forms with a same-named
  control cannot decide a region that belongs to neither. Regions
  nest: a region is shown only when its own condition holds and no
  enclosing `data-hui-when` region is hidden, and the controls inside
  any hidden region stay disabled no matter what an inner region's
  own condition says.
- **form-errors** moves focus to the summary inside
  `data-hui-form-errors`, once per form element — and the once-mark
  is spent only when a summary was found, so a form whose `Errors`
  is not a summary yet still focuses the summary a later render
  brings.
- **action** binds `[data-hui-action]` buttons through the kernel's
  `action` primitive (`window.__gofastr.action.bind`), reading the
  endpoint, method, group and untoggle hooks and the two label parts;
  the module's registration `Requires("action")`, so the primitive is
  there first. A rolled-back mutation — on the optimistic button or a
  failed toggle commit — writes the root's `data-hui-action-failed`
  sentence into the `data-hui-action-status` span.
- **drop** lists the chosen files and says the sentence, both built
  from the words the root carries (`data-hui-drop-one` and
  `data-hui-drop-many`, from `Strings.FileSelected` and
  `Strings.FilesSelected`), and takes a real drop on the zone with the
  runtime-owned `data-hui-drop-over` state. The input is resolved
  from `data-hui-drop-input` on every event, so a swap that replaced
  it while the zone survived still lands the drop on the control the
  form submits.
- **system** keeps a dismissed banner hidden for the session, and
  shows the offline one (`data-hui-system-offline`) when the framework
  reports the connection lost with a retry scheduled — reading, on
  arm, the state `sse.js` mirrors onto `window.__gofastr.sseStatus`,
  so a banner that arrives during an outage shows at once. The
  dismissed set never applies to it: losing the connection again must
  show it again, which is also why the offline banner carries no
  dismiss.
- **table** restores what an island sort or page turn destroys when a
  valid answer arrives: the click on a `data-hui-table-sort` anchor or
  the pager's `data-hui-page` anchor accepts only a signal-bound
  table, then records the control's identity (the column key, the page
  number) and replacement region; focus returns to the same control's
  anchor in that region (the current page's `aria-current` anchor when
  the answer has fewer pages, the `data-hui-table-scroll` region when
  the answer dropped the control). The sentence the server rendered
  into `data-hui-table-announcement` is copied, clear then frame, into
  the `data-hui-table-status` span. A failed answer leaves focus and
  status where they were. A plain table's status and announcement
  render for the pager's later use; this module fills neither.

Three more modules of this package ship beside it, each registered the
same way and each owning one family of the stateful controls:

- **headless-controls** (`[data-hui-counter-animate]`,
  `[data-hui-number-input-decrement]`, `[data-hui-slider-output]`,
  `[data-hui-range-slider]`): steps the number input inside the bounds
  its own attributes declare and reports a real input event; mirrors
  the slider's thumb into its output; cross-clamps the range pair and
  re-formats its sentence through the shape the output carries; and
  animates a counter from its recorded start to the SSR value, never
  touching the number the signals kernel owns. It replaced the retired
  `numberinput`, `slider`, `rangeslider` and `animatedcounter`
  runtime modules (the unanimated counter needs no module at all —
  the signals kernel is its whole increment path).
- **headless-collections** (`[data-hui-tag-input]`,
  `[data-hui-repeater]`): commits the tag draft on Enter, comma, the
  add control or blur; refuses a duplicate; removes on the chip's ×
  or Backspace-on-empty; returns focus to the field; announces
  through the `data-hui-tag-input-added`/`-removed` sentences; and
  after a repeater's island swap puts focus back on the row's control
  or the add control and says the server's status. It replaced the
  retired `taginput` and `formrepeater` runtime modules, and the IME
  guard (an Enter that confirms a composition candidate commits
  nothing) moved with it.
- **headless-wizard** (`[data-hui-step-wizard]`): owns only the
  island path — it records the step the reader left, and after the
  swap focuses the failed submit's summary or the new step's heading
  and says the step-of sentence when the step moved. The plain POST
  wizard needs none of it.

Two attributes are the `headless` module's own, written by it and
rendered by no component: `data-hui-when-off` and `data-hui-drop-over`.
One is `headless-navigation`'s: `data-hui-back-to-top-visible`.

Two more modules of this package own the feedback and page-control
families:

- **headless-feedback** (`[data-hui-copy]`, `[data-hui-toast-stack]`
  and `[data-fui-toast-stack]`, `[data-hui-notification-bell]`,
  `[data-hui-network-retry]`): the copy control (no clipboard mutation
  without script — the words travel on the wrapper from `Strings`),
  the toast stack runtime (`NS.toast`, `_initToasts`, `_dismissToast`,
  `_toastTimers`, `_toastSeq` — the API the kernel's `X-Gofastr-Toast`
  dispatch and `ToastSlot` speak; the kernel's `loadModule` target is
  this module now), the bell's spoken count re-said when a signal
  changes it, and the offline banner's retry link. It replaced the
  retired `copy`, `toasts` and `networkretrybanner` runtime modules.
- **headless-navigation** (`[data-hui-back-to-top]`,
  `[data-hui-theme-toggle]`): the back-to-top link (one sentinel for
  the document, the visibility mark, the focus return) and the theme
  group (persisted through the same storage key the bootstrap reads,
  so the scheme never flashes). It replaced the retired `backtotop`
  and `themeswitch` runtime modules.

The navigation-behaviour families ship as their own modules, each
registered the same way:

- **headless-rail** (`[data-hui-rail]`): the anchored rail's scroll
  spy — one intersection observer for every rail on the page, marking
  the link whose section owns the viewport. It replaced the retired
  `scrollspy` runtime module.
- **headless-toc** (`[data-hui-toc]`): the table of contents' current
  heading, through the same rail watch. It replaced the retired `toc`
  runtime module.
- **headless-disclosure** (`[data-hui-disclosure]`): the exclusive
  accordion group and the trap-and-inert Escape path, with optional
  session restore through a storage key. It replaced the retired
  `disclosure` runtime module.
- **headless-menu** (`[data-hui-menu]`): the menu's open/close, arrow
  traversal, typeahead and focus return, on top of the disclosure
  primitive. It replaced the retired `menu` runtime module.
- **headless-combobox** (`[data-hui-combobox]`): the listbox filter,
  the count announcement and the loading word. It replaced the retired
  `combobox` runtime module.
- **headless-navigation** also carries the keyboard-shortcut family
  (`data-hui-shortcut-*`), and **headless-tabs**
  (`[data-hui-tabs]`) owns the roving tabindex and the vacate stash;
  **headless-carousel** (`[data-hui-carousel]`) owns the fragment
  controls, the status sentence and one rotation timer per root,
  paused on hidden documents, reduced motion, hover and focus. They
  replaced the retired `tabs` and `carousel` runtime modules.
- **headless-panehost** (`[data-hui-panehost]`): the pane lifecycle —
  open/close/swap through the `data-hui-pane-*` controls, focus
  handoff and restore, the responsive drawer mode and the optional
  query deep link; a crafted slot value is refused before any
  selector, so the click is a no-op. It replaced the retired
  `panehost` runtime module.
- **headless-sidebar** (`[data-hui-sidebar]`): the collapse state —
  persisted only under a namespaced, component-encoded storage key the
  root names; a root with no key is the server's and the module never
  writes — the custom collapse/expand labels, and the button-dialect
  group toggle. It replaced the retired `sidebar` runtime module.

Two of this package's members ship no module at all: `JSONTree` (the
browser's own `<details>` is the whole behaviour, and its object keys
render sorted so the bytes are deterministic) and `Gallery` (every
item is a real link; a lightbox click travels the widget runtime's
open contract, which the adapter carries).

Arming is the kernel's. Each module registers a scanner and the kernel
calls it on every inserted subtree and over the document after a
client navigation; a host adds no observer, and no module adds one of
its own. `framework/ui` is the styled layer on top of this package:
its components render through these structures dressed with their own
class maps, and a bare headless render stays unstyled.

## Common mistakes

- **Arming the hooks by hand.** A MutationObserver or a
  `gofastr:navigate` listener in a host, or a second module binding
  the same hooks, arms everything twice: the kernel already hands
  every inserted subtree and every post-navigation document to the
  module's scanner.
- **Saying a sentence in the module.** Every string the module writes
  travels as a `data-hui-*` attribute the component rendered from its
  `Strings`, so a translated page announces in its own language; the
  gate in `behavior_test.go` refuses an English literal the module
  writes itself.
- **Finding an element from script by its class.** The runtime binds to
  `data-hui-*` hooks only. A class map may rename every class, and a class
  used as a hook is the one thing it cannot rename.
- **Rendering a search form without an Island.** That is a route for
  an in-page state change, and the component refuses it at render
  time with the rule's reason. A pager on a list screen is the
  allowed plain posture: its state is the URL's, and the Island is
  for the pager embedded in a region.
- **Smuggling a request through `ExtraAttrs` or an Override.** Both
  drop every `data-fui-*` key. A request is `ButtonProps.Action`, a
  signal is a `Bind`, a region's refresh is an `Island`.
- **One attribute under two spellings.** `NAME` and `name` are one
  attribute to the browser, which keeps the first it reads. Both ways in
  store names folded, so the component's own spelling wins, and a key
  given twice is refused at render.
- **Building the hint's id by hand.** `Field` passes a `FieldControl`
  with the id, the `aria-describedby` and the invalid state already
  agreed; a control built from anything else drifts.
- **Setting an attribute whose presence is the value through `Attrs`.**
  `Attrs` drops empty values on purpose; `hidden`, `popover`, `open`
  and every `data-hui-*` hook go through `Mark`, and boolean HTML
  attributes through `Flag`.
- **Adding an English string to a component.** Give it a `Strings`
  field with a doc comment saying its shape. The source walk in
  `strings_test.go` fails on a phrase said outside `Strings`.
- **Regenerating a golden to make a test pass.** Regenerate only after
  every other test in the package is green, then read the diff.
