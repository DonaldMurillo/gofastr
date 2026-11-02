# Form module

Form components for GoFastr: HTML primitives, framework UI components, form patterns, validation, and accessibility.

## Components

### HTML primitives (`core-ui/html`)

| Component | Function | Description |
|-----------|----------|-------------|
| Checkbox | `html.Checkbox(CheckboxConfig)` | Native `<input type="checkbox">` with label |
| Radio | `html.Radio(RadioConfig)` | Native `<input type="radio">` with label |

### Framework UI components (`framework/ui`)

| Component | Function | Self-labeled | Runtime JS |
|-----------|----------|:---:|:---:|
| PasswordInput | `ui.PasswordInput(PasswordInputConfig)` | ✗ (use FormField) | ✓ toggle |
| SearchInput | `ui.SearchInput(SearchInputConfig)` | ✗ (use FormField) | ✓ clear |
| InputGroup | `ui.InputGroup(InputGroupConfig)` | ✗ (use FormField) | — |
| NumberInput | `ui.NumberInput(NumberInputConfig)` | ✓ | ✓ stepper |
| ColorPicker | `ui.ColorPicker(ColorPickerConfig)` | ✓ | — |
| RatingInput | `ui.RatingInput(RatingConfig)` | ✓ | — |
| TextArea | `ui.TextArea(TextAreaConfig)` | ✓ | ✓ autogrow (its own module) |
| RadioGroup | `ui.RadioGroup(RadioGroupConfig)` | ✓ (fieldset) | — |
| CheckboxGroup | `ui.CheckboxGroup(CheckboxGroupConfig)` | ✓ (fieldset) | — |
| ValidationSummary | `ui.ValidationSummary(ValidationSummaryConfig)` | — | — |
| ConditionalField | `ui.ConditionalField(ConditionalFieldConfig)` | — | ✓ toggle (headless module); one value (`WhenValue`) or any of a list (`WhenValues`) |
| StepWizard | `ui.StepWizard(StepWizardConfig)` | — | — |
| FormRepeater | `ui.FormRepeater(FormRepeaterConfig)` | — | — |

**Self-labeled** = component renders its own `<label>`. Don't wrap in `FormField`.
**Runtime JS** = requires a runtime module (auto-registered).

### Form containers

| Component | Function | Purpose |
|-----------|----------|---------|
| Form | `ui.Form(FormConfig)` | `<form>` with action, method, optional FieldErrors; `SubmitFullWidth` makes the actions row a block column so the primary button spans the card (mobile-first auth/wizard forms) |
| FormFrame | `ui.FormFrame(FormFrameConfig)` | Two-column record-form frame: a wide main column beside a narrow side rail, stacking on the form's own width |
| FormField | `ui.FormField(FormFieldConfig)` | Label + input + help/error wrapper |
| FormFieldFor | `ui.FormFieldFor(errs, name, config)` | FormField with per-field error from FieldErrors |
| TextField | `ui.TextField(TextFieldConfig)` | Self-labelled native text input with typed common attributes |
| NumberField | `ui.NumberField(NumberFieldConfig)` | Self-labelled native number input; use `NumberInput` for +/- controls |
| DateField | `ui.DateField(DateFieldConfig)` | Self-labelled native date input with typed min/max bounds |
| DateTimeField | `ui.DateTimeField(DateTimeFieldConfig)` | Self-labelled native date-and-time input with min/max bounds and a step in seconds (`float64`; `0.001` shows milliseconds) |
| DatePicker | `ui.DatePicker(DatePickerConfig)` | A calendar beside the native date input, only for what the native picker cannot do: a range, refused days, notes on days (see "When the native picker is not enough") |

The typed field wrappers compose `FormField`'s builder with the styled
native control and own the `for`/`id`, `aria-describedby`, and
`aria-invalid` wiring by construction: the field hands its control the
wiring before the control renders. Prefer them for ordinary forms so
`Required`, `Placeholder`, bounds, value, help, and error states stay
visible in the Go type instead of being repeated through `html.Attrs`
literals at each call site. For the input types they do not name
(email, password, file, tel, url, search), build a `ui.Control` inside
a `FormField` builder.

### Dates and times

Use `ui.DateField` for a date and `ui.DateTimeField` for a date with a
time of day. Both render the browser's native picker
(`<input type="date">` and `<input type="datetime-local">`), and GoFastr
ships no custom calendar. The native picker is accessible, follows the
user's locale and OS, opens the platform's own picker on phones, and
needs no JavaScript. Values travel in the HTML formats (`2026-07-22`,
`2026-07-22T09:30`), so the server parses one shape whatever the
display language.

