# Form Module

Complete form infrastructure for GoFastr — HTML primitives, framework UI components, form patterns, validation, and accessibility.

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

`/components/forms` — comprehensive demo showcasing every form component with live examples, validation round-trip, and accessible markup.

## E2E Test Coverage

14 dedicated tests in `examples/website/e2e_form_module_test.go`:

- PasswordInput toggle (password→text)
- SearchInput clear button
- InputGroup prepend/append
- ConditionalField show/hide toggling
- StepWizard step rendering + navigation
- FormRepeater item count + add button
- ValidationSummary anchor links
- Validation round-trip error display
- Checkbox/Radio primitives present
- Form error callout
- Form method attributes
- Select dropdown options
- Label-to-input association
- Page load timing
