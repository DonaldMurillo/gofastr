package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ControlConfig configures a Control.
type ControlConfig struct {
	// Field is the wiring the enclosing FormField handed its builder; it
	// alone supplies the id, description, invalid state and required flag.
	Field headless.FieldControl
	// Type is the input type; empty is text.
	Type string
	// Name is the form-field name. Required.
	Name                             string
	Value, Placeholder, AutoComplete string
	Disabled                         bool
	Min, Max, Step                   string
	MinLength, MaxLength             int
	// EnterKeyHint and InputMode set enterkeyhint and inputmode.
	EnterKeyHint, InputMode string
	// NoSpellCheck sets spellcheck="false" and autocapitalize="off".
	NoSpellCheck bool
	Class        string
	// ExtraAttrs lands on the input; keys the control owns are dropped.
	ExtraAttrs html.Attrs
}

// Control renders the styled native input a FormField builder uses when
// no typed field names its input type.
func Control(cfg ControlConfig) render.HTML {
	if cfg.Field.ID == "" {
		panic("ui: Control requires Field.ID — the wiring a FormField hands its builder; a control with no id is a label pointing at nothing, however much else it carries")
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
		"min", "max", "step", "enterkeyhint", "inputmode", "spellcheck", "autocapitalize",
		"required", "disabled", "aria-invalid", "aria-describedby")
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
	if cfg.EnterKeyHint != "" {
		extra["enterkeyhint"] = cfg.EnterKeyHint
	}
	if cfg.InputMode != "" {
		extra["inputmode"] = cfg.InputMode
	}
	if cfg.NoSpellCheck {
		extra["spellcheck"] = "false"
		extra["autocapitalize"] = "off"
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

func inputHTML(p headless.InputProps, class string) render.HTML {
	return headless.Input(p, withRootClass(inputClasses, class))
}

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
