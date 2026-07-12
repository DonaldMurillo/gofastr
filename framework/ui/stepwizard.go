package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FormStepWizard ─────────────────────────────────────────────────
//
// Multi-step form with a visual progress indicator. Server-driven:
// each "Continue" / "Back" click is a standard form POST. The server
// reads wizard_action=next|back to know the direction and re-renders
// with the updated CurrentStep.

// StepWizardStep is one step in the wizard.
type StepWizardStep struct {
	// Heading is the step heading. Required.
	Heading string
	// Description is optional supporting text below the heading.
	Description string
	// Fields are the form fields rendered for this step.
	Fields []render.HTML
}

// StepWizardConfig configures a multi-step form wizard.
type StepWizardConfig struct {
	// Steps is the ordered list of wizard steps. Required, min 1.
	Steps []StepWizardStep

	// CurrentStep is 0-indexed. The server sets this after each POST.
	CurrentStep int

	// Action is the form action URL. Required.
	Action string

	// Method defaults to "POST".
	Method string

	// HiddenFields are hidden inputs to carry forward between steps
	// (e.g. previously entered data).
	HiddenFields []render.HTML

	Class string

	// Ctx carries the per-request context used to resolve i18n strings
	// (Back, Continue, Submit button labels). When nil, context.Background()
	// is used and English fallbacks are returned — preserving today's behaviour.
	Ctx context.Context
}

// StepWizard renders a multi-step form with a progress indicator bar.
//
// Server-driven: each step is a full form submission. The server
// reads the "wizard_action" field (value "next" or "back") to
// determine direction and re-renders with the updated CurrentStep.
func StepWizard(cfg StepWizardConfig) render.HTML {
	if len(cfg.Steps) == 0 {
		panic("ui: StepWizard requires at least one Step")
	}
	if cfg.Action == "" {
		panic("ui: StepWizard requires Action")
	}
	if cfg.CurrentStep < 0 || cfg.CurrentStep >= len(cfg.Steps) {
		panic("ui: StepWizard CurrentStep out of range")
	}

	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	if method != "GET" && method != "POST" {
		panic("ui: StepWizard Method must be GET or POST, got " + method)
	}

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	cls := "ui-step-wizard"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	children := []render.HTML{}

	// 1. Step indicator bar (visual dots/segments).
	children = append(children, renderStepIndicator(ctx, cfg.Steps, cfg.CurrentStep))

	// 2. Current step content wrapped in a section.
	step := cfg.Steps[cfg.CurrentStep]
	stepContent := renderStepContent(step, cfg.CurrentStep, len(cfg.Steps))
	children = append(children, stepContent)

	// 3. Hidden fields for carrying state.
	children = append(children, cfg.HiddenFields...)

	// 4. Navigation buttons.
	children = append(children, renderStepActions(ctx, cfg.CurrentStep, len(cfg.Steps)))

	return stepWizardStyle.WrapHTML(html.Form(html.FormConfig{
		Method:     method,
		Action:     cfg.Action,
		Class:      cls,
		ExtraAttrs: html.Attrs{"data-fui-comp": "ui-step-wizard"},
	}, children...))
}

// renderStepIndicator builds the visual progress bar.
func renderStepIndicator(ctx context.Context, steps []StepWizardStep, current int) render.HTML {
	dots := make([]render.HTML, 0, len(steps))
	for i := range steps {
		dotCls := "ui-step-wizard__step-dot"
		if i < current {
			dotCls += " is-completed"
		} else if i == current {
			dotCls += " is-current"
		}
		dotAttrs := map[string]string{
			"class": dotCls,
			"role":  "listitem",
		}
		if i == current {
			dotAttrs["aria-current"] = "step"
		}
		dotAttrs["aria-label"] = i18nui.TVars(ctx, i18nui.KeyStepWizardStep, map[string]string{"step": strconv.Itoa(i + 1), "heading": steps[i].Heading})
		dots = append(dots, render.Tag("div", dotAttrs))
	}
	return render.Tag("div", map[string]string{
		"class":      "ui-step-wizard__indicator",
		"role":       "list",
		"aria-label": i18nui.TVars(ctx, i18nui.KeyStepWizardStepOf, map[string]string{"step": strconv.Itoa(current + 1), "total": strconv.Itoa(len(steps))}),
	}, dots...)
}

// renderStepContent builds the current step's fields.
func renderStepContent(step StepWizardStep, current, total int) render.HTML {
	content := []render.HTML{}

	if step.Heading != "" {
		content = append(content, html.Heading(html.HeadingConfig{
			Level: 2,
			Class: "ui-step-wizard__heading",
		}, render.Text(step.Heading)))
	}
	if step.Description != "" {
		content = append(content, html.Paragraph(html.TextConfig{
			Class: "ui-step-wizard__description",
		}, render.Text(step.Description)))
	}
	if len(step.Fields) > 0 {
		content = append(content, render.Tag("div", map[string]string{
			"class": "ui-step-wizard__fields",
		}, step.Fields...))
	}

	return render.Tag("div", map[string]string{
		"class": "ui-step-wizard__content",
	}, content...)
}

// renderStepActions builds the navigation buttons.
func renderStepActions(ctx context.Context, current, total int) render.HTML {
	btns := []render.HTML{}

	// Back button (not on first step).
	if current > 0 {
		btns = append(btns, html.Button(html.ButtonConfig{
			Label: i18nui.T(ctx, i18nui.KeyStepWizardBack),
			Type:  "submit",
			Class: "ui-button ui-button--secondary",
			ExtraAttrs: html.Attrs{
				"name":  "wizard_action",
				"value": "back",
			},
		}))
	}

	// Continue or Submit button.
	isLast := current == total-1
	if isLast {
		btns = append(btns, html.Button(html.ButtonConfig{
			Label: i18nui.T(ctx, i18nui.KeyStepWizardSubmit),
			Type:  "submit",
			Class: "ui-button",
			ExtraAttrs: html.Attrs{
				"name":  "wizard_action",
				"value": "next",
			},
		}))
	} else {
		btns = append(btns, html.Button(html.ButtonConfig{
			Label: i18nui.T(ctx, i18nui.KeyStepWizardNext),
			Type:  "submit",
			Class: "ui-button",
			ExtraAttrs: html.Attrs{
				"name":  "wizard_action",
				"value": "next",
			},
		}))
	}

	return render.Tag("div", map[string]string{
		"class": "ui-step-wizard__actions",
	}, btns...)
}

// stepWizardStyle is registered in styles_components.go

func stepWizardCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-step-wizard"] {
  display: grid;
  gap: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__indicator {
  display: flex;
  gap: var(--spacing-xs, 4px);
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__step-dot {
  flex: 1;
  height: 4px;
  border-radius: 2px;
  background: var(--color-border, #E4E4E7);
  transition: background 150ms ease;
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__step-dot.is-completed {
  background: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__step-dot.is-current {
  background: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__content {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__heading {
  margin: 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__fields {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-step-wizard"] .ui-step-wizard__actions {
  display: flex;
  gap: var(--spacing-md, 8px);
  justify-content: flex-end;
}
`
}
