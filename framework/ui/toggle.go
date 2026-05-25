package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Toggle controls — Checkbox / Radio / Switch ─────────────────────
//
// Three labelled, FieldErrors-aware form controls that wrap a native
// <input type={checkbox,radio}> with a properly associated <label>.
// All emit data-fui-comp="ui-toggle" so a single stylesheet covers
// the family.

// ToggleConfig configures a Checkbox/Radio/Switch.
type ToggleConfig struct {
	// Name is the form-field name. Required.
	Name string

	// Label is the visible label text shown next to the control.
	// Required for accessibility.
	Label string

	// ID is the input element's id. When empty, defaults to Name.
	// FormField-style components key error wiring off this id.
	ID string

	// Value is the form-submit value (for checkboxes / radios sharing
	// a Name). Defaults to "on" for Checkbox/Switch, required for
	// Radio when several share a Name.
	Value string

	// Checked is the initial selected state.
	Checked bool

	// Disabled disables interaction.
	Disabled bool

	// Required marks the control as required in form submission.
	Required bool

	// Help renders supporting text under the label.
	Help string

	// Error overrides Help and switches the control to the error state
	// (aria-invalid="true", red ring, role="alert" message).
	Error string

	// Attrs lets callers attach data-fui-* attributes (e.g. for RPC).
	ExtraAttrs html.Attrs

	Class string
}

// Checkbox renders a single labelled checkbox. Pair with FormField
// when you need section-level grouping; use the standalone Checkbox
// for inline toggles ("Remember me", "Send copy to admin").
func Checkbox(cfg ToggleConfig) render.HTML {
	return renderToggle("checkbox", "ui-toggle--checkbox", cfg)
}

// Radio renders a single radio. Share Name across multiple Radios to
// form a group; pass distinct Value strings.
func Radio(cfg ToggleConfig) render.HTML {
	if cfg.Value == "" {
		panic("ui: Radio requires Value")
	}
	return renderToggle("radio", "ui-toggle--radio", cfg)
}

// Switch renders a checkbox styled as an iOS-style toggle switch.
// Same form-submission semantics as Checkbox — submits Value (or
// "on") when checked, omits when unchecked.
func Switch(cfg ToggleConfig) render.HTML {
	return renderToggle("checkbox", "ui-toggle--switch", cfg)
}

func renderToggle(inputType, modifierClass string, cfg ToggleConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: " + modifierClass + " requires Name")
	}
	if cfg.Label == "" {
		panic("ui: " + modifierClass + " requires Label")
	}
	id := cfg.ID
	if id == "" {
		// Default to Name for checkbox/switch (one-per-Name); for radio
		// groups, append the Value so each input in the group gets a
		// distinct id — otherwise multiple <label for=…> point at the
		// same id and label-click activates the wrong (first) radio.
		id = cfg.Name
		if cfg.Value != "" && inputType == "radio" {
			id = cfg.Name + "-" + slug(cfg.Value)
		}
	}

	cls := "ui-toggle " + modifierClass
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := html.Attrs{
		"type": inputType,
		"name": cfg.Name,
		"id":   id,
		"class": "ui-toggle__input",
	}
	if cfg.Value != "" {
		inputAttrs["value"] = cfg.Value
	}
	if cfg.Checked {
		inputAttrs["checked"] = ""
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	if cfg.Required {
		inputAttrs["required"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		inputAttrs["aria-describedby"] = id + "-help"
	}
	for k, v := range cfg.ExtraAttrs {
		inputAttrs[k] = v
	}

	// Switch uses an extra visual "track" element painted via CSS;
	// no extra DOM beyond input + label needed because the
	// :before/:after pseudo-elements handle the thumb/track. Keeps
	// the markup uniform across checkbox/radio/switch.
	input := render.Tag("input", inputAttrs)

	control := html.Span(html.TextConfig{Class: "ui-toggle__control"}, input,
		html.Span(html.TextConfig{Class: "ui-toggle__indicator", ExtraAttrs: html.Attrs{"aria-hidden": "true"}}))

	labelText := html.Span(html.TextConfig{Class: "ui-toggle__label"}, render.Text(cfg.Label))

	children := []render.HTML{control, labelText}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-error",
			Class: "ui-toggle__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-toggle__help",
		}, render.Text(cfg.Help)))
	}

	// Native <label for=…> wraps the control. The for/id pairing is
	// what the screen reader uses; the click-on-label-toggles-checkbox
	// behavior is also native.
	return toggleStyle.WrapHTML(render.Tag("label", map[string]string{
		"class": cls,
		"for":   id,
	}, children...))
}

// ─── RadioGroup / CheckboxGroup ───────────────────────────────────────
//
// <fieldset> + <legend> wrappers around N existing Radio/Checkbox
// controls with group-level help and error wiring.

