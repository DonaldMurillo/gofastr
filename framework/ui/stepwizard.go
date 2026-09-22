package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FormStepWizard ─────────────────────────────────────────────────
//
// Multi-step form over headless.StepWizard: a rail of named steps, a
// validation summary the existing headless module focuses after a
// failed submit, and Back/Continue controls that submit the plain form
// with wizard_action=back|next. The server reads the action and
// re-renders with the updated CurrentStep.

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

	// Errors is an optional set of field-level errors for the current
	// step, the same shape ui.Form takes. When non-empty the wizard
	// renders a ValidationSummary between the step rail and the
	// step's fields, and marks the form (data-hui-form-errors) so the
	// headless behaviour module moves focus to the summary after a
	// failed submit — which requires ID, the summary's id being derived
	// from it. FieldErrors round-trips directly from a server-side
	// validation of the submitted step.
	Errors FieldErrors
	// Summary is a sentence that belongs to no one field, rendered in
	// the summary after the field errors. Empty means the framework
	// default when Errors is not.
	Summary string
	// FieldLabels, FieldIDs and FieldOrder are passed to the
	// ValidationSummary; see ValidationSummaryConfig.
	FieldLabels map[string]string
	FieldIDs    map[string]string
	FieldOrder  []string

	// Island, when set, makes each submit a region update: the same
	// form and controls, the answer swapped into the signal's region.
	Island headless.Island

	// ID is the form's id; required when Errors is set (the summary's
	// id is derived from it).
	ID string

	Class string

	// ExtraAttrs forwards additional attributes to the wizard's root
	// <form> element. Keys the component owns are dropped: class and
	// id (use Class / ID), data-fui-*, method, and action (both
	// validated by the primitive; the form posts wizard_action
	// through them).
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve i18n strings
	// (Back, Continue, Submit button labels, the rail's sentence).
	// When nil, context.Background() is used and English fallbacks are
	// returned.
	Ctx context.Context
}

// stepWizardClasses dresses headless.StepWizard's parts in this
// package's own vocabulary — the names the registered ui-step-wizard
// sheet matches. The rail's row (PartStepRow) carries no class: its
// marker and step text are visually hidden and the sheet draws the
// rail through the step-dot list items, so a class there would be a
// selector nothing owns.
var stepWizardClasses = headless.Classes{
	headless.PartRoot:     "fui-step-wizard",
	headless.PartSteps:    "fui-step-wizard__indicator",
	headless.PartStep:     "fui-step-wizard__step-dot",
	headless.PartMarker:   "fui-visually-hidden",
	headless.PartStepText: "fui-visually-hidden",
	headless.PartTitle:    "fui-step-wizard__heading",
	headless.PartDesc:     "fui-step-wizard__description",
	headless.PartBody:     "fui-step-wizard__fields",
	headless.PartActions:  "fui-step-wizard__actions",
	headless.PartPrev:     "fui-step-wizard__back",
	headless.PartNext:     "fui-step-wizard__next",
	headless.PartStatus:   "fui-visually-hidden",
}

// StepWizard renders a multi-step form with a progress indicator bar.
//
// Server-driven: each step is a full form submission. The server
// reads the "wizard_action" field (value "back" or "next") to
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

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	steps := make([]headless.WizardStep, len(cfg.Steps))
	for i, st := range cfg.Steps {
		steps[i] = headless.WizardStep{
			Heading:     st.Heading,
			Description: st.Description,
			Fields:      st.Fields,
		}
	}

	// The failed submit's summary, the same one ui.Form renders: above
	// the fields, focused on arrival by the existing headless module.
	var errors render.HTML
	if len(cfg.Errors) > 0 || cfg.Summary != "" {
		if cfg.ID == "" {
			panic("ui: StepWizard rendering Errors requires ID — the summary's id is derived from it (StepWizardConfig.ID + \"-errors\"), and two summaries on one page would share one title id")
		}
		general := cfg.Summary
		if general == "" {
			general = i18nui.T(ctx, i18nui.KeyFormErrorsSummary)
		}
		errors = ValidationSummary(ValidationSummaryConfig{
			ID:          cfg.ID + "-errors",
			Errors:      cfg.Errors,
			General:     general,
			FieldLabels: cfg.FieldLabels,
			FieldIDs:    cfg.FieldIDs,
			FieldOrder:  cfg.FieldOrder,
			Title:       i18nui.T(ctx, i18nui.KeyFormHasErrors),
			Ctx:         ctx,
		})
	}

	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": rootClass}}
	}

	return stepWizardStyle.WrapHTML(headless.StepWizard(headless.StepWizardProps{
		Steps:        steps,
		Current:      cfg.CurrentStep,
		Action:       cfg.Action,
		Method:       cfg.Method,
		HiddenFields: cfg.HiddenFields,
		Errors:       errors,
		Island:       cfg.Island,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "class", "id", "method", "action"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, stepWizardClasses))
}

// stepWizardStyle is registered in styles_components.go

func stepWizardCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-step-wizard"] {
  display: grid;
  gap: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-step-wizard"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__indicator {
  list-style: none;
  display: flex;
  gap: var(--spacing-xs, 2px);
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__step-dot {
  flex: 1;
  height: 4px;
  border-radius: 2px;
  background: var(--color-border, #E4E4E7);
  transition: background 150ms ease;
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__step-dot[data-state="done"],
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__step-dot[data-state="current"] {
  background: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__heading {
  margin: 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__fields {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__actions {
  display: flex;
  gap: var(--spacing-md, 8px);
  justify-content: flex-end;
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__back,
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__next {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: 40px;
  padding: 0 var(--spacing-lg, 16px);
  border-radius: var(--radii-md, 8px);
  font: inherit;
  font-weight: 500;
  cursor: pointer;
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__back {
  border: 1px solid var(--color-border, #E4E4E7);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__back:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__next {
  border: 1px solid var(--color-primary, #4F46E5);
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__next:hover {
  filter: brightness(1.05);
}
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__back:focus-visible,
[data-fui-comp="ui-step-wizard"] .fui-step-wizard__next:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
`
}
