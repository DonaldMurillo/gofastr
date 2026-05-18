package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── RangeSlider (dual thumb) ───────────────────────────────────────
//
// Two overlaid <input type="range"> elements representing a low and
// a high bound. Native semantics on each thumb (keyboard, accessibility
// tree). The runtime module ensures the low thumb never exceeds the
// high thumb and vice-versa.
//
// Form-submit shape: two fields — Name+"-min" and Name+"-max" — so
// the server gets explicit lo/hi values without parsing a composite
// string.

// RangeSliderConfig configures a RangeSlider.
type RangeSliderConfig struct {
	// Name is the form-field base name (required). Two inputs ship —
	// Name+"-min" and Name+"-max".
	Name string
	// Label is the accessible group name (required, used as the
	// fieldset legend / radiogroup aria-label).
	Label string
	// Min / Max bound the range. Defaults: 0 / 100.
	Min int
	Max int
	// Step is the step granularity. Default 1.
	Step int
	// ValueLow / ValueHigh are the initial low and high values.
	// Defaults: Min / Max.
	ValueLow  int
	ValueHigh int
	// ShowValue renders a live "lo – hi" text alongside the label.
	ShowValue bool
	// Disabled disables both thumbs.
	Disabled bool
	ID       string
	Class    string
	Attrs    html.Attrs
}

// RangeSlider renders a dual-thumb range input.
func RangeSlider(cfg RangeSliderConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: RangeSlider requires Name")
	}
	if cfg.Label == "" {
		panic("ui: RangeSlider requires Label")
	}
	min := cfg.Min
	max := cfg.Max
	if max == 0 && min == 0 {
		max = 100
	}
	if min >= max {
		panic("ui: RangeSlider requires Min < Max")
	}
	step := cfg.Step
	if step == 0 {
		step = 1
	}
	lo := cfg.ValueLow
	hi := cfg.ValueHigh
	if lo == 0 && hi == 0 {
		lo, hi = min, max
	}
	if lo < min {
		lo = min
	}
	if hi > max {
		hi = max
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-range-slider"
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	mkInput := func(suffix string, val int, ariaLabel string) render.HTML {
		a := map[string]string{
			"type":       "range",
			"name":       cfg.Name + "-" + suffix,
			"id":         id + "-" + suffix,
			"class":      "ui-range-slider__input ui-range-slider__input--" + suffix,
			"min":        strconv.Itoa(min),
			"max":        strconv.Itoa(max),
			"step":       strconv.Itoa(step),
			"value":      strconv.Itoa(val),
			"aria-label": ariaLabel,
			// Module marker so the cross-clamp + value-mirror code only
			// hooks pairs that opt in.
			"data-fui-range-slider": id,
		}
		if cfg.Disabled {
			a["disabled"] = ""
		}
		return render.Tag("input", a)
	}

	header := []render.HTML{
		render.Tag("span", map[string]string{"class": "ui-range-slider__label"},
			render.Text(cfg.Label)),
	}
	if cfg.ShowValue {
		header = append(header,
			render.Tag("output", map[string]string{
				"class":                       "ui-range-slider__value",
				"data-fui-range-slider-value": id,
			}, render.Text(strconv.Itoa(lo)+" – "+strconv.Itoa(hi))))
	}

	children := []render.HTML{
		render.Tag("div", map[string]string{"class": "ui-range-slider__header"}, header...),
		render.Tag("div", map[string]string{"class": "ui-range-slider__track-wrap"},
			render.Tag("div", map[string]string{"class": "ui-range-slider__track"}),
			mkInput("min", lo, cfg.Label+" minimum"),
			mkInput("max", hi, cfg.Label+" maximum"),
		),
	}

	attrs := html.Attrs{
		"class":       cls,
		"role":        "group",
		"aria-label":  cfg.Label,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.Attrs {
		attrs[k] = v
	}
	return rangeSliderStyle.WrapHTML(render.Tag("div", attrs, children...))
}

var rangeSliderStyle = registry.RegisterStyle("ui-range-slider", rangeSliderCSS)

func rangeSliderCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-range-slider"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--spacing-md, 12px);
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  font-size: 0.9rem;
  color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__track-wrap {
  position: relative;
  block-size: var(--spacing-touch-target, 44px);
  padding-block: calc((var(--spacing-touch-target, 44px) - 6px) / 2);
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__track {
  position: absolute;
  inset-inline: 0;
  inset-block-start: calc(50% - 3px);
  block-size: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__input {
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
[data-fui-comp="ui-range-slider"] .ui-range-slider__input::-webkit-slider-thumb {
  appearance: none;
  -webkit-appearance: none;
  width: 20px; height: 20px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
  pointer-events: auto;
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__input::-moz-range-thumb {
  width: 18px; height: 18px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
  pointer-events: auto;
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__input::-webkit-slider-runnable-track {
  background: transparent;
  height: 6px;
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__input::-moz-range-track {
  background: transparent;
  height: 6px;
}
[data-fui-comp="ui-range-slider"] .ui-range-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}
[data-fui-comp="ui-range-slider"].is-disabled .ui-range-slider__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
