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

The framework's first hard rule is that an in-page state change is
never a route: no `<a href="?page=2">` that the router treats as a
navigation. A headless component that turns a page, sorts a column or
applies a filter takes an `Island` and renders the framework's own RPC
contract (`data-fui-rpc`, `data-fui-rpc-method`, `data-fui-rpc-signal`,
`data-fui-push-state`) on the same element that keeps its href or
action for a reader with no script. First paint is the page; hydration
makes it the island; the URL is written by the runtime.

```go
headless.Pagination(headless.PaginationProps{
    Page: 2, Pages: 9, HrefPattern: "/apps?page=%d",
    Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"},
}, classes)
```

Where the change would otherwise be a route the Island is required:
`Pagination`, `ToolbarSearch`, a `Tag` with a dismiss and an `Alert`
with a dismiss panic without one, so the link-only render cannot be
built. On a `Form` it is optional, for a page that is the form — and
an island form answers a failed validation with **200 and the
region's HTML**: the errors are the answer, the runtime swaps them
into the signal-bound region, and the arrival pass focuses the
summary. A non-2xx lands in the signal as `{ok:false, status, text}`
and renders nothing, so it is for transport and server errors, never
for validation.

A `Table`'s Island is optional too, for the opposite reason a Form's
is: the URL is the truth for a list. A list screen renders plain sort
anchors that work with no script and that the client router
intercepts when script is present — the sort changes the address bar
because the address bar is where a list's state lives — and the
Island is for an embedded table whose sort must not change the URL:
the same anchors then carry the RPC contract beside their hrefs,
exactly as the pager's do. `Pagination` still requires one; that
stays as it is until the pager moves onto a `Table`'s footer slot.

Every href a component writes goes through the framework's anchor
policy, `urlsafe.CleanAnchor`: a `Button` whose href is rejected
renders the disabled-link posture; a form action, a dismiss href and
a pager pattern that are rejected are refused at render; and a
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
module.

**See it live:** the product site ships a landing page under each of two
boot-registered themes —
`/examples/headless/default/landing` and
`/examples/headless/dense/landing` — with every variant and size, the
same palette under two option sets, an A → B → A nest, explicit scheme
controls, bare headless beside styled ui, a newsletter form whose
no-script POST is answered with a 303 back to the page (the outcome
rides the route's query and the answer renders inside the site's
chrome; the submitted address never travels in the URL), and a cold
`LoadAuto` insertion. The browser proofs live in
`examples/site/e2e_headless_landing_test.go`.

The form family's first dashboard lives under the same routes —
`/examples/headless/default/dashboard` and
`/examples/headless/dense/dashboard` — one settings form that submits
both ways (island RPC with the runtime, plain POST without),
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
ValidationSummary, Card, Stack, Cluster, Grid, Container, Section,
Divider, Spacer, Spinner, Skeleton, Alert, SystemBanner, Badge, Tag,
Toolbar, ToolbarGroup, ToolbarSpacer, ToolbarSearch, Pagination, Table, Steps,
Timeline, OptimisticAction and ToggleAction.

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
- **table** restores what an island sort destroys when a valid answer
  arrives: the click on a `data-hui-table-sort` anchor accepts only a
  signal-bound table, then records the column key and replacement region;
  focus returns to the same column's anchor in that region (the
  `data-hui-table-scroll` region when the answer dropped the column).
  The sentence the server rendered into
  `data-hui-table-announcement` is copied, clear then frame, into the
  `data-hui-table-status` span. A failed answer leaves focus and status
  where they were. A plain table's status and announcement render for
  the pager's later use; this module fills neither.

Two attributes are the module's own, written by it and rendered by no
component: `data-hui-when-off` and `data-hui-drop-over`.

Arming is the kernel's. The module registers a scanner and the kernel
calls it on every inserted subtree and over the document after a
client navigation; a host adds no observer, and the module adds none
of its own. `framework/ui` is the styled layer on top of this package:
its components render through these structures dressed with their own
class maps (Button first), and a bare headless render stays unstyled.

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
- **Rendering a pager or a search form without an Island.** That is a
  route for an in-page state change, and the component refuses it at
  render time with the rule's reason.
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
