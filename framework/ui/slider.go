package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// SliderConfig configures a Slider.
type SliderConfig struct {
	// Ctx resolves the output and edge labels. Nil means English.
	Ctx context.Context

	// Name and Label are required.
	Name  string
	Label string
	// Min and Max default to 0 and 100, Step to 1.
	Min  int
	Max  int
	Step int
	// Value is clamped into Min..Max.
	Value int
	// ShowValue renders the value output beside the label, spoken on
	// the thumb as aria-valuetext.
	ShowValue bool
	// ShowEdgeLabels renders Min and Max under the track.
	ShowEdgeLabels bool
	Disabled       bool
	ID             string
	Class          string
	// ExtraAttrs forwards attributes to the root; keys the component
	// owns are dropped.
	ExtraAttrs html.Attrs
}

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
		Strings:        StringsFor(cfg.Ctx),
		Ctx:            cfg.Ctx,
	}, sliderClasses))
}

var sliderStyle = registry.RegisterStyle("ui-slider", sliderCSS)

func sliderCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-slider"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-slider"] .fui-slider__label {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-slider"] .fui-slider__label + .fui-slider__value {
  justify-self: end;
}
[data-cui-comp="ui-slider"] .fui-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: var(--font-weight-semibold);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-primary, #18181B);
  min-inline-size: 3ch;
  text-align: end;
}
[data-cui-comp="ui-slider"] .fui-slider__edges {
  display: flex;
  justify-content: space-between;
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  margin-top: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-slider"] .fui-slider__input {
  appearance: none;
  -webkit-appearance: none;
  width: 100%;
  height: var(--spacing-touch-target, 44px);
  background: transparent;
  cursor: pointer;
}
[data-cui-comp="ui-slider"] .fui-slider__input:focus { outline: none; }
/* WebKit + Blink */
[data-cui-comp="ui-slider"] .fui-slider__input::-webkit-slider-runnable-track {
  height: var(--ui-slider-track-height, 6px);
  background: var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="ui-slider"] .fui-slider__input::-webkit-slider-thumb {
  appearance: none;
  -webkit-appearance: none;
  width: var(--ui-slider-thumb-size, 20px);
  height: var(--ui-slider-thumb-size, 20px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thick, 2px) solid var(--color-primary, #18181B);
  box-shadow: var(--shadow-sm);
  margin-top: calc((var(--ui-slider-track-height, 6px) - var(--ui-slider-thumb-size, 20px)) / 2);
  cursor: pointer;
  transition: transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-slider"] .fui-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 var(--stroke-focus-offset, 2px) var(--color-surface, #fff), 0 0 0 calc(var(--stroke-focus-offset, 2px) + var(--stroke-focus, 2px)) var(--color-text-subtle);
}
[data-cui-comp="ui-slider"] .fui-slider__input:active::-webkit-slider-thumb {
  transform: scale(1.15);
}
/* Firefox */
[data-cui-comp="ui-slider"] .fui-slider__input::-moz-range-track {
  height: var(--ui-slider-track-height, 6px);
  background: var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="ui-slider"] .fui-slider__input::-moz-range-thumb {
  width: var(--ui-slider-thumb-size-moz, 18px);
  height: var(--ui-slider-thumb-size-moz, 18px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thick, 2px) solid var(--color-primary, #18181B);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
}
[data-cui-comp="ui-slider"] .fui-slider__input:focus-visible::-moz-range-thumb {
  box-shadow: 0 0 0 var(--stroke-focus-offset, 2px) var(--color-surface, #fff), 0 0 0 calc(var(--stroke-focus-offset, 2px) + var(--stroke-focus, 2px)) var(--color-text-subtle);
}

[data-cui-comp="ui-slider"].is-disabled .fui-slider__input {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}`
}
