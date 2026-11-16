package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ProgressConfig configures one bar.
type ProgressConfig struct {
	// Value is the progress, clamped to Max; a negative one is indeterminate.
	Value float64
	// Max is the ceiling. 0 takes 100.
	Max float64

	// Label names the bar, as text with ShowLabel, else aria-label. Required.
	Label     string
	ShowLabel bool

	// Description is an optional line beside the bar ("73 of 100").
	Description string

	ID    string
	Class string
	// ExtraAttrs go on the root, less class and id.
	ExtraAttrs html.Attrs
}

// Progress renders a native <progress> bar for one quantity.
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
	return `[data-cui-comp="ui-progress"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-progress"] .fui-progress__label {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-medium);
}
[data-cui-comp="ui-progress"] .fui-progress__bar {
  appearance: none;
  -webkit-appearance: none;
  inline-size: 100%;
  block-size: var(--ui-progress-bar-height, 0.5rem);
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: var(--color-border, #E4E4E7);
  overflow: hidden;
}
[data-cui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-bar {
  background: var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-value {
  background: var(--color-primary, #18181B);
  border-radius: var(--radii-full, 9999px);
  transition: inline-size var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-progress"] .fui-progress__bar::-moz-progress-bar {
  background: var(--color-primary, #18181B);
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="ui-progress"] .fui-progress__desc {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-value { transition: none; }
}`
}
