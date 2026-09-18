package main

// =============================================================================
// Wizard E2E demo, /forms/wizard (ported from examples/website).
//
// A self-contained 3-step wizard with a real round-trip server: Continue/Back
// post the form, the handler accumulates entered values via hidden fields, the
// wizard re-renders at the new step, and the final Submit captures the payload
// to a package-local var the wizard E2E tests assert against.
//
// The page is intentionally minimal, no site chrome, no SPA shell, because
// the form submits to itself with method=POST and the response must be served
// as the next full page. The wizard handler is registered in main.go.
// =============================================================================

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const wizardDemoPath = "/forms/wizard"

// wizardDemoFields lists every input the wizard collects across its three
// steps. Any field not on the current step is carried forward via a hidden
// input so the server-side accumulator picks it up on the next POST.
var wizardDemoFields = []string{
	"wd-name", "wd-email", "wd-theme", "wd-comments",
}

var (
	wizardDemoMu       sync.Mutex
	wizardDemoLastVals url.Values
)

// wizardDemoReset clears the last-submission record. Used by tests.
func wizardDemoReset() {
	wizardDemoMu.Lock()
	wizardDemoLastVals = nil
	wizardDemoMu.Unlock()
}

// wizardDemoLast returns the most recent captured form payload, or nil.
func wizardDemoLast() url.Values {
	wizardDemoMu.Lock()
	defer wizardDemoMu.Unlock()
	if wizardDemoLastVals == nil {
		return nil
	}
	out := make(url.Values, len(wizardDemoLastVals))
	for k, v := range wizardDemoLastVals {
		dup := make([]string, len(v))
		copy(dup, v)
		out[k] = dup
	}
	return out
}

func wizardDemoStore(v url.Values) {
	wizardDemoMu.Lock()
	defer wizardDemoMu.Unlock()
	wizardDemoLastVals = v
}

// WizardDemoHandler serves the 3-step wizard at /forms/wizard.
//
// GET → render step 0 with empty values.
// POST → read wizard_action ("next" | "back") and _step (the step the form
// was submitted from), clamp the next step into [0, len-1], and re-render. On
// the final-step "next" the payload is recorded and a confirmation page shows.
// wizardDemoStepCount is how many steps the demo has; the handler and
// the completeness check read the same number.
const wizardDemoStepCount = 3

func WizardDemoHandler(w http.ResponseWriter, r *http.Request) {
	values := url.Values{}
	action := ""
	submittedStep := 0

	if r.Method == http.MethodPost {
		// Cap the body: a sitemap-listed public form must not buffer an
		// attacker-chosen body (the stdlib only refuses at its own 10 MiB
		// urlencoded floor). 4 KiB covers three short fields.
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
			return
		}
		for _, name := range wizardDemoFields {
			if v := r.PostForm.Get(name); v != "" {
				values.Set(name, v)
			}
		}
		action = r.PostForm.Get("wizard_action")
		if s := r.PostForm.Get("_step"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				submittedStep = n
			}
		}
	}

	// The server owns validation (the form is novalidate), so the
	// submitted step is validated before the flow advances or the
	// confirmation renders: a blank or malformed value re-renders the
	// same step with the field's error set and the summary populated.
	errs := ui.FieldErrors{}
	if r.Method == http.MethodPost && action == "next" {
		errs = wizardDemoValidate(submittedStep, values)
	}

	totalSteps := wizardDemoStepCount
	current := submittedStep

	switch action {
	case "next":
		// A step that failed validation re-renders itself; the flow
		// neither advances nor confirms on an invalid step.
		if len(errs) > 0 {
			current = submittedStep
			break
		}
		// Final-step Next means Submit, capture and confirm. Guard against
		// a stale POST with _step=last pushing past the last index.
		if submittedStep >= totalSteps-1 {
			// The step number came from the client, so a post of the
			// final step carrying only the final field would otherwise
			// confirm having skipped the name and the email. Every
			// required field is checked here, and a failure sends the
			// reader to the first step that is wrong.
			if all, badStep := wizardDemoComplete(values); len(all) > 0 {
				errs = all
				current = badStep
				break
			}
			wizardDemoStore(values)
			render.RespondHTML(w, wizardDemoConfirmation(values))
			return
		}
		current = submittedStep + 1
	case "back":
		current = submittedStep - 1
	default:
		current = 0
	}

	if current < 0 {
		current = 0
	}
	if current >= totalSteps {
		current = totalSteps - 1
	}

	render.RespondHTML(w, wizardDemoPage(current, values, errs))
}

// wizardDemoValidate checks the fields visible on the given step and
// returns the per-field errors. Steps past the wizard's range have no
// visible fields to validate (their POST is clamped onto a real step
// before rendering).
//
// It is not the whole story on a final submit: see wizardDemoComplete.
// wizardDemoComplete validates every required field the wizard
// collects, whatever step the client says it is on.
//
// The step number arrives in the request, so a client can post the
// FINAL step with only the final step's field and skip the ones
// before it. Validating the submitted step alone trusts that number;
// this is the check that does not. A field that fails here belongs to
// an earlier step, so the answer sends the reader back to the first
// step that is wrong rather than confirming or re-rendering a step
// whose own fields are fine.
func wizardDemoComplete(values url.Values) (ui.FieldErrors, int) {
	for step := range wizardDemoStepCount {
		if errs := wizardDemoValidate(step, values); len(errs) > 0 {
			return errs, step
		}
	}
	return ui.FieldErrors{}, 0
}

