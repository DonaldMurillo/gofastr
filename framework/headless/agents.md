# framework/headless — structure without styling

The headless layer renders a component's tags, roles, labelling
relationships, state attributes and runtime hooks, and nothing else: no
classes at a nil `Classes`, no CSS, no script. A `Classes` value (a flat
map from part to class) dresses it; the behaviour module (behavior.go,
served as the runtime module "headless") binds its `data-hui-*` hooks.
The action buttons carry their own hooks — `data-hui-action-endpoint`,
`-method`, `-group`, `-untoggle`, and the `data-hui-action-idle` /
`-done` label parts — and bind through the kernel's `action` primitive,
which the module's registration `Requires`; nothing here borrows a
`data-fui-comp` marker, so a headless button can never pull
`framework/ui`'s stylesheet. The harness
proves every registered component against the same contract, so a class
map can be replaced without a single accessibility guarantee moving.
`framework/ui`'s Button family renders through this package with the
internal `fui-button` class map (its stylesheet and marker still live
in the styled layer); the plain-markup form family does too (Form,
FormField whose Input is a builder receiving the field's wiring, the
typed fields, Select, ValidationSummary, InputGroup, and ui.Control
for the types the typed fields do not name), with the `fui-form*` /
`fui-field*` / `fui-select*` / `fui-input*` class maps. A Field
renders its hint AND its error when both are set, the error first,
with both ids in the control's described-by: the hint is the rule the
value must obey and the error is the violation, so dropping the rule
exactly when it was broken is dropping it when it is needed most.
`ReserveError` keeps an empty, wired error node rendered for a script
that fills it without re-rendering; the node carries no hook of its
own — it is found by the control's id with `-error` appended, the same
id that rides the control's aria-describedby.
The other families adopt their components' headless counterparts in
their own changes; the lightbox viewer is the first overlay anatomy
here (`LightboxViewer`, the body a zoom modal mounts): its
`data-hui-lightbox*` hooks are for a host writing its own viewer
module and render exactly when `LightboxWiring` is zero; a set Wiring
renders the `data-fui-lightbox*` family for framework/ui's lightbox
behaviour instead — one vocabulary per render, because a host module
binding the hooks beside the framework's would double-bind the
gallery it steps — and this package's module reads neither.
**Use this when** the prompt mentions: headless, unstyled, reskin, a
second design system, parts, anatomy, slots, attrs, binds, strings, translated
component strings, an island on a table or pager, or a component whose
accessibility must be pinned by a golden.

**Import:** `github.com/DonaldMurillo/gofastr/framework/headless`

## Shape

```go
// A component: props and a Classes value in, HTML out. Nil = no classes.
headless.Button(headless.ButtonProps{Label: "Save", Type: "submit"}, classes)

// Classes: part → class. The only coupling between the two layers.
classes := headless.Classes{headless.PartRoot: "btn", headless.PartIcon: "btn__icon"}

// Parts: how a page reaches inside without forking, keyed by part.
headless.Card(headless.CardProps{Title: "Apps", Parts: headless.Parts{
    Attrs: headless.PartAttrs{headless.PartFooter: {"data-testid": "f"}},
    Slots: headless.Slots{headless.PartCardHeader: header},
    Binds: headless.Binds{headless.PartTitle: {Signal: "count"}},
}, Strings: strings /* nil means English; ui.StringsFor(ctx) resolves it */}, classes, body)

// An in-page state change is an island, never a route (hard rule 1) —
// and a list's own state (page, sort) lives in the URL, so a list
// screen's pager and sort anchors are plain navigations the client
// router intercepts, and the Island is for the embedded case.
headless.Pagination(headless.PaginationProps{
    Page: 2, Pages: 9, Path: "/apps",
    Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"},
}, classes)

Every component registers a `Spec`: its name, the parts it draws, the
parts a slot may fill, the hooks it publishes, and the cases worth
rendering with a reason each. One fixture drives the nil-Classes sweep,
the parts tests and the two goldens (`testdata/spec_golden.txt` at the
English strings, `spec_golden_strings.txt` at probe strings). The `Kit`
the fixtures are handed is harness infrastructure, not caller surface.

## Don't reinvent

- **A link that changes in-page state.** `ToolbarSearch`, a `Tag`
  with a dismiss and an `Alert` with a dismiss require an `Island`
  and refuse to render without one; the same element keeps its href
  for no-script. `Table` and `Pagination` take the list posture
  instead: the URL is the truth for a list, so a list screen's sort
  and page anchors are plain navigations the client router
  intercepts, and the `Island` is for an embedded table or pager
  whose page turn must not navigate the document — its anchors then
  carry the contract beside their hrefs (the runtime swaps the region
  and writes the URL through `pushState`), and the module restores
  focus and
  announces the swap through the `data-hui-table*` and `data-hui-page`
  hooks the components render. Every href goes through the
  framework's anchor policy (`urlsafe.CleanAnchor`).
- **A request through `ExtraAttrs`.** `Safe` drops every `data-fui-*`
  key. A request is `ButtonProps.Action`; a signal is a `Bind`; a
  region's refresh is an `Island`. Action also admits the wiring
  keys a page can put on any clickable — widget and pane open/close,
  toast, push-state, deeplink, prefetch, intercept-close — each
  checked for what it deserves; on an anchor only the four that say
  where a click goes may ride.
- **One attribute under two spellings.** `Safe` and the part-attrs
  sanitiser store names folded, as the browser reads them, so a
  caller's `NAME` cannot land beside the component's `name`; a key
  given twice is refused.
- **An English string in a component.** Strings live on `Strings`, one
  typed field each, so a translation is a value the compiler checks
  the shape of; a field left empty falls back to its English default
  at runtime — the miss is a stray English word on a French page, not
  a compile error — and `strings_test.go` refuses English written
  outside `Strings`. The layer above that resolves them per request is
  `ui.StringsFor(ctx)` (framework/ui), one field-to-key table over
  `i18nui`; a field added here fails that package's gate until the
  bridge maps it.
- **A golden update without reading it.** `GOFASTR_UPDATE_GOLDEN=1 go test`
  regenerates; every changed line is a change to what assistive
  technology is told.
- **Arming the hooks by hand.** The behaviour module is armed by the
  kernel, which hands it every inserted subtree; a MutationObserver or
  a navigate listener in a host, or a second module binding the same
  hooks, arms everything twice.
- **Saying a sentence in the behaviour module.** Strings travel as
  `data-hui-*` attributes from `Strings`, so the module itself writes no
  sentence a translated page would say in English.

Full contract: `gofastr docs ui-headless`.
