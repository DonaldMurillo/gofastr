package setup

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// buildStepForm composes the wizard body for one step: a progress
// indicator (ProgressSteps) + the form (AuthCard wrapping Form +
// FormField rows). Zero bespoke CSS, zero hand-rolled structural markup —
// everything is typed framework/ui composition.
func (r *Runner) buildStepForm(stepIdx int, steps []Step, fieldErrors map[string]string) render.HTML {
	// 1. Progress indicator.
	progressSteps := make([]ui.ProgressStep, len(steps))
	for i, s := range steps {
		status := ui.ProgressStepUpcoming
		switch {
		case i < stepIdx:
			status = ui.ProgressStepComplete
		case i == stepIdx:
			status = ui.ProgressStepCurrent
		}
		progressSteps[i] = ui.ProgressStep{
			Label:  s.Name,
			Status: status,
		}
	}
	progress := ui.ProgressSteps(ui.ProgressStepsConfig{
		Steps:       progressSteps,
		Orientation: ui.ProgressStepsVertical,
	})

	// 2. Form fields for the current step.
	currentStep := steps[stepIdx]
	var fields []render.HTML
	for _, f := range currentStep.Fields {
		inputType := "text"
		if f.Secret {
			inputType = "password"
		}
		inputID := "setup-field-" + f.Name
		inputValue := ""
		// Pre-fill from env (secrets excepted).
		if !f.Secret && f.EnvVar != "" {
			inputValue = os.Getenv(f.EnvVar)
		}
		fields = append(fields, ui.FormFieldFor(ui.FieldErrors(fieldErrors), f.Name, ui.FormFieldConfig{
			Label:    f.Label,
			For:      inputID,
			Required: true,
			Input: html.Input(html.InputConfig{
				Type:  inputType,
				Name:  f.Name,
				ID:    inputID,
				Value: inputValue,
			}),
		}))
	}

	// 3. Step error (non-field specific, e.g. step.Run failure).
	var alert render.HTML
	if stepErr, ok := fieldErrors["_step"]; ok && stepErr != "" {
		alert = ui.Notification(ui.NotificationConfig{
			Title:   "Step failed",
			Body:    stepErr,
			Variant: ui.StatusDanger,
		})
	}
	// Force-mode warning: setup already completed but operator re-entered.
	if isForceMode() {
		forceAlert := ui.Notification(ui.NotificationConfig{
			Title:   "Setup already completed",
			Body:    "You are in rescue mode (GOFASTR_SETUP=force). Re-running setup steps.",
			Variant: ui.StatusWarning,
		})
		if alert != "" {
			alert = render.HTML(string(alert) + string(forceAlert))
		} else {
			alert = forceAlert
		}
	}

	submitLabel := "Continue"
	if stepIdx == len(steps)-1 {
		submitLabel = "Complete Setup"
	}

	formBody := ui.Form(ui.FormConfig{
		Action:      "/setup",
		Method:      "POST",
		SubmitLabel: submitLabel,
	}, fields...)

	// AuthCard wraps everything in a centered, constrained card.
	return ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title,
		Alert: alert,
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			progress,
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(currentStep.Name)),
			formBody,
		),
	})
}

// buildCompletionPage renders the "all done" body.
func (r *Runner) buildCompletionPage() render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title,
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Setup Complete")),
			html.Paragraph(html.TextConfig{}, render.Text("Your application is ready.")),
			html.Link(html.LinkConfig{
				Href: "/",
				Text: "Go to the app →",
			}),
		),
	})
}

// renderStep writes the current step's wizard page.
func (r *Runner) renderStep(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	stepIdx := r.currentStep
	steps := r.cfg.Steps
	r.mu.Unlock()

	if stepIdx >= len(steps) {
		// Steps exhausted but this request got PAST handleSetup's
		// completeAndSwap gate — so Complete is still false (or erroring).
		// Never claim completion here; explain instead.
		r.renderIncomplete(w, "Every step ran, but setup still reports incomplete. "+
			"The Complete predicate does not observe the steps' writes — e.g. AdminStep configured "+
			"with a different users table than the auth store writes to. Fix the wiring and restart.")
		return
	}
	body := r.buildStepForm(stepIdx, steps, nil)
	r.writePage(w, body)
}

// renderIncomplete writes an honest "steps ran, but setup is not complete"
// page: a danger notification with the reason, never the completion page —
// the app is still serving 503s, and saying "ready" would strand the
// operator on a lie.
func (r *Runner) renderIncomplete(w http.ResponseWriter, reason string) {
	body := ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title,
		Alert: ui.Notification(ui.NotificationConfig{
			Title:   "Setup is not complete",
			Body:    reason,
			Variant: ui.StatusDanger,
		}),
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Setup incomplete")),
			html.Link(html.LinkConfig{
				Href: "/setup",
				Text: "Retry",
			}),
		),
	})
	r.writePage(w, body)
}

// renderStepWithErrors writes the step page with validation errors.
func (r *Runner) renderStepWithErrors(w http.ResponseWriter, _ *http.Request, stepIdx int, errors map[string]string) {
	body := r.buildStepForm(stepIdx, r.cfg.Steps, errors)
	r.writePage(w, body)
}

// renderCompletionPage writes the "all done" page.
func (r *Runner) renderCompletionPage(w http.ResponseWriter, _ *http.Request) {
	r.writePage(w, r.buildCompletionPage())
}

// writePage emits a complete standalone HTML document. Mirrors the
// battery/admin pattern: theme CSS tokens + registered component CSS
// collected via registry.Scan, so every framework/ui component used in
// the body is styled without any bespoke stylesheet.
func (r *Runner) writePage(w http.ResponseWriter, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	comps := registry.Scan(string(body))

	// Build <link> tags for the single combined CSS endpoint.
	cssLink := `<link rel="stylesheet" href="/__setup/style.css">`

	_ = comps // used by serveCSS via registry.Scan at request time

	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  %s
</head>
<body>
%s
</body>
</html>`, r.cfg.Title, cssLink, body)
}

// serveCSS emits the combined stylesheet: theme :root tokens + the CSS
// for every framework/ui component used in the wizard body. Registered
// components are scoped to [data-fui-comp="..."] so they don't collide
// with the host app's styles.
func (r *Runner) serveCSS(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	theme := r.cfg.theme()
	var b strings.Builder
	b.WriteString(theme.CSSCustomProperties())
	b.WriteString("\n")
	for _, e := range registry.All() {
		b.WriteString(e.CSSFor(theme))
		b.WriteString("\n")
	}
	_, _ = fmt.Fprint(w, b.String())
}
