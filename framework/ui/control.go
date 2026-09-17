package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Control ──────────────────────────────────────────────────────
//
// The styled native control a FormField builder reaches for when none
// of the typed fields (TextField, NumberField, DateField) names the
// input type it needs: email, password, datetime-local, file, tel,
// url, search, hidden. It renders headless.Input dressed with this
// package's input classes and NOTHING else — no label, no hint, no
// error — because those belong to the field that builds it.

// ControlConfig configures a Control.
type ControlConfig struct {
	// Field is the wiring the enclosing FormField handed the builder
	// (the FieldControl its Input closure received). Required: a
	// control built without it is exactly the defect the builder
	// exists to prevent — an id the label does not point at, a
	// description that never arrives, an invalid state the control
	// does not carry. It supplies the id, the aria-describedby, the
	// invalid state and the required flag; those four never come from
	// anywhere else, because two sources for one fact is how they
	// drift.
	Field headless.FieldControl
	// Type is the input type: text (the default), email, password,
	// datetime-local, file, tel, url, search, hidden — any type the
	// browser knows.
	Type string
	// Name is the form-field name (required).
	Name string
	// Value, Placeholder and AutoComplete render their attributes.
	Value, Placeholder, AutoComplete string
	// Disabled disables the control.
	Disabled bool
	// Min, Max and Step are the numeric and date bounds, through
	// headless's Owned seam (which admits exactly those three keys).
	Min, Max, Step string
	// MinLength and MaxLength are the length constraints.
	MinLength, MaxLength int
	// Class appends to the control's own class.
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks, a
	// relation's data-rel-entity, pattern and title) onto the input.
	// Keys the control owns are dropped: class and id, type, name,
	// value, placeholder, autocomplete, minlength, maxlength, min,
	// max, step, required, disabled, aria-invalid and
	// aria-describedby.
	ExtraAttrs html.Attrs
}

// Control renders a styled native input, dressed with the input class
// map. The field sheet (ui-form-field) is what styles it, fetched by
// the field whose builder this control was built in.
func Control(cfg ControlConfig) render.HTML {
	if cfg.Field.ID == "" && cfg.Field.DescribedBy == "" && !cfg.Field.Invalid && !cfg.Field.Required {
		panic("ui: Control requires Field — the wiring a FormField hands its builder; a control built beside its field is the defect the builder exists to prevent")
	}
	if cfg.Name == "" {
		panic("ui: Control requires Name — a control with no name submits nothing")
	}
	owned := html.Attrs{}
	if cfg.Min != "" {
		owned["min"] = cfg.Min
	}
	if cfg.Max != "" {
		owned["max"] = cfg.Max
	}
	if cfg.Step != "" {
		owned["step"] = cfg.Step
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "name", "value", "placeholder", "autocomplete", "minlength", "maxlength",
		"min", "max", "step", "required", "disabled", "aria-invalid", "aria-describedby")
	if extra == nil {
		extra = html.Attrs{}
	}
	if cfg.AutoComplete != "" {
		extra["autocomplete"] = cfg.AutoComplete
	}
	if cfg.MinLength > 0 {
		extra["minlength"] = strconv.Itoa(cfg.MinLength)
	}
	if cfg.MaxLength > 0 {
		extra["maxlength"] = strconv.Itoa(cfg.MaxLength)
	}
	return inputHTML(headless.InputProps{
		Type:        cfg.Type,
		DescribedBy: cfg.Field.DescribedBy,
		Name:        cfg.Name,
		Value:       cfg.Value,
		Placeholder: cfg.Placeholder,
		Required:    cfg.Field.Required,
		Disabled:    cfg.Disabled,
		Invalid:     cfg.Field.Invalid,
		ID:          cfg.Field.ID,
		Extra:       extra,
		Owned:       owned,
	}, cfg.Class)
}

// inputHTML renders one headless.Input in this package's input
// classes, with a caller's class appended on the root part (never by
// mutating the shared map). The single place the typed fields and
// Control meet the input class map.
func inputHTML(p headless.InputProps, class string) render.HTML {
	return headless.Input(p, withRootClass(inputClasses, class))
}

// withRootClass returns a copy of the class map with class appended
// to the root part's classes. The shared maps are never mutated: a
// per-call class that landed in the package-level map would style
// every control on every page.
func withRootClass(m headless.Classes, class string) headless.Classes {
	if class == "" {
		return m
	}
	out := headless.Classes{}
	for k, v := range m {
		out[k] = v
	}
	out[headless.PartRoot] = out[headless.PartRoot] + " " + class
	return out
}
