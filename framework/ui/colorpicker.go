package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ColorPicker ────────────────────────────────────────────────────
//
// Styled wrapper around <input type="color">. The browser's native
// color UI handles the actual color picking — we own the visual
// chrome (label + sized swatch that respects --spacing-touch-target).
//
// Preset-swatches strip is deferred (see ui-component-roadmap.md):
// implementing arbitrary-color swatches without inline `style="…"`
// requires either a runtime module that paints them via CSS custom
// properties or a pre-known palette baked into the stylesheet.

// ColorPickerConfig configures a ColorPicker.
type ColorPickerConfig struct {
	// Name is the form field name for the color input (required).
	Name string
	// Label is the accessible label (required).
	Label string
	// Value is the initial color (hex, e.g. "#4F46E5"). Defaults to
	// the browser's native default (black) when empty.
	Value string
	// Disabled disables the input.
	Disabled bool
	ID       string
	Class    string
}

// ColorPicker renders a styled native color input with a label.
func ColorPicker(cfg ColorPickerConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: ColorPicker requires Name")
	}
	if cfg.Label == "" {
		panic("ui: ColorPicker requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	cls := "ui-color-picker"
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":  "color",
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-color-picker__input",
	}
	if cfg.Value != "" {
		inputAttrs["value"] = cfg.Value
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}

	// Swatch first, label after — the same reading order as Checkbox
	// (control on the left, its name on the right).
	row := []render.HTML{
		render.Tag("input", inputAttrs),
		render.Tag("label", map[string]string{
			"for":   id,
			"class": "ui-color-picker__label",
		}, render.Text(cfg.Label)),
	}

	return colorPickerStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls, "id": id + "-wrap"},
		render.Tag("div", map[string]string{"class": "ui-color-picker__row"}, row...),
	))
}

var colorPickerStyle = registry.RegisterStyle("ui-color-picker", colorPickerCSS)

func colorPickerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-color-picker"] {
  display: grid;
  gap: var(--spacing-sm, 8px);
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 12px);
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__input {
  appearance: none;
  -webkit-appearance: none;
  width: var(--spacing-touch-target, 44px);
  height: var(--spacing-touch-target, 44px);
  padding: 0;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: transparent;
  cursor: pointer;
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__input::-webkit-color-swatch-wrapper {
  padding: 0;
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__input::-webkit-color-swatch {
  border: 0;
  border-radius: calc(var(--radii-md, 8px) - 2px);
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__input::-moz-color-swatch {
  border: 0;
  border-radius: calc(var(--radii-md, 8px) - 2px);
}
[data-fui-comp="ui-color-picker"] .ui-color-picker__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-color-picker"].is-disabled .ui-color-picker__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
