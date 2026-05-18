package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ProgressSteps ──────────────────────────────────────────────────
//
// Step indicator showing current + completed + upcoming steps in a
// linear flow. Horizontal by default; vertical orientation for tall
// narrow layouts (mobile, sidebar). Pairs with the (deferred) Form
// Step Wizard pattern.
//
// Renders as <ol> with each step in <li>; the current step is marked
// aria-current="step" so screen readers announce position.

// ProgressStepStatus is the rendered state of a single step.
type ProgressStepStatus string

const (
	ProgressStepUpcoming ProgressStepStatus = ""         // default
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
	// to "Progress".
	Label string
	ID    string
	Class string
	Attrs html.Attrs
}

// ProgressSteps renders a step indicator.
func ProgressSteps(cfg ProgressStepsConfig) render.HTML {
	if len(cfg.Steps) == 0 {
		panic("ui: ProgressSteps requires at least one Step")
	}
	switch cfg.Orientation {
	case ProgressStepsHorizontal, ProgressStepsVertical:
	default:
		panic("ui: ProgressSteps unknown Orientation " + string(cfg.Orientation) +
			` — pick one of: "" (horizontal), vertical`)
	}
	label := cfg.Label
	if label == "" {
		label = "Progress"
	}
	cls := "ui-progress-steps"
	if cfg.Orientation == ProgressStepsVertical {
		cls += " ui-progress-steps--vertical"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	navAttrs := html.Attrs{"class": cls, "aria-label": label}
	if cfg.ID != "" {
		navAttrs["id"] = cfg.ID
	}
	for k, v := range cfg.Attrs {
		navAttrs[k] = v
	}

	items := make([]render.HTML, 0, len(cfg.Steps))
	for i, s := range cfg.Steps {
		if s.Label == "" {
			panic("ui: ProgressSteps step requires Label")
		}
		switch s.Status {
		case ProgressStepUpcoming, ProgressStepCurrent, ProgressStepComplete:
		default:
			panic("ui: ProgressSteps step unknown Status " + string(s.Status) +
				` — pick one of: "" (upcoming), current, complete`)
		}
		liCls := "ui-progress-steps__item"
		if s.Status != ProgressStepUpcoming {
			liCls += " ui-progress-steps__item--" + string(s.Status)
		}
		liAttrs := map[string]string{"class": liCls}
		if s.Status == ProgressStepCurrent {
			liAttrs["aria-current"] = "step"
		}
		// Marker: number for upcoming/current, checkmark for complete.
		var marker render.HTML
		switch s.Status {
		case ProgressStepComplete:
			marker = render.Tag("span", map[string]string{
				"class":       "ui-progress-steps__marker",
				"aria-hidden": "true",
			}, render.HTML(progressStepsCheckIcon()))
		default:
			marker = render.Tag("span", map[string]string{
				"class":       "ui-progress-steps__marker",
				"aria-hidden": "true",
			}, render.Text(strconv.Itoa(i+1)))
		}

		labelChildren := []render.HTML{
			html.Span(html.TextConfig{Class: "ui-progress-steps__label"}, render.Text(s.Label)),
		}
		if s.Hint != "" {
			labelChildren = append(labelChildren,
				html.Span(html.TextConfig{Class: "ui-progress-steps__hint"}, render.Text(s.Hint)))
		}

		var inner render.HTML
		if s.Status == ProgressStepComplete && s.Href != "" {
			inner = render.Tag("a", map[string]string{"href": s.Href, "class": "ui-progress-steps__row"},
				append([]render.HTML{marker}, labelChildren...)...)
		} else {
			inner = render.Tag("span", map[string]string{"class": "ui-progress-steps__row"},
				append([]render.HTML{marker}, labelChildren...)...)
		}

		items = append(items, render.Tag("li", liAttrs, inner))
	}
	return progressStepsStyle.WrapHTML(render.Tag("nav", navAttrs,
		render.Tag("ol", map[string]string{"class": "ui-progress-steps__list"}, items...)))
}

func progressStepsCheckIcon() string {
	return `<svg width="14" height="14" viewBox="0 0 14 14" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M11.667 3.5L5.25 9.917 2.333 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

var progressStepsStyle = registry.RegisterStyle("ui-progress-steps", progressStepsCSS)

func progressStepsCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-progress-steps"] {
  display: block;
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__list {
  display: flex;
  gap: var(--spacing-sm, 8px);
  margin: 0;
  padding: 0;
  list-style: none;
  counter-reset: progress-steps;
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__item {
  flex: 1 1 0;
  position: relative;
  min-width: 0;
}
/* Connector line between steps. Drawn from the right edge of every
   item except the last, behind the marker so the marker punches
   through. Tinted by the NEXT step's status — green if both complete,
   border-color otherwise. */
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__item + .ui-progress-steps__item::before {
  content: "";
  position: absolute;
  left: 0;
  right: 50%;
  top: 14px;
  height: 2px;
  background: var(--color-border, #E4E4E7);
  z-index: 0;
}
.ui-progress-steps__item--current + .ui-progress-steps__item::before,
.ui-progress-steps__item--complete + .ui-progress-steps__item--complete::before,
.ui-progress-steps__item--complete + .ui-progress-steps__item::before {
  background: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__row {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-rows: auto auto;
  justify-items: center;
  gap: var(--spacing-xs, 4px);
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
}
[data-fui-comp="ui-progress-steps"] a.ui-progress-steps__row:hover {
  text-decoration: underline;
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__marker {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 999px;
  background: var(--color-surface, #FFFFFF);
  border: 2px solid var(--color-border, #E4E4E7);
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__label {
  font-size: 0.85rem;
  font-weight: 600;
  text-align: center;
}
[data-fui-comp="ui-progress-steps"] .ui-progress-steps__hint {
  font-size: 0.75rem;
  color: var(--color-text-muted, #52525B);
  text-align: center;
}

/* Status states. */
.ui-progress-steps__item--current .ui-progress-steps__marker {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
}
.ui-progress-steps__item--current .ui-progress-steps__label {
  color: var(--color-text, #18181B);
}
.ui-progress-steps__item--complete .ui-progress-steps__marker {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
}
.ui-progress-steps__item--complete .ui-progress-steps__label {
  color: var(--color-text, #18181B);
}

/* Vertical orientation. */
.ui-progress-steps--vertical .ui-progress-steps__list {
  flex-direction: column;
  gap: var(--spacing-md, 12px);
}
.ui-progress-steps--vertical .ui-progress-steps__item {
  flex: 0 0 auto;
}
.ui-progress-steps--vertical .ui-progress-steps__row {
  grid-template-rows: auto;
  grid-template-columns: auto 1fr;
  justify-items: start;
  align-items: center;
  gap: var(--spacing-md, 12px);
}
.ui-progress-steps--vertical .ui-progress-steps__label,
.ui-progress-steps--vertical .ui-progress-steps__hint {
  text-align: start;
}
.ui-progress-steps--vertical .ui-progress-steps__item + .ui-progress-steps__item::before {
  left: 13px;
  right: auto;
  top: -12px;
  bottom: auto;
  width: 2px;
  height: 12px;
}`
}
