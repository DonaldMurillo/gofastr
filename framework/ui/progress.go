package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Renders headless.Progress dressed with the fui-progress class map:
// the native <progress> element, named (visibly or through
// aria-label) and clamped — a value the request carried is clamped
// into [0, Max] at render, a negative value is the indeterminate
// posture. No script: the element carries its own accessibility
// contract. This is one fraction of one quantity, not a walk through
// named stages — a step indicator is ProgressSteps, a different
// component.

// ProgressConfig configures one bar.
type ProgressConfig struct {
	// Value is the current progress. 0 to Max renders a determinate
	// bar; a negative value renders an indeterminate one. Clamped to
	// Max at render.
	Value float64
	// Max is the ceiling. 0 takes 100.
	Max float64

	// Label is the bar's accessible name. Required; visible as text
	// when ShowLabel is set, aria-label otherwise.
	Label     string
	ShowLabel bool

	// Description is an optional sentence beside the bar ("73 of
	// 100", "Uploading…").
	Description string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root. Keys the
	// component owns are dropped: class and id (use Class / ID).
	ExtraAttrs html.Attrs
}

// Progress renders the bar.
func Progress(cfg ProgressConfig) render.HTML {
	classes := headless.Classes{
		headless.PartRoot:          "fui-progress",
		headless.PartLabel:         "fui-progress__label",
		headless.PartProgressValue: "fui-progress__bar",
		headless.PartDesc:          "fui-progress__desc",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.Progress(headless.ProgressProps{
		Value:        cfg.Value,
		Max:          cfg.Max,
		Label:        cfg.Label,
		LabelVisible: cfg.ShowLabel,
		Description:  cfg.Description,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "class", "id"),
	}, classes)
	return progressStyle.WrapHTML(out)
}

var progressStyle = registry.RegisterStyle("ui-progress", progressCSS)

func progressCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-progress"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-progress"] .fui-progress__label {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #1F2937);
  font-weight: 500;
}
[data-fui-comp="ui-progress"] .fui-progress__bar {
  appearance: none;
  -webkit-appearance: none;
  inline-size: 100%;
  block-size: 0.5rem;
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: var(--color-border, #E5E7EB);
  overflow: hidden;
}
[data-fui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-bar {
  background: var(--color-border, #E5E7EB);
  border-radius: var(--radii-full, 9999px);
}
[data-fui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-value {
  background: var(--color-primary, #4F46E5);
  border-radius: var(--radii-full, 9999px);
  transition: inline-size 200ms ease;
}
[data-fui-comp="ui-progress"] .fui-progress__bar::-moz-progress-bar {
  background: var(--color-primary, #4F46E5);
  border-radius: var(--radii-full, 9999px);
}
[data-fui-comp="ui-progress"] .fui-progress__desc {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #6B7280);
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-value { transition: none; }
}`
}
