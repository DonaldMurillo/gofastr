# Fixes for C03 and C15 — November 7, 2026

Base: `7d8cd9eea` (main), shared checkout. Uncommitted working-tree changes;
no commit, branch or PR.

| ID | Status | Production change | Regression test |
| --- | --- | --- | --- |
| C03 | Fixed | `framework/entityui/record_form.go` | `framework/entityui/record_single_enum_test.go` |
| C15 | Fixed | `core-ui/runtime/src/widgets.js` | `core-ui/runtime/widget_inherited_name_e2e_test.go` |

## C03 — required single-value enum form

`typedInput` drew a segmented control for any required, non-compact enum
holding a declared value with at most four values. `ui.SegmentedControl`
panics below two options, and the record renderer replaced the whole form
with its failure callout. The segmented branch now also requires at least
two values, so a one-value required enum falls through to the select path:
one option, `required`, no placeholder (the existing `required` arm of the
select switch).

Unchanged: optional enums (select with the empty clearing choice),
required two-to-four-value enums (segmented), more than four values,
stale stored values (disabled placeholder), compact/inline editors, and
locked or `ReadOnly` fields, which return through `readOnly` before the
type switch.

`TestSingleValueEnumFormsRender` renders the record screen (`row1`) and the
create screen (empty id) through the public `UI.Record(...).RenderCtx`
builder for five configurations: required with default, required without
default, optional, required two-value, and required `ReadOnly`. It asserts
no danger callout and a title input on every screen, then the stage
control: option values `only` (required) or `,only` (optional) with a
matching `required` attribute, the segmented control for two values, and
no stage input for the locked field.

Doc: `framework/docs/content/entityui.md` (form controls paragraph) now says
which required enums draw a segmented control and which draw a select.

Guard proof: with the `len(f.Values) >= 2` condition removed, the
`required_default` and `required_no_default` subtests fail on both
screens; restored, all pass.

## C15 — widget mount guard and inherited names

`NS.mountWidget` tested `NS._widgets[cfg.name]`, an ordinary object read, so
`toString` and `valueOf` resolved to `Object.prototype` members and the
mount returned as if the widget were already mounted. The guard now uses
the module's existing own-property reader `own(...)`, which every other
registry read in `widgets.js` already used. The sibling reads in
`popover.js`, `rpc.js`, `widgetlinks.js` and `widgetfocus.js` were checked
and already test own properties; none changed.

`TestPrototypeNamedWidgetsMount` serves the real `RuntimeJS()` bundle and
embedded modules to chromedp, registers `notes`, `toString` and `valueOf`
as hidden catalog widgets with fetched chrome, and for each one: clicks its
`data-cui-open` trigger, waits for one root and an own registry entry,
calls `__gofastr.mountWidget` again on the mounted widget (the genuine
duplicate guard), then closes through `data-cui-action="close"`. It
asserts one root after the second mount, one `fui:widget-open`, one
`fui:widget-close`, and no root left after close.

Guard proof, two mutations: the old inherited read fails at `toString`
(no root ever appears); removing the guard entirely fails at `notes`
(the second mount adds a second root). Restored, the test passes.

`frag/kernel.js`, `nav.js` and the composed `runtime.js` were not edited:
`widgets.js` ships as a split module served by `Module("widgets")`, so the
canonical runtime assembly is unaffected.

## Tests run

All with `-count=1`, `-p 2` where a package list applies:

- `go test ./framework/entityui/` — ok
- `go test ./framework/entity/ -run '^TestEnumNeedsValues$'` — ok
- `go test ./framework/ui/ -run '^TestSegmented'` — ok
- `go test ./framework/docs/` — ok (identifier and corpus gates after the doc edit)
- `go test ./core-ui/runtime/ -run 'Widget|PrototypeNamed|Budget|Fragment|Integrity|ComposedRuntimeIsSymbolComplete|KernelMarkupMatchesGo|Module|Security|Forms|Dedup'` — ok (80 s)
- `go vet` with the repo `cmd/vettool` over `./framework/entityui/ ./core-ui/runtime/` — clean
- `gofmt -l` over the changed Go files — clean

## Limits

Coordinator follow-up: independently inspected both production diffs and
reran `TestSingleValueEnumFormsRender` and `TestPrototypeNamedWidgetsMount`
against the combined working tree; both pass. This does not extend the
worker's scoped coverage into a full runtime-suite claim.

- The full `core-ui/runtime` browser suite was not run; the findings ledger
  records its timeout. Only the filtered set above ran.
- Pixels were not checked: the C03 test reads rendered HTML, and the C15
  test reads DOM state and events in headless Chrome. Neither claims layout.
- One build of the shared checkout failed mid-run on another agent's
  in-progress `core/schema/decimal.go` edit; the entityui, entity, ui and
  docs runs above are from after that edit built again.
- Meridian was not re-verified end to end; neither change touches a style,
  token or component shape.