func wizardDemoValidate(step int, values url.Values) ui.FieldErrors {
	errs := ui.FieldErrors{}
	if step != 0 {
		return errs
	}
	if strings.TrimSpace(values.Get("wd-name")) == "" {
		errs["wd-name"] = "Your full name is required."
	}
	if v := strings.TrimSpace(values.Get("wd-email")); v == "" {
		errs["wd-email"] = "Your email is required."
	} else if _, err := mail.ParseAddress(v); err != nil {
		errs["wd-email"] = "That email address does not look right."
	}
	return errs
}

func wizardDemoPage(current int, values url.Values, errs ui.FieldErrors) render.HTML {
	wiz := ui.StepWizard(ui.StepWizardConfig{
		Action:       wizardDemoPath,
		Method:       "POST",
		CurrentStep:  current,
		HiddenFields: wizardDemoHiddenCarry(current, values),
		Steps:        wizardDemoSteps(values, errs),
		// The handler owns the step flow and answers every POST
		// server-side, so the form is novalidate for the same reason
		// the newsletter's is: a browser validation bubble on an
		// untouched step would trap the flow the demo exists to show.
		ExtraAttrs: html.Attrs{"novalidate": ""},
		// The failed submit re-renders through the same summary and
		// focus hook ui.Form uses; the control ids equal the field
		// names, so the summary's links need no FieldIDs map.
		ID:          "wd-form",
		Errors:      errs,
		FieldLabels: map[string]string{"wd-name": "Full name", "wd-email": "Email"},
		FieldOrder:  []string{"wd-name", "wd-email"},
	})

	body := render.Tag("body", nil,
		render.Tag("h1", nil, render.Text("Wizard demo")),
		wiz,
	)
	return render.HTML("<!doctype html>") +
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.Tag("title", nil, render.Text("Wizard demo")),
			),
			body,
		)
}

func wizardDemoConfirmation(values url.Values) render.HTML {
	items := []render.HTML{}
	for _, name := range wizardDemoFields {
		items = append(items,
			render.Tag("li", nil,
				html.Strong(html.TextConfig{}, render.Text(name+": ")),
				render.Text(values.Get(name)),
			))
	}
	body := render.Tag("body", map[string]string{"data-wizard-confirm": "true"},
		render.Tag("h1", nil, render.Text("Wizard submitted")),
		render.Tag("ul", nil, items...),
	)
	return render.HTML("<!doctype html>") +
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.Tag("title", nil, render.Text("Wizard submitted")),
			),
			body,
		)
}

// wizardDemoHiddenCarry emits hidden inputs for every field NOT visible on the
// current step, plus a _step marker so the handler knows which step the user
// just submitted from.
func wizardDemoHiddenCarry(current int, values url.Values) []render.HTML {
	visible := map[string]bool{}
	switch current {
	case 0:
		visible["wd-name"] = true
		visible["wd-email"] = true
	case 1:
		visible["wd-theme"] = true
	case 2:
		visible["wd-comments"] = true
	}
	out := []render.HTML{}
	for _, name := range wizardDemoFields {
		if visible[name] {
			continue
		}
		if v := values.Get(name); v != "" {
			out = append(out, render.VoidTag("input", map[string]string{
				"type":  "hidden",
				"name":  name,
				"value": v,
			}))
		}
	}
	out = append(out, render.VoidTag("input", map[string]string{
		"type":  "hidden",
		"name":  "_step",
		"value": strconv.Itoa(current),
	}))
	return out
}

func wizardDemoSteps(values url.Values, errs ui.FieldErrors) []ui.StepWizardStep {
	themeLight := []ui.RadioGroupOption{
		{Label: "Light", Value: "light"},
		{Label: "Dark", Value: "dark"},
	}
	currentTheme := values.Get("wd-theme")

	return []ui.StepWizardStep{
		{
			Heading:     "Personal info",
			Description: "Your basic details.",
			Fields: []render.HTML{
				ui.TextField(ui.TextFieldConfig{
					Name: "wd-name", Label: "Full name", ID: "wd-name", Required: true,
					Value: values.Get("wd-name"), Error: errs["wd-name"],
				}),
				ui.FormField(ui.FormFieldConfig{
					Label: "Email", For: "wd-email", Required: true,
					Error: errs["wd-email"],
					Input: func(c headless.FieldControl) render.HTML {
						return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "wd-email",
							Value: values.Get("wd-email")})
					},
				}),
			},
		},
		{
			Heading:     "Preferences",
			Description: "Pick a theme.",
			Fields: []render.HTML{
				wizardDemoRadioGroup("wd-theme", "Theme", themeLight, currentTheme),
			},
		},
		{
			Heading:     "Review",
			Description: "Add a final comment.",
			Fields: []render.HTML{
				ui.TextField(ui.TextFieldConfig{
					Name: "wd-comments", Label: "Comments", ID: "wd-comments",
					Value: values.Get("wd-comments"),
				}),
			},
		},
	}
}

// wizardDemoRadioGroup wraps ui.RadioGroup with a pre-checked option so Back
// navigation preserves the user's previous selection.
func wizardDemoRadioGroup(name, legend string, options []ui.RadioGroupOption, current string) render.HTML {
	dup := make([]ui.RadioGroupOption, len(options))
	for i, o := range options {
		dup[i] = o
		if o.Value == current {
			dup[i].Checked = true
		}
	}
	return ui.RadioGroup(ui.RadioGroupConfig{
		Name:    name,
		Legend:  legend,
		Options: dup,
	})
}
