package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ColorPickerConfig configures a ColorPicker.
type ColorPickerConfig struct {
	// Name and Label are required.
	Name  string
	Label string
	// Value is the initial hex colour; empty leaves the browser's black.
	Value    string
	Disabled bool
	ID       string
	Class    string

	// ExtraAttrs forwards attributes to the wrapper div; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs
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
	cls := "fui-color-picker"
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
		"class": "fui-color-picker__input",
	}
	if cfg.Value != "" {
		inputAttrs["value"] = cfg.Value
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}

	row := []render.HTML{
		render.Tag("input", inputAttrs),
		render.Tag("label", map[string]string{
			"for":   id,
			"class": "fui-color-picker__label",
		}, render.Text(cfg.Label)),
	}

	rootAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs)
	if rootAttrs == nil {
		rootAttrs = map[string]string{}
	}
	rootAttrs["class"] = cls
	rootAttrs["id"] = id + "-wrap"
	return colorPickerStyle.WrapHTML(render.Tag("div",
		rootAttrs,
		render.Tag("div", map[string]string{"class": "fui-color-picker__row", "data-cui-internal": ""}, row...),
	))
}

var colorPickerStyle = registry.RegisterStyle("ui-color-picker", colorPickerCSS)

func colorPickerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-color-picker"] {
  display: grid;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__label {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__input {
  appearance: none;
  -webkit-appearance: none;
  width: var(--spacing-touch-target, 44px);
  height: var(--spacing-touch-target, 44px);
  padding: 0;
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--radii-md, 8px);
  background: transparent;
  cursor: pointer;
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__input::-webkit-color-swatch-wrapper {
  padding: 0;
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__input::-webkit-color-swatch {
  border: 0;
  border-radius: calc(var(--radii-md, 8px) - 2px);
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__input::-moz-color-swatch {
  border: 0;
  border-radius: calc(var(--radii-md, 8px) - 2px);
}
[data-cui-comp="ui-color-picker"] .fui-color-picker__input:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-color-picker"].is-disabled .fui-color-picker__input {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}`
}
