# Form Module

Form components for GoFastr: HTML primitives, framework UI components, form patterns, validation, and accessibility.

## Components

### HTML Primitives (`core-ui/html`)

| Component | Function | Description |
|-----------|----------|-------------|
| Checkbox | `html.Checkbox(CheckboxConfig)` | Native `<input type="checkbox">` with label |
| Radio | `html.Radio(RadioConfig)` | Native `<input type="radio">` with label |

### Framework UI Components (`framework/ui`)

| Component | Function | Self-labeled | Runtime JS |
|-----------|----------|:---:|:---:|
| PasswordInput | `ui.PasswordInput(PasswordInputConfig)` | ✗ (use FormField) | ✓ toggle |
| SearchInput | `ui.SearchInput(SearchInputConfig)` | ✗ (use FormField) | ✓ clear |
| InputGroup | `ui.InputGroup(InputGroupConfig)` | ✗ (use FormField) | — |
| NumberInput | `ui.NumberInput(NumberInputConfig)` | ✓ | ✓ stepper |
| ColorPicker | `ui.ColorPicker(ColorPickerConfig)` | ✓ | — |
| RatingInput | `ui.RatingInput(RatingConfig)` | ✓ | — |
| TextArea | `ui.TextArea(TextAreaConfig)` | ✓ | ✓ autogrow |
| RadioGroup | `ui.RadioGroup(RadioGroupConfig)` | ✓ (fieldset) | — |
| CheckboxGroup | `ui.CheckboxGroup(CheckboxGroupConfig)` | ✓ (fieldset) | — |
| ValidationSummary | `ui.ValidationSummary(ValidationSummaryConfig)` | — | — |
| ConditionalField | `ui.ConditionalField(ConditionalFieldConfig)` | — | ✓ toggle |
| StepWizard | `ui.StepWizard(StepWizardConfig)` | — | — |
| FormRepeater | `ui.FormRepeater(FormRepeaterConfig)` | — | — |

**Self-labeled** = component renders its own `<label>`. Don't wrap in `FormField`.
**Runtime JS** = requires a runtime module (auto-registered).

### Form Containers

| Component | Function | Purpose |
|-----------|----------|---------|
| Form | `ui.Form(FormConfig)` | `<form>` with action, method, optional FieldErrors |
| FormSection | `ui.FormSection(FormSectionConfig)` | Fieldset grouping with heading |
| FormField | `ui.FormField(FormFieldConfig)` | Label + input + help/error wrapper |
| FormFieldFor | `ui.FormFieldFor(errs, name, config)` | FormField with per-field error from FieldErrors |
| TextField | `ui.TextField(TextFieldConfig)` | Self-labelled native text input with typed common attributes |
| NumberField | `ui.NumberField(NumberFieldConfig)` | Self-labelled native number input; use `NumberInput` for +/- controls |
| DateField | `ui.DateField(DateFieldConfig)` | Self-labelled native date input with typed min/max bounds |

The typed field wrappers compose `FormField + html.Input` and own the
`for`/`id`, `aria-describedby`, and `aria-invalid` wiring. Prefer them for
ordinary forms so `Required`, `Placeholder`, bounds, value, help, and error
states stay visible in the Go type instead of being repeated through
`html.Attrs` literals at each call site.

## Validation

Server-side validation uses `ui.FieldErrors` (a `map[string]string` of field-name → error-message).

```go
errs := ui.FieldErrors{
    "email": "Please enter a valid email address.",
    "name":  "Name is required.",
}

// Pass to Form for the error callout
ui.Form(ui.FormConfig{Action: "/submit", Method: "POST", Errors: errs}, ...)

// Per-field error display
ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
    Label: "Email", For: "f-email",
    Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "f-email"}),
})
```

### ValidationSummary

Standalone component rendering `<div role="alert"><ul>` with anchor links per error:

```go
ui.ValidationSummary(ui.ValidationSummaryConfig{
    Errors: errs,
    FieldLabels: map[string]string{"email": "Email", "name": "Name"},
})
```

## Form Patterns

### Conditional Fields

Show/hide fields based on another field's value. Runtime JS listens for `change`/`input` on the parent form and toggles `hidden`+`aria-hidden`.

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

### Step Wizard

Multi-step form with progress indicator. Pure server-driven — each Continue/Back click is a form POST.

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

### Form Repeater

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
- **Hidden state**: Conditional fields use `hidden` + `aria-hidden="true"` when inactive

## Runtime JS Modules

| Module | File | Purpose |
|--------|------|---------|
| passwordinput | `core-ui/runtime/src/passwordinput.js` | Toggle password visibility |
| searchinput | `core-ui/runtime/src/searchinput.js` | Clear button + auto-show/hide |
| conditionalfield | `core-ui/runtime/src/conditionalfield.js` | Show/hide based on parent field value |

## Demo Page

`/components/forms` — demo page showing every form component with live examples, validation round-trip, and accessible markup.

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
- **Expecting `ConditionalField` to hide server-side.** The visibility
  toggle runs in the browser. On first load, `ConditionalField` renders
  with the `hidden` attribute when the condition is false — but the
  _server_ does not skip the field from the HTML. Server-side logic
  must independently ignore values from hidden fields.
- **Relying on `StepWizard` to prevent multi-step submission.** Each
  Continue/Back click is a regular form POST; the wizard does not
  disable earlier-step fields. Validate each step's data on the server
  for the relevant step before advancing.

## E2E Test Coverage

7 dedicated tests in `examples/site/e2e_form_module_test.go`:

- PasswordInput renders and toggles (password→text)
- SearchInput clear button
- InputGroup renders prepend/append
- ValidationSummary renders anchor links
- Form field order and title
- Checkbox/Radio primitives present
- Forms page loads quickly
