package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Slider ─────────────────────────────────────────────────────────
//
// A labelled range input over headless.Slider: native semantics
// (keyboard ArrowLeft/Right, PageUp/Down, Home/End), an output the
// module keeps in step while the thumb moves, and the min/max edge
// labels. The styled track and thumb live in this sheet; the value the
// output shows is SSR text, so the number is right before script.

// SliderConfig configures a Slider.
type SliderConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required, used as <label for=…>).
	Label string
	// Min / Max bound the range. Defaults: 0 / 100.
	Min int
	Max int
	// Step is the step granularity. Default 1.
	Step int
	// Value is the initial value. Outside the range or off a step is
	// refused at render — the server's own props are not repaired.
	Value int
	// ShowValue renders the value output beside the label.
	ShowValue bool
	// ShowEdgeLabels renders the Min and Max values under the track.
	ShowEdgeLabels bool
	// Disabled disables interaction.
	Disabled bool
	ID       string
	Class    string
	// ExtraAttrs forwards additional attributes to the root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs
}

// sliderClasses dresses headless.Slider's parts in this package's own
// vocabulary — the names the registered ui-slider sheet matches.
var sliderClasses = headless.Classes{
	headless.PartRoot:         "fui-slider",
	headless.PartLabel:        "fui-slider__label",
	headless.PartControl:      "fui-slider__input",
	headless.PartSliderOutput: "fui-slider__value",
	headless.PartSliderEdges:  "fui-slider__edges",
	headless.PartSliderEdge:   "fui-slider__edge",
}

// Slider renders a labelled range input.
func Slider(cfg SliderConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Slider requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Slider requires Label")
	}
	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(modifierClass("is-disabled", cfg.Disabled) +
		" " + cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(rootClass)}}
	}
	return sliderStyle.WrapHTML(headless.Slider(headless.SliderProps{
		Name:           cfg.Name,
		Label:          cfg.Label,
		Min:            cfg.Min,
		Max:            cfg.Max,
		Step:           cfg.Step,
		Value:          cfg.Value,
		ShowValue:      cfg.ShowValue,
		ShowEdgeLabels: cfg.ShowEdgeLabels,
		Disabled:       cfg.Disabled,
		ID:             cfg.ID,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:          parts,
	}, sliderClasses))
}

var sliderStyle = registry.RegisterStyle("ui-slider", sliderCSS)

func sliderCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-slider"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-slider"] .fui-slider__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-slider"] .fui-slider__label + .fui-slider__value {
  justify-self: end;
}
[data-fui-comp="ui-slider"] .fui-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-primary, #4F46E5);
  min-inline-size: 3ch;
  text-align: end;
}
[data-fui-comp="ui-slider"] .fui-slider__edges {
  display: flex;
  justify-content: space-between;
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  margin-top: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-slider"] .fui-slider__input {
  appearance: none;
  -webkit-appearance: none;
  width: 100%;
  height: var(--spacing-touch-target, 44px);
  background: transparent;
  cursor: pointer;
}
[data-fui-comp="ui-slider"] .fui-slider__input:focus { outline: none; }
/* WebKit + Blink */
[data-fui-comp="ui-slider"] .fui-slider__input::-webkit-slider-runnable-track {
  height: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-slider"] .fui-slider__input::-webkit-slider-thumb {
  appearance: none;
  -webkit-appearance: none;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  margin-top: -7px;
  cursor: pointer;
  transition: transform 100ms ease;
}
[data-fui-comp="ui-slider"] .fui-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}
[data-fui-comp="ui-slider"] .fui-slider__input:active::-webkit-slider-thumb {
  transform: scale(1.15);
}
/* Firefox */
[data-fui-comp="ui-slider"] .fui-slider__input::-moz-range-track {
  height: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-slider"] .fui-slider__input::-moz-range-thumb {
  width: 18px;
  height: 18px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
}
[data-fui-comp="ui-slider"] .fui-slider__input:focus-visible::-moz-range-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}

[data-fui-comp="ui-slider"].is-disabled .fui-slider__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