```go
ui.DateField(ui.DateFieldConfig{
	Label: "Due date", Name: "due_on", Value: "2026-07-22",
	Min: "2026-01-01", Required: true,
})
```

Bound the range with `Min` and `Max`, and validate on the server as
for any other field: the browser refuses an out-of-range value before
submitting, but a request that skips the form reaches the handler
unchecked.
For a date range, use two fields (`starts_on`, `ends_on`) and reject
an end before the start in the handler.

### When the native picker is not enough

`ui.DatePicker` is a calendar that stays on the page beside the native
date input it fills. Reach for it only for what `DateField` cannot do:

- picking a range (a start and an end) in one control;
- a calendar that stays visible instead of a popup;
- refusing specific days (booked, closed, holidays), not just a min and max;
- writing data on the days ("3 left", "Full").

For one ordinary date, keep `DateField`. The picker does not replace
it, and there is no popup version.

The native `<input type="date">` is still the value. It is rendered,
named and submitted whatever the calendar does, so the form posts
`2026-11-12` without script, and a reader can always type the date.
The calendar is the WAI-ARIA date grid: arrow keys move a day or a week
(mirrored on a right-to-left page), Home and End go to the first and
last day of the row (on a month's first and last rows, the first and
last day of the month in that week), Page Up and Page Down move a month
and, with Shift, a year; Enter or Space picks. Each day is named by its full date in the reader's
language, today is `aria-current="date"`, and a refused day is
`aria-disabled` but still reachable. The week starts on the reader's
first day (`i18n.FirstWeekday`).

Changing the month is an island. Each month anchor keeps a real href to
the same page with the month in its query (the no-script path) and
GETs `Endpoint` with script; the endpoint answers `ui.DatePickerMonth`
and the calendar swaps in place, with focus kept on the day or anchor
it was on. Keys pressed while a month loads are not lost: they count
from the day the last one asked for. A month that fails to load is
toasted and the calendar stays where it was. The shown month is held
between `Min`'s and `Max`'s months, and a month anchor moves at most to
that edge. Build the page and the endpoint from one function, so they
agree on the days:

<!-- gofastr:compile
import "net/http"
import "github.com/DonaldMurillo/gofastr/framework/ui"
func today(*http.Request) string { return "2026-11-02" }
-->
```go
func stay(r *http.Request) ui.DatePickerConfig {
	return ui.DatePickerConfig{
		ID: "stay", Label: "Your stay", Endpoint: "/book/month",
		Range: &ui.DatePickerRange{StartName: "check_in", EndName: "check_out"},
		Month: r.URL.Query().Get("month"), // the page and the endpoint alike
		Today: today(r), Min: today(r),    // the reader's today, in their zone
		Days: map[string]ui.DatePickerDay{
			"2026-11-13": {Disabled: true, Note: "Full"},
			"2026-11-14": {Note: "2 left"},
		},
		Ctx: r.Context(),
	}
}

// The page: ui.DatePicker(stay(r)) inside the booking form.
// The endpoint, a read with no state:
func bookMonth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.DatePickerMonth(stay(r))))
}
```

