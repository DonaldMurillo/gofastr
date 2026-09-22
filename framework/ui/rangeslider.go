package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── RangeSlider (dual thumb) ───────────────────────────────────────
//
// Two overlaid <input type="range"> elements over headless.RangeSlider,
// one low bound and one high bound. Native semantics on each thumb.
// The module cross-clamps a drag that would cross the pair and keeps
// the output sentence in step, re-formatted through the sentence the
// server rendered into the output's hook.
//
// Form-submit shape: two fields, Name+"-min" and Name+"-max", so the
// server gets explicit lo/hi values without parsing a composite
// string.

// RangeSliderConfig configures a RangeSlider.
type RangeSliderConfig struct {
	// Name is the form-field base name (required). Two inputs ship:
	// Name+"-min" and Name+"-max".
	Name string
	// Label is the accessible group name (required; each thumb is
	// named from it: "Minimum <Label>", "Maximum <Label>").
	Label string
	// Min / Max bound the range. Defaults: 0 / 100.
	Min int
	Max int
	// Step is the step granularity. Default 1.
	Step int
	// ValueLow / ValueHigh are the initial values. Defaults: Min / Max.
	// A crossed pair (low above high) is refused at render — the
	// module clamps drags, never the server's props.
	ValueLow  int
	ValueHigh int
	// ShowValue renders the live "lo to hi" sentence beside the label.
	ShowValue bool
	// Disabled disables both thumbs.
	Disabled bool
	ID       string
	Class    string
	// ExtraAttrs forwards additional attributes to the root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, role, and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// rangeSliderClasses dresses headless.RangeSlider's parts in this
// package's own vocabulary — the names the registered ui-range-slider
// sheet matches. The two thumbs carry one class: they are visually
// identical by design (the module cross-clamps the pair), and the
// inputs are distinguished by their data-hui-range-slider-low/-high
// hooks and their -min/-max field names, which is what a selector
// should key on — not by a styling modifier nothing styles.
var rangeSliderClasses = headless.Classes{
	headless.PartRoot:        "fui-range-slider",
	headless.PartLabel:       "fui-range-slider__label",
	headless.PartRangeLow:    "fui-range-slider__input",
	headless.PartRangeHigh:   "fui-range-slider__input",
	headless.PartRangeOutput: "fui-range-slider__value",
	headless.PartRangeTrack:  "fui-range-slider__track",
}

// RangeSlider renders a dual-thumb range input.
func RangeSlider(cfg RangeSliderConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: RangeSlider requires Name")
	}
	if cfg.Label == "" {
		panic("ui: RangeSlider requires Label")
	}
	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(modifierClass("is-disabled", cfg.Disabled) +
		" " + cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(rootClass)}}
	}
	return rangeSliderStyle.WrapHTML(headless.RangeSlider(headless.RangeSliderProps{
		Name:       cfg.Name,
		Label:      cfg.Label,
		Min:        cfg.Min,
		Max:        cfg.Max,
		Step:       cfg.Step,
		ValueLow:   cfg.ValueLow,
		ValueHigh:  cfg.ValueHigh,
		ShowValue:  cfg.ShowValue,
		Disabled:   cfg.Disabled,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:      parts,
		Strings:    StringsFor(nil),
	}, rangeSliderClasses))
}

var rangeSliderStyle = registry.RegisterStyle("ui-range-slider", rangeSliderCSS)

func rangeSliderCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-range-slider"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__label + .fui-range-slider__value {
  justify-self: end;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-primary, #4F46E5);
}
/* The track is the positioning context the two thumbs overlay; its
   own bar is drawn behind them. */
[data-fui-comp="ui-range-slider"] .fui-range-slider__track {
  position: relative;
  block-size: var(--spacing-touch-target, 44px);
  padding-block: calc((var(--spacing-touch-target, 44px) - 6px) / 2);
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__track::before {
  content: "";
  position: absolute;
  inset-inline: 0;
  inset-block-start: calc(50% - 3px);
  block-size: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__input {
  position: absolute;
  inset-inline: 0;
  inset-block: 0;
  inline-size: 100%;
  block-size: 100%;
  appearance: none;
  -webkit-appearance: none;
  background: transparent;
  pointer-events: none;
}
/* The thumbs ARE clickable (pointer-events:auto on the thumb only). */
[data-fui-comp="ui-range-slider"] .fui-range-slider__input::-webkit-slider-thumb {
  appearance: none;
  -webkit-appearance: none;
  width: 20px; height: 20px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
  pointer-events: auto;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__input::-moz-range-thumb {
  width: 18px; height: 18px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
  pointer-events: auto;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__input::-webkit-slider-runnable-track {
  background: transparent;
  height: 6px;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__input::-moz-range-track {
  background: transparent;
  height: 6px;
}
[data-fui-comp="ui-range-slider"] .fui-range-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}
[data-fui-comp="ui-range-slider"].is-disabled .fui-range-slider__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
