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
| ConditionalField | `ui.ConditionalField(ConditionalFieldConfig)` | — | ✓ toggle (headless module) |
| StepWizard | `ui.StepWizard(StepWizardConfig)` | — | — |
| FormRepeater | `ui.FormRepeater(FormRepeaterConfig)` | — | — |

**Self-labeled** = component renders its own `<label>`. Don't wrap in `FormField`.
**Runtime JS** = requires a runtime module (auto-registered).

### Form containers

| Component | Function | Purpose |
|-----------|----------|---------|
| Form | `ui.Form(FormConfig)` | `<form>` with action, method, optional FieldErrors; `SubmitFullWidth` makes the actions row a block column so the primary button spans the card (mobile-first auth/wizard forms) |
| FormSection | `ui.FormSection(FormSectionConfig)` | Fieldset grouping with heading |
| FormField | `ui.FormField(FormFieldConfig)` | Label + input + help/error wrapper |
| FormFieldFor | `ui.FormFieldFor(errs, name, config)` | FormField with per-field error from FieldErrors |
| TextField | `ui.TextField(TextFieldConfig)` | Self-labelled native text input with typed common attributes |
| NumberField | `ui.NumberField(NumberFieldConfig)` | Self-labelled native number input; use `NumberInput` for +/- controls |
| DateField | `ui.DateField(DateFieldConfig)` | Self-labelled native date input with typed min/max bounds |

The typed field wrappers compose `FormField`'s builder with the styled
native control and own the `for`/`id`, `aria-describedby`, and
`aria-invalid` wiring by construction: the field hands its control the
wiring before the control renders. Prefer them for ordinary forms so
`Required`, `Placeholder`, bounds, value, help, and error states stay
visible in the Go type instead of being repeated through `html.Attrs`
literals at each call site. For the input types they do not name
(email, password, datetime-local, file, tel, url, search), build a
`ui.Control` inside a `FormField` builder.

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
| searchinput | `core-ui/runtime/src/searchinput.js` | Clear button + auto-show/hide |
| textarea | `core-ui/runtime/src/textarea.js` | Autogrow (`data-fui-autogrow`) |
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

## E2E test coverage

7 dedicated tests in `examples/site/e2e_form_module_test.go`:

- PasswordInput renders and toggles (password→text)
- SearchInput clear button
- InputGroup renders prepend/append
- ValidationSummary renders anchor links
- Form field order and title
- Checkbox/Radio primitives present
- Forms page loads quickly
