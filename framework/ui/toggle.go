package ui

import (
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Toggle controls: Checkbox / Radio / Switch ─────────────────────
//
// Three labelled form controls rendered through the headless choice
// family: headless.Choice (checkbox, radio) and headless.Switch wrap
// the native input in its own label, so one row is one inline run
// that keeps its own structure and ignores the field sheet's
// --fui-field-columns (the field sheet documents the exemption).
//
// A choice carries a hint; an error belongs to the field or the group
// around it. A standalone control with an Error set therefore wraps
// itself in the fui-choice-field shell — the run and its message as
// one unit — because the message cannot ride inside the run without
// becoming part of the label's accessible name. The either/or shape
// is the family's own and does not follow the Field's both-visible
// rule: the choice never had a rule paragraph to keep beside the
// violation.

// ToggleConfig configures a Checkbox/Radio/Switch.
type ToggleConfig struct {
	// Name is the form-field name. Required.
	Name string

	// Label is the visible label text shown next to the control.
	// Required for accessibility.
	Label string

	// ID is the input element's id. When empty, defaults to Name
	// (Checkbox/Switch, one per name) or Name-slug(Value) (Radio, so
	// each input in a group gets a distinct id and no two labels
	// point at the same control).
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

	// Help renders supporting text inside the row, under the label
	// text (Checkbox/Radio) or under the run (Switch, whose headless
	// structure carries no hint part).
	Help string

	// Error replaces Help (either/or, the family's shape), marks the
	// input aria-invalid and wires its message by id. Prefer the
	// enclosing FormField's Error or the group's: an error is the
	// field's verdict, not the choice's.
	Error string

	// ExtraAttrs forwards additional attributes to the control's
	// <input> element — the control that submits. Keys the component
	// owns are dropped (type, name, id, value and the state
	// attributes, plus every data-fui-* and data-hui-* key); the
	// label that wraps the control offers no attribute seam of its
	// own, so what rides here rides on the input.
	ExtraAttrs html.Attrs

	Class string
}

// Checkbox renders a single labelled checkbox. Pair with FormField
// when you need section-level grouping; use the standalone Checkbox
// for inline toggles ("Remember me", "Send copy to admin").
func Checkbox(cfg ToggleConfig) render.HTML {
	return renderToggle("checkbox", cfg)
}

// Radio renders a single radio. Share Name across multiple Radios to
// form a group; pass distinct Value strings.
func Radio(cfg ToggleConfig) render.HTML {
	if cfg.Value == "" {
		panic("ui: Radio requires Value")
	}
	return renderToggle("radio", cfg)
}

// Switch renders a checkbox styled as an iOS-style toggle switch.
// Same form-submission semantics as Checkbox: submits Value (or
// "on") when checked, omits when unchecked.
func Switch(cfg ToggleConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Switch requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Switch requires Label")
	}
	// headless.Switch carries no hint part: a standalone Switch with
	// a message renders it under the run through the same shell an
	// errored choice uses. The message sits OUTSIDE the label, so it
	// is not part of the control's name — the input's described-by
	// must point at it or it is decoration.
	id := choiceID(cfg.ID, "checkbox", cfg.Name, cfg.Value)
	if cfg.Error == "" && cfg.Help == "" {
		return toggleStyle.WrapHTML(headless.Switch(switchProps(cfg), switchClasses))
	}
	p := switchProps(cfg)
	if cfg.Error != "" {
		p.Extra["aria-invalid"] = "true"
		p.Extra["aria-describedby"] = id + "-error"
	} else {
		p.Extra["aria-describedby"] = id + "-hint"
	}
	return toggleStyle.WrapHTML(erroredRun(
		headless.Switch(p, switchClasses),
		cfg.Help, cfg.Error, id))
}

// switchProps maps the shared config onto headless.Switch.
func switchProps(cfg ToggleConfig) headless.SwitchProps {
	id := choiceID(cfg.ID, "checkbox", cfg.Name, cfg.Value)
	extra := html.Attrs{}
	maps.Copy(extra, html.SafeExtraAttrs(cfg.ExtraAttrs))
	// The label's for= relationship no longer exists as a pair (the
	// label wraps the control); a caller's for= is inert on an input
	// and does not ship.
	delete(extra, "for")
	if cfg.Required {
		extra["required"] = ""
	}
	return headless.SwitchProps{
		Name: cfg.Name, Value: cfg.Value, Label: cfg.Label,
		Checked: cfg.Checked, Disabled: cfg.Disabled,
		ID: id, Extra: extra,
	}
}

