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
	// ID), data-cui-*, role, and aria-label (use Label).
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
	// Knobs: --ui-range-slider-track-height (6px, the bar both thumbs
	// ride), --ui-range-slider-thumb-size (20px, the WebKit thumb) and
	// --ui-range-slider-thumb-size-moz (18px, the Firefox thumb). The
	// bar's centring arithmetic (track padding, 50% offset, the
	// WebKit thumb's margin-top) is calc() over the same knobs.

	return `[data-cui-comp="ui-range-slider"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__label {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__label + .fui-range-slider__value {
  justify-self: end;
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: var(--font-weight-semibold);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-primary, #4F46E5);
}
/* The track is the positioning context the two thumbs overlay; its
   own bar is drawn behind them. */
[data-cui-comp="ui-range-slider"] .fui-range-slider__track {
  position: relative;
  block-size: var(--spacing-touch-target, 44px);
  padding-block: calc((var(--spacing-touch-target, 44px) - var(--ui-range-slider-track-height, 6px)) / 2);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__track::before {
  content: "";
  position: absolute;
  inset-inline: 0;
  inset-block-start: calc(50% - var(--ui-range-slider-track-height, 6px) / 2);
  block-size: var(--ui-range-slider-track-height, 6px);
  background: var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__input {
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
[data-cui-comp="ui-range-slider"] .fui-range-slider__input::-webkit-slider-thumb {
  appearance: none;
  -webkit-appearance: none;
  width: var(--ui-range-slider-thumb-size, 20px); height: var(--ui-range-slider-thumb-size, 20px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thick, 2px) solid var(--color-primary, #18181B);
  box-shadow: var(--shadow-sm);
  /* Centre the 20px thumb on the 6px runnable track, as Slider does;
     WebKit aligns the thumb's top edge to the track otherwise. */
  margin-top: calc((var(--ui-range-slider-track-height, 6px) - var(--ui-range-slider-thumb-size, 20px)) / 2);
  cursor: pointer;
  pointer-events: auto;
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__input::-moz-range-thumb {
  width: var(--ui-range-slider-thumb-size-moz, 18px); height: var(--ui-range-slider-thumb-size-moz, 18px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thick, 2px) solid var(--color-primary, #18181B);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  pointer-events: auto;
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__input::-webkit-slider-runnable-track {
  background: transparent;
  height: var(--ui-range-slider-track-height, 6px);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__input::-moz-range-track {
  background: transparent;
  height: var(--ui-range-slider-track-height, 6px);
}
/* The thumb carries the ring; the input's own outline would box the
   whole track. */
[data-cui-comp="ui-range-slider"] .fui-range-slider__input:focus { outline: none; }
[data-cui-comp="ui-range-slider"] .fui-range-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 var(--stroke-focus-offset, 2px) var(--color-surface, #fff), 0 0 0 calc(var(--stroke-focus-offset, 2px) + var(--stroke-focus, 2px)) var(--color-text-subtle);
}
[data-cui-comp="ui-range-slider"] .fui-range-slider__input:focus-visible::-moz-range-thumb {
  box-shadow: 0 0 0 var(--stroke-focus-offset, 2px) var(--color-surface, #fff), 0 0 0 calc(var(--stroke-focus-offset, 2px) + var(--stroke-focus, 2px)) var(--color-text-subtle);
}
[data-cui-comp="ui-range-slider"].is-disabled .fui-range-slider__input {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}`
}
