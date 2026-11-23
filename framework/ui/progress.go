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

/* Indeterminate (no value): a primary band sweeps the track. The band
   is the bar's own background image, so the engine's value pseudo
   elements are cleared to let it show. */
[data-cui-comp="ui-progress"] .fui-progress__bar:indeterminate {
  background-color: var(--color-border, #E4E4E7);
  background-image: linear-gradient(90deg, transparent, var(--color-primary, #18181B), transparent);
  background-size: 40% 100%;
  background-repeat: no-repeat;
  animation: fui-progress-sweep 1.4s var(--easing-ease-in-out, ease-in-out) infinite;
}
[data-cui-comp="ui-progress"] .fui-progress__bar:indeterminate::-webkit-progress-bar { background: transparent; }
[data-cui-comp="ui-progress"] .fui-progress__bar:indeterminate::-moz-progress-bar { background: transparent; }
[data-cui-comp="ui-progress"] .fui-progress__bar:indeterminate:dir(rtl) { animation-direction: reverse; }
@keyframes fui-progress-sweep {
  from { background-position: -70% 0; }
  to   { background-position: 170% 0; }
}
/* Reduced motion: the band stops moving and fills the track, and only
   its opacity breathes, so the bar still reads as working. */
@keyframes fui-progress-breathe {
  0%, 100% { opacity: 1; }
  50%      { opacity: var(--opacity-disabled, 0.5); }
}

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-progress"] .fui-progress__bar::-webkit-progress-value { transition: none; }
  [data-cui-comp="ui-progress"] .fui-progress__bar:indeterminate {
    background-image: linear-gradient(90deg, color-mix(in oklab, var(--color-primary, #18181B) 45%, transparent), color-mix(in oklab, var(--color-primary, #18181B) 45%, transparent));
    background-size: 100% 100%;
    animation: fui-progress-breathe 2.4s var(--easing-ease-in-out, ease-in-out) infinite;
  }
}`
}