// choiceID derives the input id: the caller's when given, else the
// name (checkbox/switch) or the name and the value's slug (radio).
func choiceID(callerID, inputType, name, value string) string {
	if callerID != "" {
		return callerID
	}
	if inputType == "radio" && value != "" {
		return name + "-" + slug(value)
	}
	return name
}

func renderToggle(inputType string, cfg ToggleConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: " + inputType + " requires Name")
	}
	if cfg.Label == "" {
		panic("ui: " + inputType + " requires Label")
	}
	id := choiceID(cfg.ID, inputType, cfg.Name, cfg.Value)
	hint := cfg.Help
	if cfg.Error != "" {
		hint = ""
	}
	extra := html.Attrs{}
	maps.Copy(extra, html.SafeExtraAttrs(cfg.ExtraAttrs))
	// The label's for= relationship no longer exists as a pair (the
	// label wraps the control); a caller's for= is inert on an input
	// and does not ship.
	delete(extra, "for")
	if cfg.Required {
		extra["required"] = ""
	}
	if cfg.Error != "" {
		// The message paragraph rides outside the label, so it is not
		// part of the control's name: the invalid state and the
		// described-by must land on the input itself.
		extra["aria-invalid"] = "true"
		extra["aria-describedby"] = id + "-error"
	}
	run := headless.Choice(headless.ChoiceProps{
		Type: inputType, Name: cfg.Name, Value: cfg.Value, Label: cfg.Label,
		Hint: hint, Checked: cfg.Checked, Disabled: cfg.Disabled,
		ID: id, Extra: extra,
	}, withRootClass(withRootClass(choiceClasses, "fui-choice--"+inputType), cfg.Class))
	if cfg.Error == "" {
		return toggleStyle.WrapHTML(run)
	}
	return toggleStyle.WrapHTML(erroredRun(run, "", cfg.Error, id))
}

// erroredRun wraps a rendered choice run with its message paragraph —
// error or hint — as one unit. The message cannot sit inside the
// label (it would join the control's accessible name), so the div
// holds the run and the paragraph together for whatever lays fields
// out around them. errText wins over helpText: the family's shape.
func erroredRun(run render.HTML, helpText, errText, id string) render.HTML {
	switch {
	case errText != "":
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run,
			render.Tag("p", map[string]string{
				"id":    id + "-error",
				"class": "fui-choice-field__error",
				"role":  "alert",
			}, render.Text(errText)))
	case helpText != "":
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run,
			render.Tag("p", map[string]string{
				"id":    id + "-hint",
				"class": "fui-choice-field__hint",
			}, render.Text(helpText)))
	default:
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run)
	}
}

// ─── RadioGroup / CheckboxGroup ───────────────────────────────────────
//
// A headless.Group — a real fieldset with a real legend — around the
// rendered leaves, plus the group's own message paragraph. The group
// is the field for its leaves: a group's error belongs here, never on
// each leaf, because Choice carries a hint only.

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
	// Error replaces Help with an error message.
	Error string
	// Required marks every leaf required, which is how HTML makes a
	// radio group required.
	Required bool
	ID       string
	Class    string

	// ExtraAttrs forwards additional attributes to the group's root
	// <fieldset> element. Keys the component owns are dropped: class
	// and id (use Class / ID), role (the fieldset is the native group
	// semantic; headless renders no role on it), aria-describedby
	// (wired to the group's message), and every data-fui-* key.
	ExtraAttrs html.Attrs
}

