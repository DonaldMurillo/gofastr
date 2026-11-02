package setup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// buildStepForm composes the wizard body for one step: a progress
// indicator (ProgressSteps) + the form (AuthCard wrapping Form +
// FormField rows). Zero bespoke CSS, zero hand-rolled structural markup,
// everything is typed framework/ui composition.
func (r *Runner) buildStepForm(ctx context.Context, stepIdx int, steps []Step, fieldErrors map[string]string) render.HTML {
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
			Label:  s.title(ctx),
			Status: status,
		}
	}
	progress := ui.ProgressSteps(ui.ProgressStepsConfig{
		Ctx:         ctx,
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
			Label:    f.label(ctx),
			For:      inputID,
			Required: true,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{
					Field: c, Type: inputType, Name: f.Name, Value: inputValue,
				})
			},
		}))
	}

	// 3. Step error (non-field specific, e.g. step.Run failure).
	var alert render.HTML
	if stepErr, ok := fieldErrors["_step"]; ok && stepErr != "" {
		alert = ui.Notification(ui.NotificationConfig{
			Ctx:     ctx,
			Title:   pageStepFailed.Text(ctx),
			Body:    stepErr,
			Variant: ui.StatusDanger,
		})
	}
	// Force-mode warning: setup already completed but operator re-entered.
	if isForceMode() {
		forceAlert := ui.Notification(ui.NotificationConfig{
			Ctx:     ctx,
			Title:   pageForceTitle.Text(ctx),
			Body:    pageForceBody.Text(ctx),
			Variant: ui.StatusWarning,
		})
		if alert != "" {
			//gofastr:allow(GOFASTR1403) both halves are ui.Notification output, already markup
			alert = render.HTML(string(alert) + string(forceAlert))
		} else {
			alert = forceAlert
		}
	}

	submitLabel := pageContinue.Text(ctx)
	if stepIdx == len(steps)-1 {
		submitLabel = pageCompleteSetup.Text(ctx)
	}

	formBody := ui.Form(ui.FormConfig{
		Ctx:         ctx,
		Action:      "/setup",
		Method:      "POST",
		SubmitLabel: submitLabel,
	}, fields...)

	// AuthCard wraps everything in a centered, constrained card.
	return ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title.Text(ctx),
		Alert: alert,
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			progress,
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(currentStep.title(ctx))),
			formBody,
		),
	})
}

// buildCompletionPage renders the "all done" body.
func (r *Runner) buildCompletionPage(ctx context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title.Text(ctx),
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(pageCompleteTitle.Text(ctx))),
			html.Paragraph(html.TextConfig{}, render.Text(pageCompleteBody.Text(ctx))),
			html.Link(html.LinkConfig{
				Href: "/",
				Text: pageGoToApp.Text(ctx),
			}),
		),
	})
}

// renderStep writes the current step's wizard page.
func (r *Runner) renderStep(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	stepIdx := r.currentStep
	steps := r.cfg.Steps
	r.mu.Unlock()

	if stepIdx >= len(steps) {
		// Steps exhausted but this request got PAST handleSetup's
		// completeAndSwap gate, so Complete is still false (or erroring).
		// Never claim completion here; explain instead.
		r.renderIncomplete(req.Context(), w, pageIncompleteWiring.Text(req.Context()))
		return
	}
	body := r.buildStepForm(req.Context(), stepIdx, steps, nil)
	r.writePage(req.Context(), w, body)
}

// renderIncomplete writes an honest "steps ran, but setup is not complete"
// page: a danger notification with the reason, never the completion page,
// the app is still serving 503s, and saying "ready" would strand the
// operator on a lie.
func (r *Runner) renderIncomplete(ctx context.Context, w http.ResponseWriter, reason string) {
	body := ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title.Text(ctx),
		Alert: ui.Notification(ui.NotificationConfig{
			Ctx:     ctx,
			Title:   pageNotCompleteTitle.Text(ctx),
			Body:    reason,
			Variant: ui.StatusDanger,
		}),
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(pageIncompleteHeading.Text(ctx))),
			html.Link(html.LinkConfig{
				Href: "/setup",
				Text: pageRetry.Text(ctx),
			}),
		),
	})
	r.writePage(ctx, w, body)
}

// renderStepWithErrors writes the step page with validation errors.
func (r *Runner) renderStepWithErrors(w http.ResponseWriter, req *http.Request, stepIdx int, errors map[string]string) {
	body := r.buildStepForm(req.Context(), stepIdx, r.cfg.Steps, errors)
	r.writePage(req.Context(), w, body)
}

// renderCompletionPage writes the "all done" page.
func (r *Runner) renderCompletionPage(w http.ResponseWriter, req *http.Request) {
	r.writePage(req.Context(), w, r.buildCompletionPage(req.Context()))
}

// writePage emits a complete standalone HTML document. Mirrors the
// battery/admin pattern: theme CSS tokens + registered component CSS
// collected via registry.Scan, so every framework/ui component used in
// the body is styled without any bespoke stylesheet.
func (r *Runner) writePage(ctx context.Context, w http.ResponseWriter, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	comps := registry.Scan(string(body))

	// Build <link> tags for the single combined CSS endpoint.
	cssLink := `<link rel="stylesheet" href="/__setup/style.css">`

	_ = comps // used by serveCSS via registry.Scan at request time

	// The wizard speaks its phrases in the request's locale, so the
	// document declares that language and its direction.
	lang := i18n.DocLang(ctx)
	fmt.Fprintf(w, `<!doctype html>
<html dir="%s" lang="%s">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  %s
</head>
<body>
%s
</body>
</html>`, i18n.Dir(lang), render.Escape(lang), render.Escape(r.cfg.Title.Text(ctx)), cssLink, body)
}

// serveCSS emits the combined stylesheet: theme :root tokens + the CSS
// for every framework/ui component used in the wizard body. Registered
// components are scoped to [data-cui-comp="..."] so they don't collide
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
	_, _ = fmt.Fprint(w, minify.ServedCSS(b.String()))
}