// RadioGroupOption describes one radio button in a RadioGroup.
type RadioGroupOption struct {
	Value   string
	Label   string
	Checked bool
	Disabled bool
}

// RadioGroupConfig configures a group of radio buttons.
type RadioGroupConfig struct {
	// Name is the shared form-field name for all radios (required).
	Name string
	// Legend is the group label rendered as <legend> (required).
	Legend string
	// Options is the list of radio options (required, at least one).
	Options []RadioGroupOption
	// Help renders supporting text under the group.
	Help string
	// Error overrides Help with an error message.
	Error string
	// Required marks the group as required.
	Required bool
	ID      string
	Class   string
}

// RadioGroup renders a <fieldset> of radio buttons with a shared
// name, group-level legend, and optional help/error text.
func RadioGroup(cfg RadioGroupConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: RadioGroup requires Name")
	}
	if cfg.Legend == "" {
		panic("ui: RadioGroup requires Legend")
	}

	id := cfg.ID
	if id == "" {
		id = cfg.Name + "-group"
	}

	cls := "ui-toggle-group"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	legend := render.Tag("legend", map[string]string{"class": "ui-toggle-group__legend"},
		render.Text(cfg.Legend))
	if cfg.Required {
		legend = render.Join(legend,
			html.Span(html.TextConfig{
				Class: "ui-form-field__required",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")))
	}

	children := []render.HTML{legend}
	for i, opt := range cfg.Options {
		rbID := id + "-" + slug(opt.Value)
		if opt.Value == "" {
			rbID = fmt.Sprintf("%s-%d", id, i)
		}
		children = append(children, Radio(ToggleConfig{
			Name:     cfg.Name,
			Label:    opt.Label,
			Value:    opt.Value,
			Checked:  opt.Checked,
			Disabled: opt.Disabled,
			Required: cfg.Required,
			ID:       rbID,
		}))
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-error",
			Class: "ui-toggle-group__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-toggle-group__help",
		}, render.Text(cfg.Help)))
	}

	attrs := map[string]string{
		"class": cls,
		"id":    id,
		"role":  "radiogroup",
	}
	if cfg.Error != "" {
		attrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		attrs["aria-describedby"] = id + "-help"
	}

	return toggleStyle.WrapHTML(render.Tag("fieldset", attrs, children...))
}

// CheckboxGroupOption describes one checkbox in a CheckboxGroup.
type CheckboxGroupOption struct {
	Value    string
	Label    string
	Checked  bool
	Disabled bool
}

// CheckboxGroupConfig configures a group of checkboxes.
type CheckboxGroupConfig struct {
	// Name is the shared form-field name for all checkboxes (required).
	Name string
	// Legend is the group label rendered as <legend> (required).
	Legend string
	// Options is the list of checkbox options (required, at least one).
	Options []CheckboxGroupOption
	// Help renders supporting text under the group.
	Help string
	// Error overrides Help with an error message.
	Error string
	// Required marks the group as required.
	Required bool
	ID      string
	Class   string
}

// CheckboxGroup renders a <fieldset> of checkboxes with a shared
// name, group-level legend, and optional help/error text.
func CheckboxGroup(cfg CheckboxGroupConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: CheckboxGroup requires Name")
	}
	if cfg.Legend == "" {
		panic("ui: CheckboxGroup requires Legend")
	}

	id := cfg.ID
	if id == "" {
		id = cfg.Name + "-group"
	}

	cls := "ui-toggle-group"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	legend := render.Tag("legend", map[string]string{"class": "ui-toggle-group__legend"},
		render.Text(cfg.Legend))
	if cfg.Required {
		legend = render.Join(legend,
			html.Span(html.TextConfig{
				Class: "ui-form-field__required",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")))
	}

	children := []render.HTML{legend}
	for i, opt := range cfg.Options {
		cbID := id + "-" + slug(opt.Value)
		if opt.Value == "" {
			cbID = fmt.Sprintf("%s-%d", id, i)
		}
		children = append(children, Checkbox(ToggleConfig{
			Name:     cfg.Name,
			Label:    opt.Label,
			Value:    opt.Value,
			Checked:  opt.Checked,
			Disabled: opt.Disabled,
			Required: cfg.Required,
			ID:       cbID,
		}))
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-error",
			Class: "ui-toggle-group__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-toggle-group__help",
		}, render.Text(cfg.Help)))
	}

	attrs := map[string]string{
		"class": cls,
		"id":    id,
		"role":  "group",
	}
	if cfg.Error != "" {
		attrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		attrs["aria-describedby"] = id + "-help"
	}

	return toggleStyle.WrapHTML(render.Tag("fieldset", attrs, children...))
}
