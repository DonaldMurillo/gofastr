# Form module

GoFastr ships accessible, server-first form primitives in `core-ui/html` and `framework/ui`. All components pass axe-core 4.10 with zero violations and work without client-side JavaScript for core submission.

## Component inventory

### Containers & typed fields (`framework/ui`)

| Component | Purpose |
|---|---|
| `ui.Form` | `<form>` wrapper with `Action`, `Method`, `ID`, optional `Errors` (`FieldErrors`), `SubmitFullWidth`, `Multipart` (`multipart/form-data`), and `LeaveGuard` |
| `ui.FormFrame` | Container-query two-column record form (`Main` ≤45rem + `Side` rail with `SideWidth` and sticky `SidePanel`, stacking below 48rem) |
| `ui.FormField` / `ui.FormFieldFor` | Label + control builder (`headless.FieldControl`) + help/error wiring (`for`/`id`, `aria-describedby`, `aria-invalid`) |
| `ui.TextField` / `ui.NumberField` | Self-labeled native text and number inputs |
| `ui.DateField` / `ui.DateTimeField` | Self-labeled native `<input type="date">` / `datetime-local` pickers (locale-aware, zero JS) |
| `ui.DatePicker` | Inline WAI-ARIA calendar beside a native date input (only for ranges, disabled days, or day notes) |
| `ui.ValidationSummary` | `<div role="alert">` error list with anchor links to each invalid field |

### Specialized inputs & patterns (`framework/ui` & `core-ui/html`)

| Component | Self-labeled | Runtime JS | Notes |
|---|:---:|:---:|---|
| `html.Checkbox` / `html.Radio` | ✓ | — | Native HTML primitives |
| `ui.PasswordInput` | ✗ (use `FormField`) | ✓ (`headless`) | Show/hide toggle, no spellcheck/autocapitalize |
| `ui.OneTimeCode` | ✗ (use `FormField`) | — | 6-digit numeric text input (`autocomplete="one-time-code"`) |
| `ui.SearchInput` | ✗ (use `FormField`) | ✓ (`searchinput`) | Clear button + `<search>` landmark |
| `ui.InputGroup` | ✗ (use `FormField`) | — | Prefix/suffix adornments |
| `ui.NumberInput` | ✓ | ✓ (`headless-controls`) | Stepper +/− controls |
| `ui.ColorPicker` / `ui.RatingInput` | ✓ | ✓ (`headless`) | Hex/swatch sync; accessible star rating |
| `ui.TextArea` | ✓ | ✓ (`textarea`) | Optional `Autogrow: true` |
| `ui.RadioGroup` / `ui.CheckboxGroup` | ✓ (`<fieldset>`) | — | Group-level legend and error |
| `ui.ConditionalField` | — | ✓ (`headless-when`) | Show/hide + disable by `WhenValue` or `WhenValues` |
| `ui.StepWizard` | — | optional (`headless-wizard`) | Server-driven multi-step form with error summary |
| `ui.FormRepeater` | — | optional (`headless-collections`) | Repeating rows via `name_add` / `name_remove` POST fields |

**Self-labeled** components render their own `<label>` or `<fieldset>`—do not wrap them in `ui.FormField`. For input types without a dedicated wrapper (`email`, `tel`, `url`, `file`), render `ui.Control` inside a `ui.FormField` builder.

## Dates, times & `ui.DatePicker`

Prefer `ui.DateField` and `ui.DateTimeField` for ordinary dates—they use the platform's accessible native picker and submit standard HTML formats (`2026-07-22`, `2026-07-22T09:30`). Always validate bounds on the server.

Reach for `ui.DatePicker` only when you need date ranges, blocked/booked dates, or per-day notes ("2 left"). It keeps the native `<input type="date">` for form submission and swaps months via an island endpoint returning `ui.DatePickerMonth` (with plain `?month=` fallback links when JS is off):

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
		Month: r.URL.Query().Get("month"),
		Today: today(r), Min: today(r),
		Days: map[string]ui.DatePickerDay{
			"2026-11-13": {Disabled: true, Note: "Full"},
			"2026-11-14": {Note: "2 left"},
		},
		Ctx: r.Context(),
	}
}

func bookMonth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.DatePickerMonth(stay(r))))
}
```

Pass the page's constant `Path` (never `r.URL.Path`), read `?month=` in both the page and month endpoint, validate submitted dates/ranges server-side, and parse untrusted input before passing to `Today`/`Min`/`Max` (invalid config dates panic at render).

## Validation

Use `ui.FieldErrors` (`map[string]string` keyed by HTML `name`) with `ui.Form` (set `ID` so focus jumps to the summary on failed submit) and `ui.FormFieldFor`:

```go
errs := ui.FieldErrors{
    "email": "Please enter a valid email address.",
    "name":  "Name is required.",
}

ui.Form(ui.FormConfig{Action: "/submit", Method: "POST", ID: "signup", Errors: errs},
    ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
        Label: "Email", For: "f-email",
        Input: func(c headless.FieldControl) render.HTML {
            return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email"})
        },
    }),
)
```

For standalone summaries, render `ui.ValidationSummary` with `FieldLabels` and `FieldIDs`. Invalid fields also highlight client-side on `:user-invalid` once touched.

## Form patterns

- **Conditional fields (`ui.ConditionalField`):** Renders visible on SSR (accessible without JS); `headless-when` hides and disables non-matching regions (`WhenValue` or `WhenValues`, mutually exclusive, non-empty) so hidden controls don't submit. Still validate on the server.
- **Record form frame (`ui.FormFrame`):** Two-column layout switching via container query at 48rem (`SideWidth`: `FormFrameSideNarrow` 12rem, default 16rem, `FormFrameSideWide` 22rem; `SidePanel: true` for a sticky bordered rail). Note that `ui.Form` caps width at `--ui-form-max` (42rem) by default—widen `--ui-form-max` or place `FormFrame` outside the capped width to let it split.
- **Step wizard (`ui.StepWizard`):** Server-driven multi-step POST form; pass `ID`, `CurrentStep`, and `Errors` to focus the validation summary on failure.
- **Repeater (`ui.FormRepeater`):** Server-driven dynamic rows (`MinItems`, `MaxItems`, `AddLabel`, `RemoveLabel`) backed by `name_add`/`name_remove` POST fields.

Live demos of all form components are served at `/components/forms` in `examples/site`.

## Common mistakes

- **Wrapping self-labeled components in `ui.FormField`.** `NumberInput`, `TextArea`, `RadioGroup`, `CheckboxGroup`, `ColorPicker`, and `RatingInput` render their own label/fieldset—wrapping them produces duplicate labels and axe failures.
- **Mismatching `FormFieldConfig.For` and the input `ID`.** Use the `headless.FieldControl` handed to `FormField`'s `Input` builder so `id`, `aria-describedby`, and `aria-invalid` always match.
- **Using camelCase keys in `ui.FieldErrors`.** Keys must match the HTML `name` attribute (typically `snake_case`) or errors won't attach to the field.
- **Assuming `ConditionalField` skips server validation.** Hidden regions are disabled in JS, not omitted from the server's POST handler—validate all submitted fields on the server.
- **Rebuilding a custom calendar popup.** Use `ui.DateField` / `ui.DateTimeField` for ordinary dates and `ui.DatePicker` only for ranges, blocked days, or day notes.