A single date takes `Name` and `Value` instead of `Range`, and renders
as a `FormField` (its `Help` and `Error` describe the input); a range
renders as a fieldset named by `Label`, its inputs labelled
`StartLabel` and `EndLabel` (default "Start date" and "End date" in the
reader's language).

Things to get right in the host:

- **The page reads `month` too.** The no-script href is the page with
  `?month=`; the page's own handler passes the query's month, as the
  endpoint does. Without script a month change is a page load, so
  anything typed in the form is lost; the native input stays usable
  either way.
- **`Path` is the page's constant path, never `r.URL.Path`.** The
  endpoint renders the same config, and its `r.URL.Path` is the
  endpoint's.
- **One endpoint per picker**, or carry what tells them apart in
  `Query`: the endpoint receives only the month and `Query`.
- **`Today`, `Min` and `Max` are configuration.** One that is not a
  date panics at render. A check-out picker whose `Min` is the
  submitted check-in parses that value first; `Value` and `Month` are
  the fields that take request data and fall back.
- **The handler still validates** what it receives: a refused day is
  refused in the calendar, not in a request that skips it, and a range
  may come back with the end equal to the start (one day) or empty.

## Validation

Server-side validation uses `ui.FieldErrors` (a `map[string]string` of field-name → error-message).

```go
errs := ui.FieldErrors{
    "email": "Please enter a valid email address.",
    "name":  "Name is required.",
}

// Pass to Form for the summary (ID is required: the summary's id is
// derived from it, and focus moves to the summary after a failed submit)
ui.Form(ui.FormConfig{Action: "/submit", Method: "POST", ID: "signup", Errors: errs}, ...)

// Per-field error display
ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
    Label: "Email", For: "f-email",
    Input: func(c headless.FieldControl) render.HTML {
        return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email"})
    },
})
```

### ValidationSummary

Standalone component rendering `<div role="alert"><ul>` with anchor links per error:

```go
ui.ValidationSummary(ui.ValidationSummaryConfig{
    ID: "signup-errors",
    Errors: errs,
    FieldLabels: map[string]string{"email": "Email", "name": "Name"},
    // FieldIDs maps a field name to its control's real id when they
    // differ; an error whose field has no known id renders as text.
    FieldIDs: map[string]string{"email": "f-email", "name": "f-name"},
})
```

## Form patterns

### Conditional fields

Show/hide fields based on another field's value. The region renders
VISIBLE and the headless behaviour module hides it until the watched
field matches (`data-hui-when` / `data-hui-when-value`), because a
field only a script can reveal is a field a reader without script
never reaches. The module also disables the controls inside a hidden
region, so nothing hidden submits.

```go
ui.RadioGroup(ui.RadioGroupConfig{
    Name: "type", Legend: "Account type",
    Options: []ui.RadioGroupOption{
        {Label: "Personal", Value: "personal"},
        {Label: "Business", Value: "business"},
    },
})
ui.ConditionalField(ui.ConditionalFieldConfig{
    WhenName: "type", WhenValue: "business",
    Children: []render.HTML{...},
})
```

The `in` condition shows the region while the watched field holds ANY
of a list of values (`WhenValues`, carried as one JSON attribute,
`data-hui-when-in`, so a value containing a comma, a quote or a
bracket stays one value). Select, radio-group and checkbox controllers
drive it exactly as they drive the single value: the checked radio's
value, the checkbox's value when checked, any other control's value.
`WhenValue` and `WhenValues` are mutually exclusive; neither empty
values nor an empty list are accepted, for the same reason the single
value refuses the empty string.

```go
ui.Select(ui.SelectConfig{
    Name: "notify", Label: "Notify",
    Options: []ui.SelectOption{
        {Value: "none", Text: "Never"},
        {Value: "email", Text: "Email"},
        {Value: "webhook", Text: "Webhook"},
    },
})
ui.ConditionalField(ui.ConditionalFieldConfig{
    WhenName: "notify",
    WhenValues: []string{"email", "webhook"},
    Children: []render.HTML{...},
})
```

### Record form frame

A record form in two columns: the bulk of the fields in a wide main
column, short controllers (status, owner, dates) in a narrow side
rail. The frame switches on its OWN width — a container query, not a
viewport media query — so the same form sits side by side on a full
page and stacks in a drawer on a wide screen. Below 48rem of the
frame's width the side column drops under the main one; the rail's
width is one of three named sizes — `SideWidth:
ui.FormFrameSideNarrow` (12rem), the default (16rem) and
`ui.FormFrameSideWide` (22rem) — each a modifier class whose
registered CSS reads the `--ui-form-frame-side-narrow` / `-wide`
tokens, so a theme retunes them. The main column stops at 45rem
(`--ui-form-frame-main-max`): a text input wider than that is mostly
empty box. `SidePanel: true` draws the side column as a bordered
surface panel; beside the main column it is sticky, so the record's
facts stay in view while a long form scrolls, and stacked under it
(a drawer, a phone) it sits in the flow. Either column may be empty: an
empty `Side` leaves the main column full width, and an empty `Main`
leaves the side column full width.

Note that `ui.Form` caps its own measure at `--ui-form-max` (42rem by
default), which sits below the split point: a record page that wants
the rail widens the form (`--ui-form-max`) or draws the frame outside
the capped form element.

```go
ui.Form(ui.FormConfig{Action: "/customers/42", ID: "customer"},
    ui.FormFrame(ui.FormFrameConfig{
        Main: []render.HTML{numberField, amountField, datesSection},
        Side: []render.HTML{statusField, customerField},
    }))
```


### Step wizard

Multi-step form with progress indicator. Pure server-driven: each Continue/Back click is a form POST.

```go
ui.StepWizard(ui.StepWizardConfig{
    Action: "/wizard",
    Steps: []ui.StepWizardStep{
        {Heading: "Personal info", Fields: []render.HTML{...}},
        {Heading: "Preferences",   Fields: []render.HTML{...}},
        {Heading: "Review",        Fields: []render.HTML{...}},
    },
})
```

A step that fails server-side validation re-renders through the same
summary `ui.Form` uses: pass the errors (plus the form's `ID`, which
the summary derives its own id from) and set the failing field's
`Error` on the step's fields.

```go
ui.StepWizard(ui.StepWizardConfig{
    Action: "/wizard", CurrentStep: step, ID: "wiz-form",
    Errors: ui.FieldErrors{"name": "Your full name is required."},
    Steps:  []ui.StepWizardStep{
        {Heading: "Personal info", Fields: []render.HTML{
            ui.TextField(ui.TextFieldConfig{Name: "name", Label: "Full name", ID: "name",
                Required: true, Error: "Your full name is required."}),
        }},
    },
})
```

The wizard renders the summary between the progress indicator and the
step's fields and marks the form (`data-hui-form-errors`), so the
runtime moves focus to the summary after the failed submit — the same
announcement a failed `ui.Form` gets.

### Form repeater

Dynamic repeating field groups. Server-driven add/remove via `name_add`/`name_remove` POST fields.

```go
ui.FormRepeater(ui.FormRepeaterConfig{
    Name: "members", MinItems: 1, MaxItems: 5,
    AddLabel: "Add member", RemoveLabel: "Remove",
    Items: [][]render.HTML{...},
})
```

## Accessibility

All components pass **axe-core 4.10** with zero violations:

- **Labels**: Every input has an associated `<label>` via `for`/`id` or `aria-label`
- **Error states**: `aria-invalid="true"` + `aria-describedby` linking to error message
- **Keyboard**: All interactive elements focusable and operable via keyboard
- **Contrast**: All text meets WCAG 2.1 AA minimum (4.5:1 for normal text)
- **Roles**: Proper ARIA roles (`role="alert"`, `role="list"`, `role="listitem"`, etc.)
- **Hidden state**: the conditional region carries `hidden` (and nothing else) while inactive; the headless module owns it

## Runtime JS modules

| Module | File | Purpose |
|--------|------|---------|
| searchinput | `framework/ui/searchinput.js` | Clear button + auto-show/hide (registered behaviour, loaded on its marker) |
| textarea | `core-ui/runtime/src/textarea.js` | Autogrow (`data-cui-autogrow`) |
| filedropzone | `framework/ui/filedropzone.js` | FileDropzone's image thumbnail strip |

Four modules this family used to carry are gone: `passwordinput`,
`conditionalfield`, `fileupload` and `dropzone`. The headless
behaviour module (`framework/headless/behavior.js`) binds all of
those behaviours on its `data-hui-*` hooks (`data-hui-reveal` for the
password reveal, `data-hui-when` for a conditional region,
`data-hui-drop` for both file zones), listing the chosen names and
announcing the pick through the `Strings` sentences the component
resolved per request. The thumbnail strip is the one piece with no
headless counterpart, so it ships as framework/ui's own module.

## Demo page

`/components/forms` is a demo page showing every form component with live examples, validation round-trip, and accessible markup.

## Common mistakes

- **Wrapping a self-labeled component in `FormField`.** Components
  marked "Self-labeled" in the table above (`NumberInput`, `TextArea`,
  `RadioGroup`, `CheckboxGroup`, `ColorPicker`, `RatingInput`) already
  render their own `<label>` element. Wrapping them in `FormField`
  produces a double-label, breaks `for`/`id` linking, and fails axe
  validation.
- **Using `FormField` without setting `For` + `ID`.** The
  label-to-input association is `<label for="X">` + `<input id="X">`.
  If `FormField.For` and the inner input's `ID` don't match, screen
  readers can't pair them and axe will report a violation.
- **Passing `ui.FieldErrors` with camelCase keys.** `FieldErrors` keys
  must match the HTML field `name` attribute exactly (typically
  snake_case or the CRUD entity field name). A key mismatch causes the
  error to silently not render next to the intended field.
- **Expecting `ConditionalField` to hide server-side.** The region
  renders visible on first paint and the runtime hides it when the
  watched field does not match; the _server_ does not skip the field
  from the HTML. The module disables the controls inside a hidden
  region so they do not submit, but server-side logic must still
  validate what it receives.
- **Relying on `StepWizard` to prevent multi-step submission.** Each
  Continue/Back click is a regular form POST; the wizard does not
  disable earlier-step fields. Validate each step's data on the server
  for the relevant step before advancing.
- **Building a calendar popup for a date field.** `ui.DateField` and
  `ui.DateTimeField` already open the platform's picker. A hand-rolled
  calendar has to rebuild its keyboard grid, screen-reader
  announcements, locale week start and phone behavior, and still
  submit the HTML date format. For a range, refused days or notes on
  days, use `ui.DatePicker`, which enhances the native input instead
  of replacing it. See "Dates and times" above.

## E2E test coverage

7 dedicated tests in `examples/site/e2e_form_module_test.go`:

- PasswordInput renders and toggles (password→text)
- SearchInput clear button
- InputGroup renders prepend/append
- ValidationSummary renders anchor links
- Form field order and title
- Checkbox/Radio primitives present
- Forms page loads quickly
