# Form Module Implementation Plan

## Existing infrastructure
- `core-ui/html/interactive.go`: Input, Select, TextArea, Label, FieldSet, Legend, Form, Button
- `framework/ui/form.go`: Form, FormFieldFor, FieldErrors
- `framework/ui/components.go`: FormField, FormSection
- `framework/ui/styles_components.go`: formFieldCSS, formSectionCSS, formCSS
- `framework/ui/textarea.go`: TextArea component (autogrow, rows, etc)
- `framework/ui/select.go`: Select component (native styled)
- `framework/ui/toggle.go`: RadioGroup, CheckboxGroup (fieldset-based)
- `framework/ui/numberinput.go`: NumberInput with stepper
- `framework/ui/slider.go`, `rangeslider.go`: Range inputs
- `framework/ui/colorpicker.go`: Color input wrapper
- `framework/ui/rating.go`: Star rating input
- `framework/ui/taginput.go`: Chip/tag input
- `framework/ui/dropzone.go`, `fileupload.go`: File inputs
- `framework/ui/timepicker.go`: Time input
- `framework/ui/segmented.go`: Segmented control

## Wave 1: HTML Primitives (core-ui/html/interactive.go)
Add to existing file:
1. **Checkbox** — `<input type="checkbox">` + `<label>` wrapper
   - `CheckboxConfig`: Name, Value, ID, Checked, Label, Class, Attrs
   - Returns: `<input type="checkbox">` element (label separate via FormField)
2. **Radio** — `<input type="radio">` 
   - `RadioConfig`: Name, Value, ID, Checked, Class, Attrs
   - Returns: `<input type="radio">` element

## Wave 2: Framework UI Components (framework/ui/)
1. **PasswordInput** (`passwordinput.go`)
   - Password field with show/hide toggle button
   - Config: Name, ID, Placeholder, Required, ShowToggle, Class, Attrs
   - Runtime JS: toggle `type` between `password`/`text`
   - Accessibility: `aria-label` on toggle, announced state change

2. **SearchInput** (`searchinput.go`)
   - Text input with search icon + clear button
   - Config: Name, ID, Placeholder, Action, Class, Attrs
   - Clear button: `type="reset"` or JS clear

3. **InputGroup** (`inputgroup.go`)
   - Wrapper that prepends/appends text or icons to an input
   - Config: Prepend (render.HTML), Append (render.HTML), Input (render.HTML)
   - CSS: flex container with visual join

4. **InlineValidationSummary** (add to form.go)
   - Error list with anchor links to each field
   - Config: Errors (FieldErrors), FieldLabels (map[name]label)
   - Renders: `<ul role="list">` with `<a href="#field-id">label: error</a>`

## Wave 3: Form Patterns (framework/ui/)
1. **ConditionalField** (`conditionalfield.go`)
   - Show/hide based on another field's value
   - Config: When (CSS selector like `[name=plan]:checked[value=pro]`), Children
   - CSS-only using `:has()` or sibling selectors — NO JS
   - Hidden with `display:none` + `aria-hidden`

2. **FormStepWizard** (`stepwizard.go`)
   - Multi-step form with progress indicator
   - Config: Steps ([]StepConfig), CurrentStep (int)
   - StepConfig: Heading, Description, Fields (render.HTML)
   - Server-driven: POST advances/retreats step
   - Progress bar: StepIndicator already exists in framework

3. **DynamicFormRepeater** (`formrepeater.go`)
   - Add/remove repeating field groups
   - Config: Name, Template ([]render.HTML), MinItems, MaxItems
   - Server renders initial items; "Add" button POSTs for new row
   - "Remove" button POSTs to remove row

## Wave 4: Demo Page + Backend
1. **Dedicated forms page** (`screen_component_forms.go`)
   - Route: `/components/forms`
   - Sections for every form component with live examples
   - Full CRUD form connected to backend
   - Validation demo (pristine → submit → errors → fix → success)

## Wave 5: E2E Tests
- Every new component: renders, has correct ARIA attributes
- Form submission: validation round-trip
- PasswordInput: toggle works
- SearchInput: clear works
- ConditionalField: show/hide works
- StepWizard: navigation works
- FormRepeater: add/remove works
- axe-core on `/components/forms` page

## Wave 6: Documentation
- godoc on all new public types/functions
- Update ARCHITECTURE.md with form patterns
- Update roadmap with completed items

## Key conventions
- No inline `style` attributes (CSP linter)
- CSS in `registry.RegisterStyle` with theme tokens
- SSR-first, runtime JS only for interactive enhancements
- `html.Button` takes only `ButtonConfig` — use `render.Tag("button", ...)` for children
- Import path: `core/render` not `core-ui/render`
- Runtime modules register in `_moduleMarkers` + set `loadedModules.<name>`
