package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Select ─────────────────────────────────────────────────────────
//
// Labelled native <select> with FormField-style label, help, and
// error wiring. Wraps core-ui/html.Select but adds the same
// chrome (label, error, help, required) that Checkbox/Radio/TextArea get.

// SelectOption describes a single <option>.
type SelectOption struct {
	Value    string
	Text     string
	Selected bool
}

// SelectConfig configures a Select.
type SelectConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required).
	Label string
	// Options is the list of <option> elements (required, at least one).
	Options []SelectOption
	// Placeholder adds a disabled, selected-first option with empty value
	// that acts as a placeholder hint (e.g. "Choose a country…").
	Placeholder string
	// Required marks the field required.
	Required bool
	// Disabled disables interaction.
	Disabled bool
	// Help renders supporting text under the field.
	Help string
	// Error overrides Help with an error message + aria-invalid.
	Error string
	ID    string
	Class string
	ExtraAttrs html.Attrs
}

// Select renders a labelled native <select> dropdown.
func Select(cfg SelectConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Select requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Select requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-select"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Build options HTML
	var optChildren []render.HTML
	if cfg.Placeholder != "" {
		optChildren = append(optChildren, render.Tag("option", map[string]string{
			"value":    "",
			"disabled": "disabled",
			"selected": "selected",
		}, render.Text(cfg.Placeholder)))
	}
	for _, opt := range cfg.Options {
		attrs := map[string]string{"value": opt.Value}
		if opt.Selected {
			attrs["selected"] = "selected"
		}
		optChildren = append(optChildren, render.Tag("option", attrs, render.Text(opt.Text)))
	}

	selAttrs := map[string]string{
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-select__input",
	}
	if cfg.Disabled {
		selAttrs["disabled"] = ""
	}
	if cfg.Required {
		selAttrs["required"] = ""
	}
	if cfg.Error != "" {
		selAttrs["aria-invalid"] = "true"
		selAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		selAttrs["aria-describedby"] = id + "-help"
	}
	for k, v := range cfg.ExtraAttrs {
		selAttrs[k] = v
	}

	labelHTML := render.Tag("label", map[string]string{
		"for":   id,
		"class": "ui-select__label",
	}, render.Text(cfg.Label))
	if cfg.Required {
		labelHTML = render.Join(labelHTML,
			html.Span(html.TextConfig{
				Class: "ui-form-field__required",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")))
	}

	children := []render.HTML{
		labelHTML,
		render.Tag("select", selAttrs, optChildren...),
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-error",
			Class: "ui-select__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-select__help",
		}, render.Text(cfg.Help)))
	}

	return selectStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls, "data-fui-comp": "ui-select"}, children...))
}

var selectStyle = registry.RegisterStyle("ui-select", selectCSS)

func selectCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-select"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-select"] .ui-select__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-select"] .ui-select__input {
  font: inherit;
  font-size: 0.95rem;
  padding: 10px var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2371717A' d='M2 4l4 4 4-4'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  padding-right: 36px;
  cursor: pointer;
  min-block-size: 44px;
}
[data-fui-comp="ui-select"] .ui-select__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-select"] .ui-select__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-select"] .ui-select__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-select"].is-error .ui-select__input {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
[data-fui-comp="ui-select"].is-disabled .ui-select__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
