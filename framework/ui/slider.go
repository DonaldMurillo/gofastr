package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Slider ─────────────────────────────────────────────────────────
//
// Styled <input type="range"> with optional value display and
// min/max edge labels. Native semantics — keyboard ArrowLeft/Right,
// PageUp/Down, Home/End all work via the browser. The styled track
// + thumb live in the registered ui-slider sheet.

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
	// Value is the initial value (clamped to [Min,Max]).
	Value int
	// ShowValue renders a value bubble next to the label that updates
	// via :has() / CSS custom-property tricks isn't supported across
	// browsers yet — we emit a simple <output> element instead, which
	// the browser auto-updates as the range input moves (the native
	// form-output association).
	ShowValue bool
	// ShowEdgeLabels renders the Min and Max values under the track.
	ShowEdgeLabels bool
	// Disabled disables interaction.
	Disabled   bool
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// Slider renders a labelled range input.
func Slider(cfg SliderConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Slider requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Slider requires Label")
	}
	min := cfg.Min
	max := cfg.Max
	if max == 0 && min == 0 {
		max = 100
	}
	if min >= max {
		panic("ui: Slider requires Min < Max")
	}
	step := cfg.Step
	if step == 0 {
		step = 1
	}
	val := cfg.Value
	if val < min {
		val = min
	}
	if val > max {
		val = max
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-slider"
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":  "range",
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-slider__input",
		"min":   strconv.Itoa(min),
		"max":   strconv.Itoa(max),
		"step":  strconv.Itoa(step),
		"value": strconv.Itoa(val),
		// <label for=…> wires the visible label as the input's
		// accessible name, but some AT scanners (and the rendered
		// screenshot tooling) miss the association. aria-label
		// guarantees the name is on the input itself.
		"aria-label": cfg.Label,
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	for k, v := range cfg.ExtraAttrs {
		inputAttrs[k] = v
	}

	header := []render.HTML{
		render.Tag("label", map[string]string{"for": id, "class": "ui-slider__label"},
			render.Text(cfg.Label)),
	}
	if cfg.ShowValue {
		// <output for=id> + data-fui-slider-mirror on the input
		// triggers the slider runtime module to keep the output
		// text in sync with the live value as the user drags.
		header = append(header,
			render.Tag("output",
				map[string]string{"for": id, "class": "ui-slider__value"},
				render.Text(strconv.Itoa(val))))
		inputAttrs["data-fui-slider-mirror"] = ""
	}

	children := []render.HTML{
		render.Tag("div", map[string]string{"class": "ui-slider__header"}, header...),
		render.Tag("input", inputAttrs),
	}
	if cfg.ShowEdgeLabels {
		children = append(children,
			render.Tag("div", map[string]string{"class": "ui-slider__edges"},
				html.Span(html.TextConfig{Class: "ui-slider__edge"}, render.Text(strconv.Itoa(min))),
				html.Span(html.TextConfig{Class: "ui-slider__edge"}, render.Text(strconv.Itoa(max))),
			))
	}

	return sliderStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

var sliderStyle = registry.RegisterStyle("ui-slider", sliderCSS)

func sliderCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-slider"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-slider"] .ui-slider__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--spacing-md, 12px);
}
[data-fui-comp="ui-slider"] .ui-slider__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-slider"] .ui-slider__value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-primary, #4F46E5);
  min-inline-size: 3ch;
  text-align: end;
}
[data-fui-comp="ui-slider"] .ui-slider__edges {
  display: flex;
  justify-content: space-between;
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  margin-top: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-slider"] .ui-slider__input {
  appearance: none;
  -webkit-appearance: none;
  width: 100%;
  height: var(--spacing-touch-target, 44px);
  background: transparent;
  cursor: pointer;
}
[data-fui-comp="ui-slider"] .ui-slider__input:focus { outline: none; }
/* WebKit + Blink */
[data-fui-comp="ui-slider"] .ui-slider__input::-webkit-slider-runnable-track {
  height: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-slider"] .ui-slider__input::-webkit-slider-thumb {
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
[data-fui-comp="ui-slider"] .ui-slider__input:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}
[data-fui-comp="ui-slider"] .ui-slider__input:active::-webkit-slider-thumb {
  transform: scale(1.15);
}
/* Firefox */
[data-fui-comp="ui-slider"] .ui-slider__input::-moz-range-track {
  height: 6px;
  background: var(--color-border, #E4E4E7);
  border-radius: 999px;
}
[data-fui-comp="ui-slider"] .ui-slider__input::-moz-range-thumb {
  width: 18px;
  height: 18px;
  border-radius: 999px;
  background: var(--color-primary, #4F46E5);
  border: 2px solid var(--color-surface, #FFFFFF);
  cursor: pointer;
}
[data-fui-comp="ui-slider"] .ui-slider__input:focus-visible::-moz-range-thumb {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-primary, #4F46E5) 30%, transparent);
}

[data-fui-comp="ui-slider"].is-disabled .ui-slider__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
