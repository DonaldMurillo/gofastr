package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ProgressSteps ──────────────────────────────────────────────────
//
// Step indicator showing current + completed + upcoming steps in a
// linear flow. headless.Steps carries the contract — the ordered
// list, the per-step data-state, aria-current on the current step,
// and the optional link back on a completed step. This adapter adds
// the typed status vocabulary, the orientation modifier and the nav
// landmark wrapper with its label.

// ProgressStepStatus is the rendered state of a single step.
type ProgressStepStatus string

const (
	ProgressStepUpcoming ProgressStepStatus = "" // default
	ProgressStepCurrent  ProgressStepStatus = "current"
	ProgressStepComplete ProgressStepStatus = "complete"
)

// ProgressStepsOrientation chooses horizontal (default) or vertical
// layout.
type ProgressStepsOrientation string

const (
	ProgressStepsHorizontal ProgressStepsOrientation = ""
	ProgressStepsVertical   ProgressStepsOrientation = "vertical"
)

// ProgressStep is one entry in the indicator.
type ProgressStep struct {
	// Label is the step name (required, e.g. "Account").
	Label string
	// Hint is the optional supporting line below the label.
	Hint string
	// Status picks the visual state. Defaults to ProgressStepUpcoming.
	Status ProgressStepStatus
	// Href, when set on a complete step, makes the step a link the
	// user can click to navigate back. Upcoming steps ignore Href.
	Href string
}

// ProgressStepsConfig configures a step indicator.
type ProgressStepsConfig struct {
	// Steps are the entries in order. Required (≥1).
	Steps []ProgressStep
	// Orientation defaults to horizontal.
	Orientation ProgressStepsOrientation
	// Label is the optional aria-label for the wrapping nav. Defaults
	// to the reader's "Progress".
	Label string
	// Ctx carries the per-request context used to resolve the
	// aria-label. When nil, English fallbacks apply.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <nav> root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-* and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// progressStepsClasses dresses headless.Steps' parts under the
// wrapper the adapter draws.
var progressStepsClasses = headless.Classes{
	headless.PartRoot:     "fui-progress-steps__list",
	headless.PartStep:     "fui-progress-steps__item",
	headless.PartStepRow:  "fui-progress-steps__row",
	headless.PartMarker:   "fui-progress-steps__marker",
	headless.PartStepText: "fui-progress-steps__text",
	headless.PartLabel:    "fui-progress-steps__label",
	headless.PartStepHint: "fui-progress-steps__hint",
}

// ProgressSteps renders a step indicator on headless.Steps. A step
// with an explicit Status keeps it (the derivation from Current
// cannot express "a later step finished while an earlier one is
// open"); the rest derive from the last current step.
func ProgressSteps(cfg ProgressStepsConfig) render.HTML {
	if len(cfg.Steps) == 0 {
		panic("ui: ProgressSteps requires at least one Step")
	}
	switch cfg.Orientation {
	case ProgressStepsHorizontal, ProgressStepsVertical:
	default:
		panic("ui: ProgressSteps unknown Orientation " + string(cfg.Orientation) +
			`. Pick one of: "" (horizontal), vertical`)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyProgressLabel)
	}
	navClass := cfg.Class
	if cfg.Orientation == ProgressStepsVertical {
		navClass = joinNonEmpty("fui-progress-steps--vertical", navClass)
	}
	navAttrs := headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label")
	if navAttrs == nil {
		navAttrs = html.Attrs{}
	}
	navAttrs["class"] = joinNonEmpty("fui-progress-steps", navClass)
	navAttrs["aria-label"] = label
	if cfg.ID != "" {
		navAttrs["id"] = cfg.ID
	}

	current := 0
	steps := make([]headless.Step, len(cfg.Steps))
	for i, s := range cfg.Steps {
		if s.Label == "" {
			panic("ui: ProgressSteps step requires Label")
		}
		state := ""
		switch s.Status {
		case ProgressStepUpcoming:
		case ProgressStepCurrent:
			state = "current"
		case ProgressStepComplete:
			state = "done"
		default:
			panic("ui: ProgressSteps step unknown Status " + string(s.Status) +
				`. Pick one of: "" (upcoming), current, complete`)
		}
		if s.Status == ProgressStepCurrent {
			current = i + 1
		}
		var marker render.HTML
		if s.Status == ProgressStepComplete {
			// The done glyph is this package's check icon; the
			// primitive's own default is a text ✓.
			marker = render.HTML(progressStepsCheckIcon())
		}
		href := ""
		if s.Status == ProgressStepComplete {
			// Only a completed step is a way back; an upcoming step
			// with an Href renders no anchor, the field's own contract.
			href = s.Href
		}
		steps[i] = headless.Step{
			Label:  s.Label,
			Hint:   s.Hint,
			Href:   href,
			Marker: marker,
			State:  state,
		}
	}

	return progressStepsStyle.WrapHTML(render.Tag("nav", navAttrs,
		headless.Steps(headless.StepsProps{
			Steps:   steps,
			Current: current,
		}, progressStepsClasses),
	))
}

