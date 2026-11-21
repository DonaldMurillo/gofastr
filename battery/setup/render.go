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

func (r *Runner) buildStepForm(ctx context.Context, stepIdx int, steps []Step, fieldErrors, values map[string]string) render.HTML {
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

	currentStep := steps[stepIdx]
	var fields []render.HTML
	for _, f := range currentStep.Fields {
		inputType := "text"
		if f.Secret {
			inputType = "password"
		}
		inputID := "setup-field-" + f.Name
		inputValue := ""
		if !f.Secret {
			if submitted, ok := values[f.Name]; ok {
				inputValue = submitted
			} else if f.EnvVar != "" {
				inputValue = os.Getenv(f.EnvVar)
			}
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

	var alert render.HTML
	if stepErr, ok := fieldErrors["_step"]; ok && stepErr != "" {
		alert = ui.Notification(ui.NotificationConfig{
			Ctx:     ctx,
			Title:   pageStepFailed.Text(ctx),
			Body:    stepErr,
			Variant: ui.StatusDanger,
		})
	}
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

func (r *Runner) renderStep(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	stepIdx := r.currentStep
	steps := r.cfg.Steps
	r.mu.Unlock()

	if stepIdx >= len(steps) {
		r.renderIncomplete(req.Context(), w, pageIncompleteWiring.Text(req.Context()))
		return
	}
	body := r.buildStepForm(req.Context(), stepIdx, steps, nil, nil)
	r.writePage(req.Context(), w, body)
}

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

func (r *Runner) renderHop(w http.ResponseWriter, req *http.Request, target string) {
	ctx := req.Context()
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Refresh", "0;url="+target)
	r.writePage(ctx, w, ui.AuthCard(ui.AuthCardConfig{
		Title: r.cfg.Title.Text(ctx),
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(pageHopTitle.Text(ctx))),
			html.Link(html.LinkConfig{
				Href: target,
				Text: pageContinue.Text(ctx),
			}),
		),
	}))
}

func (r *Runner) renderStepWithErrors(w http.ResponseWriter, req *http.Request, stepIdx int, errors, values map[string]string) {
	body := r.buildStepForm(req.Context(), stepIdx, r.cfg.Steps, errors, values)
	r.writePage(req.Context(), w, body)
}

func (r *Runner) renderCompletionPage(w http.ResponseWriter, req *http.Request) {
	r.writePage(req.Context(), w, r.buildCompletionPage(req.Context()))
}

func (r *Runner) writePage(ctx context.Context, w http.ResponseWriter, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")

	comps := registry.Scan(string(body))

	cssLink := `<link rel="stylesheet" href="/__setup/style.css">`

	_ = comps

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
