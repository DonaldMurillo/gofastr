package main

import (
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// =============================================================================
// Wizard E2E demo
// =============================================================================
//
// Self-contained 3-step wizard at /components/forms/wizard-demo with a
// real round-trip server: Continue/Back post the form, the handler
// accumulates entered values via hidden fields, the wizard re-renders
// at the new step, and the final Submit captures the payload to a
// package-local var the wizard E2E tests assert against.
//
// The page is intentionally minimal — no site chrome, no SPA shell —
// because the form submits to itself with method=POST and the response
// must be served as the next full page. The /components/forms screen
// already proves the wizard renders inside the site chrome; this page
// proves the multi-step round-trip works end-to-end.

const wizardDemoPath = "/components/forms/wizard-demo"

// wizardDemoFields lists every input the wizard collects across its
// three steps. Order matters: any field not on the *current* step is
// carried forward via a hidden input keyed by the same name so the
// server-side accumulator picks it up on the next POST.
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

// wizardDemoLast returns the most recent form payload captured by
// the wizard demo handler, or nil if nothing has been submitted.
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

// WizardDemoHandler serves the 3-step wizard at /components/forms/wizard-demo.
//
// GET → render step 0 with empty values.
// POST → read wizard_action ("next" | "back") and _step (the step the
//
//	form was submitted from), clamp the next step into [0, len-1],
//	and re-render. On the final-step "next" the payload is recorded
//	and a confirmation page is shown instead.
func WizardDemoHandler(w http.ResponseWriter, r *http.Request) {
	values := url.Values{}
	action := ""
	submittedStep := 0

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
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

	totalSteps := 3
	current := submittedStep

	switch action {
	case "next":
		// Final-step Next means Submit — capture and confirm.
		// Guard against overflow: a stale POST with _step=last must
		// NOT push the wizard past the last index.
		if submittedStep >= totalSteps-1 {
			wizardDemoStore(values)
			render.RespondHTML(w, wizardDemoConfirmation(values))
			return
		}
		current = submittedStep + 1
	case "back":
		current = submittedStep - 1
	default:
		// GET → start at step 0.
		current = 0
	}

	// Clamp into range — protects against any direct/manipulated POSTs.
	if current < 0 {
		current = 0
	}
	if current >= totalSteps {
		current = totalSteps - 1
	}

	render.RespondHTML(w, wizardDemoPage(current, values))
}

// wizardDemoPage builds a self-contained HTML doc rendering the
// wizard at the given step with values pre-filled.
func wizardDemoPage(current int, values url.Values) render.HTML {
	wiz := ui.StepWizard(ui.StepWizardConfig{
		Action:       wizardDemoPath,
		Method:       "POST",
		CurrentStep:  current,
		HiddenFields: wizardDemoHiddenCarry(current, values),
		Steps:        wizardDemoSteps(values),
	})

	body := render.Tag("body", nil,
		render.Tag("h1", nil, render.Text("Wizard demo")),
		wiz,
	)
	doc := render.HTML("<!doctype html>") +
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.Tag("title", nil, render.Text("Wizard demo")),
			),
			body,
		)
	return doc
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
	doc := render.HTML("<!doctype html>") +
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.Tag("title", nil, render.Text("Wizard submitted")),
			),
			body,
		)
	return doc
}

// wizardDemoHiddenCarry emits hidden inputs for every field NOT visible
// on the current step, plus a _step marker so the handler knows which
// step the user just submitted from.
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
	// _step lets the handler tell next-from-2 vs next-from-0 apart.
	out = append(out, render.VoidTag("input", map[string]string{
		"type":  "hidden",
		"name":  "_step",
		"value": strconv.Itoa(current),
	}))
	return out
}

func wizardDemoSteps(values url.Values) []ui.StepWizardStep {
	nameAttrs := html.Attrs{}
	if v := values.Get("wd-name"); v != "" {
		nameAttrs["value"] = v
	}
	emailAttrs := html.Attrs{}
	if v := values.Get("wd-email"); v != "" {
		emailAttrs["value"] = v
	}
	commentsAttrs := html.Attrs{}
	if v := values.Get("wd-comments"); v != "" {
		commentsAttrs["value"] = v
	}

	themeLight := []ui.RadioGroupOption{
		{Label: "Light", Value: "light"},
		{Label: "Dark", Value: "dark"},
	}
	// Pre-check the previously-selected theme so Back preserves state.
	currentTheme := values.Get("wd-theme")

	return []ui.StepWizardStep{
		{
			Heading:     "Personal info",
			Description: "Your basic details.",
			Fields: []render.HTML{
				ui.FormField(ui.FormFieldConfig{
					Label: "Full name", For: "wd-name", Required: true,
					Input: html.Input(html.InputConfig{
						Type: "text", Name: "wd-name", ID: "wd-name", ExtraAttrs: nameAttrs,
					}),
				}),
				ui.FormField(ui.FormFieldConfig{
					Label: "Email", For: "wd-email", Required: true,
					Input: html.Input(html.InputConfig{
						Type: "email", Name: "wd-email", ID: "wd-email", ExtraAttrs: emailAttrs,
					}),
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
				ui.FormField(ui.FormFieldConfig{
					Label: "Comments", For: "wd-comments",
					Input: html.Input(html.InputConfig{
						Type: "text", Name: "wd-comments", ID: "wd-comments", ExtraAttrs: commentsAttrs,
					}),
				}),
			},
		},
	}
}

// wizardDemoRadioGroup wraps ui.RadioGroup with a pre-checked option
// so Back navigation preserves the user's previous selection.
func wizardDemoRadioGroup(name, legend string, options []ui.RadioGroupOption, current string) render.HTML {
	// Make a copy of the options with `Checked` set on the matching one.
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