// RadioGroup renders a <fieldset> of radio buttons with a shared
// name, group-level legend, and optional help/error text.
func RadioGroup(cfg RadioGroupConfig) render.HTML {
	return renderToggleGroup(toggleGroupSpec{
		kind:     "RadioGroup",
		leafType: "radio",
		name:     cfg.Name,
		legend:   cfg.Legend,
		help:     cfg.Help,
		errText:  cfg.Error,
		id:       cfg.ID,
		class:    cfg.Class,
		required: cfg.Required,
		extra:    html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:  normalizeRadioOptions(cfg.Options),
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
	// Error replaces Help with an error message.
	Error string
	// Required marks every leaf required.
	Required bool
	ID       string
	Class    string

	// ExtraAttrs forwards additional attributes to the group's root
	// <fieldset> element, with the same owned-key drops as
	// RadioGroup's.
	ExtraAttrs html.Attrs
}

// CheckboxGroup renders a <fieldset> of checkboxes with a shared
// name, group-level legend, and optional help/error text.
func CheckboxGroup(cfg CheckboxGroupConfig) render.HTML {
	return renderToggleGroup(toggleGroupSpec{
		kind:     "CheckboxGroup",
		leafType: "checkbox",
		name:     cfg.Name,
		legend:   cfg.Legend,
		help:     cfg.Help,
		errText:  cfg.Error,
		id:       cfg.ID,
		class:    cfg.Class,
		required: cfg.Required,
		extra:    html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:  normalizeCheckboxOptions(cfg.Options),
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

func normalizeRadioOptions(in []RadioGroupOption) []toggleGroupOption {
	opts := make([]toggleGroupOption, len(in))
	for i, opt := range in {
		opts[i] = toggleGroupOption{value: opt.Value, label: opt.Label, checked: opt.Checked, disabled: opt.Disabled}
	}
	return opts
}

func normalizeCheckboxOptions(in []CheckboxGroupOption) []toggleGroupOption {
	opts := make([]toggleGroupOption, len(in))
	for i, opt := range in {
		opts[i] = toggleGroupOption{value: opt.Value, label: opt.Label, checked: opt.Checked, disabled: opt.Disabled}
	}
	return opts
}

// toggleGroupSpec is the shared body of the two group components: the
// common config fields plus the leaf input type that differs.
type toggleGroupSpec struct {
	kind     string // component name used in panic messages
	leafType string // "radio" or "checkbox"
	name     string
	legend   string
	help     string
	errText  string
	id       string
	class    string
	required bool
	extra    html.Attrs
	options  []toggleGroupOption
}

// renderToggleGroup is the single body behind RadioGroup and
// CheckboxGroup: each leaf renders through headless.Choice with its
// own id derived from the group's, the group's message paragraph (an
// error, else a hint — either/or, the family's shape) rides as the
// last child of the fieldset, and the fieldset's aria-describedby
// points at it.
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

	// extra arrives pre-sanitized (each group function routes
	// cfg.ExtraAttrs through html.SafeExtraAttrs; the contract test
	// requires it at the read site), then the described-by the group
	// itself owns lands on top of it.
	extra := spec.extra
	if extra == nil {
		extra = map[string]string{}
	}

	items := make([]render.HTML, 0, len(spec.options)+1)
	for i, opt := range spec.options {
		optID := id + "-" + slug(opt.value)
		if opt.value == "" {
			optID = fmt.Sprintf("%s-%d", id, i)
		}
		leaf := headless.Choice(headless.ChoiceProps{
			Type: spec.leafType, Name: spec.name, Value: opt.value, Label: opt.label,
			Checked: opt.checked, Disabled: opt.disabled, ID: optID,
			Extra: choiceLeafExtra(spec.required),
		}, withRootClass(choiceClasses, "fui-choice--"+spec.leafType))
		items = append(items, leaf)
	}
	// The group's message: error wins, else the hint. It is the
	// field's message — a group's error belongs on the group, never
	// on each leaf — and the fieldset's described-by names it.
	var msg string
	var hasMsg bool
	if spec.errText != "" {
		msg = string(render.Tag("p", map[string]string{
			"id":    id + "-error",
			"class": "fui-choice-group__error",
			"role":  "alert",
		}, render.Text(spec.errText)))
		extra["aria-describedby"] = id + "-error"
		hasMsg = true
	} else if spec.help != "" {
		msg = string(render.Tag("p", map[string]string{
			"id":    id + "-hint",
			"class": "fui-choice-group__hint",
		}, render.Text(spec.help)))
		extra["aria-describedby"] = id + "-hint"
		hasMsg = true
	}
	if hasMsg {
		items = append(items, render.HTML(msg))
	}

	return toggleStyle.WrapHTML(headless.Group(headless.GroupProps{
		Legend: spec.legend,
		// The mark a sighted reader looks for. What the browser
		// enforces is the required attribute on the leaves, which
		// choiceLeafExtra puts there; this is the cue beside it.
		Required: spec.required,
		ID:       id,
		Extra:    extra,
	}, withRootClass(choiceGroupClasses, spec.class), items...))
}

// choiceLeafExtra carries the group's Required onto each leaf's
// input: one required radio is how HTML makes the group required.
func choiceLeafExtra(required bool) html.Attrs {
	if !required {
		return nil
	}
	return html.Attrs{"required": ""}
}
