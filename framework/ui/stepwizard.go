package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// StepWizardStep is one step in the wizard.
type StepWizardStep struct {
	Heading     string // required
	Description string
	Fields      []render.HTML
}

// StepWizardConfig configures a multi-step form wizard.
type StepWizardConfig struct {
	// Steps are the wizard's steps, at least one.
	Steps []StepWizardStep
	// CurrentStep is the 0-indexed step the server is rendering.
	CurrentStep int
	// Action is the form action URL (required).
	Action string
	// Method defaults to "POST".
	Method string
	// HiddenFields carry earlier steps' values forward.
	HiddenFields []render.HTML
	// Errors renders a ValidationSummary above the step's fields that
	// is focused after a failed submit; it needs ID.
	Errors FieldErrors
	// Summary is the summary's sentence for no one field. Default the framework's.
	Summary string
	// FieldLabels, FieldIDs and FieldOrder are passed to the ValidationSummary.
	FieldLabels map[string]string
	FieldIDs    map[string]string
	FieldOrder  []string
	// Island, when set, makes each submit a region update.
	Island headless.Island
	// ID is the form's id; required when Errors or Summary is set.
	ID    string
	Class string
	// ExtraAttrs lands on the <form>; class, id, data-cui-*, method and action are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the wizard's words are said in.
	Ctx context.Context
}

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

// StepWizard renders a multi-step form whose Back and Continue submit
// wizard_action=back or next; the server re-renders with the new
// CurrentStep. It panics without steps or Action, or on an out-of-range CurrentStep.
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

	var errors render.HTML
	if len(cfg.Errors) > 0 || cfg.Summary != "" {
		errors = formErrorsSummary(ctx, "StepWizard", cfg.ID, cfg.Summary, cfg.Errors, cfg.FieldLabels, cfg.FieldIDs, cfg.FieldOrder)
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

func stepWizardCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-step-wizard"] {
  display: grid;
  gap: var(--spacing-lg, 16px);
}
[data-cui-comp="ui-step-wizard"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__indicator {
  list-style: none;
  display: flex;
  gap: var(--spacing-xs, 2px);
  margin: 0;
  padding: 0;
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__step-dot {
  flex: 1;
  height: var(--ui-step-wizard-step-dot-height, 4px);
  border-radius: calc(var(--radii-sm, 6px) / 3);
  background: var(--color-border, #E4E4E7);
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__step-dot[data-state="done"],
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__step-dot[data-state="current"] {
  background: var(--color-primary, #18181B);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__heading {
  margin: 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__fields {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__actions {
  display: flex;
  gap: var(--spacing-md, 8px);
  justify-content: flex-end;
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__back,
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__next {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--ui-step-wizard-action-min-height, 40px);
  padding: 0 var(--spacing-lg, 16px);
  border-radius: var(--radii-md, 8px);
  font: inherit;
  font-weight: var(--font-weight-medium);
  cursor: pointer;
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__back {
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__back:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__next {
  border: var(--stroke-thin, 1px) solid var(--color-primary, #18181B);
  background: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__next:hover {
  filter: brightness(1.05);
}
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__back:focus-visible,
[data-cui-comp="ui-step-wizard"] .fui-step-wizard__next:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(var(--stroke-focus-offset, 2px) / 2);
}
`
}
