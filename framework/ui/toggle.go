package ui

import (
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
	Attrs html.Attrs

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
	for k, v := range cfg.Attrs {
		inputAttrs[k] = v
	}

	// Switch uses an extra visual "track" element painted via CSS;
	// no extra DOM beyond input + label needed because the
	// :before/:after pseudo-elements handle the thumb/track. Keeps
	// the markup uniform across checkbox/radio/switch.
	input := render.Tag("input", inputAttrs)

	control := html.Span(html.TextConfig{Class: "ui-toggle__control"}, input,
		html.Span(html.TextConfig{Class: "ui-toggle__indicator", Attrs: html.Attrs{"aria-hidden": "true"}}))

	labelText := html.Span(html.TextConfig{Class: "ui-toggle__label"}, render.Text(cfg.Label))

	children := []render.HTML{control, labelText}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-error",
			Class: "ui-toggle__error",
			Attrs: html.Attrs{"role": "alert"},
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