func progressStepsCheckIcon() string {
	return `<svg width="14" height="14" viewBox="0 0 14 14" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M11.667 3.5L5.25 9.917 2.333 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

var progressStepsStyle = registry.RegisterStyle("ui-progress-steps", progressStepsCSS)

func progressStepsCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-progress-steps"] {
  display: block;
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__list {
  display: flex;
  gap: var(--spacing-sm, 4px);
  margin: 0;
  padding: 0;
  list-style: none;
  counter-reset: progress-steps;
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__item {
  flex: 1 1 0;
  position: relative;
  min-width: 0;
}
/* Connector line between steps. Drawn from the right edge of every
   item except the last, behind the marker so the marker punches
   through. Tinted by the NEXT step's status — green if both complete,
   border-color otherwise. */
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__item + .fui-progress-steps__item::before {
  content: "";
  position: absolute;
  left: 0;
  right: 50%;
  top: 14px;
  height: 2px;
  background: var(--color-border, #E4E4E7);
  z-index: 0;
}
.fui-progress-steps__item[data-state="current"] + .fui-progress-steps__item::before,
.fui-progress-steps__item[data-state="done"] + .fui-progress-steps__item[data-state="done"]::before,
.fui-progress-steps__item[data-state="done"] + .fui-progress-steps__item::before {
  background: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__row {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-rows: auto auto;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__text {
  display: grid;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-fui-comp="ui-progress-steps"] a.fui-progress-steps__row:hover {
  text-decoration: underline;
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__marker {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 999px;
  background: var(--color-surface, #FFFFFF);
  border: 2px solid var(--color-border, #E4E4E7);
  font-size: var(--text-sm, 0.875rem);
  font-weight: 600;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__label {
  font-size: var(--text-sm, 0.875rem);
  font-weight: 600;
  text-align: center;
}
[data-fui-comp="ui-progress-steps"] .fui-progress-steps__hint {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  text-align: center;
}

/* Status states. */
.fui-progress-steps__item[data-state="current"] .fui-progress-steps__marker {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
}
.fui-progress-steps__item[data-state="current"] .fui-progress-steps__label {
  color: var(--color-text, #18181B);
}
.fui-progress-steps__item[data-state="done"] .fui-progress-steps__marker {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
}
.fui-progress-steps__item[data-state="done"] .fui-progress-steps__label {
  color: var(--color-text, #18181B);
}

/* Vertical orientation. */
.fui-progress-steps--vertical .fui-progress-steps__list {
  flex-direction: column;
  gap: var(--spacing-md, 8px);
}
.fui-progress-steps--vertical .fui-progress-steps__item {
  flex: 0 0 auto;
}
.fui-progress-steps--vertical .fui-progress-steps__row {
  grid-template-rows: auto;
  grid-template-columns: auto 1fr;
  justify-items: start;
  align-items: center;
  gap: var(--spacing-md, 8px);
}
.fui-progress-steps--vertical .fui-progress-steps__text {
  justify-items: start;
}
.fui-progress-steps--vertical .fui-progress-steps__label,
.fui-progress-steps--vertical .fui-progress-steps__hint {
  text-align: start;
}
.fui-progress-steps--vertical .fui-progress-steps__item + .fui-progress-steps__item::before {
  left: 13px;
  right: auto;
  top: -12px;
  bottom: auto;
  width: 2px;
  height: 12px;
}
`
}
