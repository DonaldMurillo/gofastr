package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Toggle controls: Checkbox / Radio / Switch ─────────────────────
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the control's root
	// <label> element. Keys the component owns are dropped: class
	// and id (use Class / ID), data-fui-*, and for (the label→input
	// association; the runtime keys click and screen-reader wiring
	// off it).
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
// Same form-submission semantics as Checkbox: submits Value (or
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
		// distinct id, otherwise multiple <label for=…> point at the
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
		"type":  inputType,
		"name":  cfg.Name,
		"id":    id,
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

	// Switch uses an extra visual "track" element painted via CSS;
	// no extra DOM beyond input + label needed because the
	// :before/:after pseudo-elements handle the thumb/track. Keeps
	// the markup uniform across checkbox/radio/switch.
	input := render.Tag("input", inputAttrs)

	control := html.Span(html.TextConfig{Class: "ui-toggle__control"}, input,
		html.Span(html.TextConfig{Class: "ui-toggle__indicator", ExtraAttrs: html.Attrs{"aria-hidden": "true"}}))

	labelText := html.Span(html.TextConfig{Class: "ui-toggle__label"}, render.Text(cfg.Label))

	children := []render.HTML{control, labelText}
	children = append(children, fieldMessage(id, "ui-toggle", cfg.Error, cfg.Help)...)

	// Native <label for=…> wraps the control. The for/id pairing is
	// what the screen reader uses; the click-on-label-toggles-checkbox
	// behavior is also native.
	labelAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "for")
	if labelAttrs == nil {
		labelAttrs = map[string]string{}
	}
	labelAttrs["class"] = cls
	labelAttrs["for"] = id
	return toggleStyle.WrapHTML(render.Tag("label", labelAttrs, children...))
}

// ─── RadioGroup / CheckboxGroup ───────────────────────────────────────
//
// <fieldset> + <legend> wrappers around N existing Radio/Checkbox
// controls with group-level help and error wiring.

// RadioGroupOption describes one radio button in a RadioGroup.
type RadioGroupOption struct {
	Value    string
	Label    string
	Checked  bool
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
	ID       string
	Class    string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the group's root
	// <fieldset> element. Keys the component owns are dropped: class
	// and id (use Class / ID), data-fui-*, role, and
	// aria-describedby (wired to the group's help/error message).
	ExtraAttrs html.Attrs
}

// RadioGroup renders a <fieldset> of radio buttons with a shared
// name, group-level legend, and optional help/error text.
func RadioGroup(cfg RadioGroupConfig) render.HTML {
	opts := make([]toggleGroupOption, len(cfg.Options))
	for i, opt := range cfg.Options {
		opts[i] = toggleGroupOption{value: opt.Value, label: opt.Label, checked: opt.Checked, disabled: opt.Disabled}
	}
	return renderToggleGroup(toggleGroupSpec{
		kind:       "RadioGroup",
		role:       "radiogroup",
		name:       cfg.Name,
		legend:     cfg.Legend,
		help:       cfg.Help,
		errText:    cfg.Error,
		id:         cfg.ID,
		class:      cfg.Class,
		required:   cfg.Required,
		extraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:    opts,
		leaf:       Radio,
	})
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
	ID       string
	Class    string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the group's root
	// <fieldset> element. Keys the component owns are dropped: class
	// and id (use Class / ID), data-fui-*, role, and
	// aria-describedby (wired to the group's help/error message).
	ExtraAttrs html.Attrs
}

// CheckboxGroup renders a <fieldset> of checkboxes with a shared
// name, group-level legend, and optional help/error text.
func CheckboxGroup(cfg CheckboxGroupConfig) render.HTML {
	opts := make([]toggleGroupOption, len(cfg.Options))
	for i, opt := range cfg.Options {
		opts[i] = toggleGroupOption{value: opt.Value, label: opt.Label, checked: opt.Checked, disabled: opt.Disabled}
	}
	return renderToggleGroup(toggleGroupSpec{
		kind:       "CheckboxGroup",
		role:       "group",
		name:       cfg.Name,
		legend:     cfg.Legend,
		help:       cfg.Help,
		errText:    cfg.Error,
		id:         cfg.ID,
		class:      cfg.Class,
		required:   cfg.Required,
		extraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:    opts,
		leaf:       Checkbox,
	})
}

// toggleGroupOption is the option shape the two group components share;
// renderToggleGroup works on this normalized form because Go does not
// allow field access on a type parameter constrained to a union of the
// two exported option structs.
type toggleGroupOption struct {
	value    string
	label    string
	checked  bool
	disabled bool
}

// toggleGroupSpec is the shared body of the two group components: the
// common config fields plus the pieces that differ between them (the
// fieldset ARIA role and the leaf renderer for each option).
type toggleGroupSpec struct {
	kind       string // component name used in panic messages
	role       string // fieldset ARIA role ("radiogroup" or "group")
	name       string
	legend     string
	help       string
	errText    string
	id         string
	class      string
	required   bool
	extraAttrs html.Attrs
	options    []toggleGroupOption
	leaf       func(ToggleConfig) render.HTML // Radio or Checkbox
}

// renderToggleGroup is the single body behind RadioGroup and
// CheckboxGroup; before it existed, that body was duplicated verbatim
// (once per component) in the two exported functions above.
func renderToggleGroup(spec toggleGroupSpec) render.HTML {
	if spec.name == "" {
		panic("ui: " + spec.kind + " requires Name")
	}
	if spec.legend == "" {
		panic("ui: " + spec.kind + " requires Legend")
	}

	id := spec.id
	if id == "" {
		id = spec.name + "-group"
	}

	cls := "ui-toggle-group"
	if spec.errText != "" {
		cls += " is-error"
	}
	if spec.class != "" {
		cls += " " + spec.class
	}

	// The marker lives INSIDE the <legend>: the fieldset is a grid, so a
	// sibling span would become its own row under the legend text.
	legendChildren := []render.HTML{render.Text(spec.legend)}
	if spec.required {
		legendChildren = append(legendChildren,
			html.Span(html.TextConfig{
				Class:      "ui-form-field__required",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")))
	}
	legend := render.Tag("legend", map[string]string{"class": "ui-toggle-group__legend"}, legendChildren...)

	children := []render.HTML{legend}
	for i, opt := range spec.options {
		optID := id + "-" + slug(opt.value)
		if opt.value == "" {
			optID = fmt.Sprintf("%s-%d", id, i)
		}
		children = append(children, spec.leaf(ToggleConfig{
			Name:     spec.name,
			Label:    opt.label,
			Value:    opt.value,
			Checked:  opt.checked,
			Disabled: opt.disabled,
			Required: spec.required,
			ID:       optID,
		}))
	}
	children = append(children, fieldMessage(id, "ui-toggle-group", spec.errText, spec.help)...)

	// extraAttrs arrives pre-sanitized (each group function routes
	// cfg.ExtraAttrs through html.SafeExtraAttrs; the contract test
	// requires it at the read site). SafeExtraAttrs returns nil for a
	// nil input, so an empty attrs map starts here.
	attrs := spec.extraAttrs
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["class"] = cls
	attrs["id"] = id
	attrs["role"] = spec.role
	if spec.errText != "" {
		attrs["aria-describedby"] = id + "-error"
	} else if spec.help != "" {
		attrs["aria-describedby"] = id + "-help"
	}

	return toggleStyle.WrapHTML(render.Tag("fieldset", attrs, children...))
}
