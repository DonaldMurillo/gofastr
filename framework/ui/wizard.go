package ui

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// WizardStep defines a single step in a form wizard.
type WizardStep struct {
	Title       string
	Description string
	Content     render.HTML
	ValidateRPC string
}

// WizardConfig configures a multi-step form wizard.
type WizardConfig struct {
	Steps       []WizardStep
	ID          string
	Name        string
	CurrentStep int
	SubmitLabel string
	NextLabel   string
	BackLabel   string
	RPCPath     string
}

// Wizard renders a multi-step form wizard.
func Wizard(cfg WizardConfig) render.HTML {
	if cfg.ID == "" {
		cfg.ID = autoID("wiz")
	}
	if cfg.SubmitLabel == "" {
		cfg.SubmitLabel = "Submit"
	}
	if cfg.NextLabel == "" {
		cfg.NextLabel = "Next"
	}
	if cfg.BackLabel == "" {
		cfg.BackLabel = "Back"
	}
	if cfg.CurrentStep < 0 {
		cfg.CurrentStep = 0
	}
	if cfg.CurrentStep >= len(cfg.Steps) {
		cfg.CurrentStep = len(cfg.Steps) - 1
	}

	var children []render.HTML

	children = append(children, wizardStepIndicator(cfg))

	for i, step := range cfg.Steps {
		attrs := map[string]string{
			"class":      "ui-wizard-step",
			"data-step":  strconv.Itoa(i),
			"role":       "tabpanel",
			"aria-label": step.Title,
		}
		if i != cfg.CurrentStep {
			attrs["hidden"] = ""
		}
		children = append(children, html.Div(html.DivConfig{Attrs: attrs}, step.Content))
	}

	children = append(children, html.Input(html.InputConfig{
		Type:  "hidden",
		Name:  cfg.Name + "[_step]",
		Class: "ui-wizard-current-step",
		Attrs: map[string]string{
			"value": strconv.Itoa(cfg.CurrentStep),
		},
	}))

	children = append(children, wizardNav(cfg))

	return wizardStyle.WrapHTML(html.Form(html.FormConfig{
		Method: "POST",
		Class:  "ui-wizard",
		Attrs: map[string]string{
			"id":                cfg.ID,
			"data-wizard-steps": strconv.Itoa(len(cfg.Steps)),
		},
	}, children...))
}

func wizardStepIndicator(cfg WizardConfig) render.HTML {
	var items []render.HTML
	for i, step := range cfg.Steps {
		state := "upcoming"
		ariaCurrent := ""
		if i < cfg.CurrentStep {
			state = "complete"
		} else if i == cfg.CurrentStep {
			state = "current"
			ariaCurrent = "step"
		}
		attrs := map[string]string{
			"class":        "ui-wizard-step-indicator ui-wizard-step-" + state,
			"aria-current": ariaCurrent,
		}
		stepChildren := []render.HTML{
			html.Span(html.TextConfig{Class: "ui-wizard-step-number"}, render.HTML(strconv.Itoa(i+1))),
			html.Span(html.TextConfig{Class: "ui-wizard-step-title"}, render.HTML(step.Title)),
		}
		items = append(items, html.ListItem(html.ListItemConfig{Attrs: attrs}, stepChildren...))
	}
	return html.OrderedList(html.ListConfig{
		Class: "ui-wizard-steps",
		Attrs: map[string]string{"role": "list"},
	}, items...)
}

func wizardNav(cfg WizardConfig) render.HTML {
	var buttons []render.HTML

	if cfg.CurrentStep > 0 {
		backAttrs := map[string]string{"type": "button", "class": "ui-wizard-back"}
		if cfg.RPCPath != "" {
			backAttrs["data-fui-rpc"] = fmt.Sprintf("%s?direction=back&step=%d", cfg.RPCPath, cfg.CurrentStep-1)
			backAttrs["data-fui-rpc-method"] = "POST"
			backAttrs["data-fui-rpc-signal"] = cfg.ID
		}
		buttons = append(buttons, html.Button(html.ButtonConfig{
			Label: cfg.BackLabel,
			Attrs: backAttrs,
		}))
	}

	if cfg.CurrentStep < len(cfg.Steps)-1 {
		nextAttrs := map[string]string{"type": "button", "class": "ui-wizard-next"}
		if cfg.RPCPath != "" {
			nextAttrs["data-fui-rpc"] = fmt.Sprintf("%s?direction=next&step=%d", cfg.RPCPath, cfg.CurrentStep+1)
			nextAttrs["data-fui-rpc-method"] = "POST"
			nextAttrs["data-fui-rpc-signal"] = cfg.ID
		}
		buttons = append(buttons, html.Button(html.ButtonConfig{
			Label: cfg.NextLabel,
			Attrs: nextAttrs,
		}))
	} else {
		buttons = append(buttons, html.Button(html.ButtonConfig{
			Type:  "submit",
			Label: cfg.SubmitLabel,
			Class: "ui-wizard-submit",
		}))
	}

	return html.Div(html.DivConfig{Class: "ui-wizard-nav"}, buttons...)
}

var wizardStyle = registry.RegisterStyle("ui-wizard", wizardCSS)

func wizardCSS(t style.Theme) string {
	return `.ui-wizard { display: flex; flex-direction: column; gap: var(--spacing-lg); }
.ui-wizard-steps { display: flex; gap: var(--spacing-sm); list-style: none; padding: 0; margin: 0; }
.ui-wizard-step-indicator { display: flex; align-items: center; gap: var(--spacing-xs); padding: var(--spacing-xs) var(--spacing-sm); border-radius: 9999px; font-size: var(--text-sm); }
.ui-wizard-step-number { display: flex; align-items: center; justify-content: center; width: 1.5rem; height: 1.5rem; border-radius: 9999px; background: var(--color-surface-secondary); font-size: var(--text-xs); }
.ui-wizard-step-current .ui-wizard-step-number { background: var(--color-primary); color: var(--color-primary-fg); }
.ui-wizard-step-complete .ui-wizard-step-number { background: var(--color-success); color: white; }
.ui-wizard-nav { display: flex; justify-content: space-between; gap: var(--spacing-sm); }`
}
