package ui

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TextFieldConfig configures a labelled native text field. The wrapper owns
// label association, help/error ARIA wiring, and the common typed attributes;
// use html.Input directly only when a less common input type is required.
type TextFieldConfig struct {
	Name         string
	Label        string
	ID           string
	Value        string
	Placeholder  string
	AutoComplete string
	Help         string
	Error        string
	Class        string
	Required     bool
	Disabled     bool
	MinLength    int
	MaxLength    int
}

// TextField renders a FormField containing an input[type=text].
func TextField(cfg TextFieldConfig) render.HTML {
	id := fieldID("TextField", cfg.Name, cfg.Label, cfg.ID)
	attrs := fieldAttrs(cfg.Required, cfg.Disabled)
	if cfg.AutoComplete != "" {
		attrs["autocomplete"] = cfg.AutoComplete
	}
	if cfg.MinLength > 0 {
		attrs["minlength"] = strconv.Itoa(cfg.MinLength)
	}
	if cfg.MaxLength > 0 {
		attrs["maxlength"] = strconv.Itoa(cfg.MaxLength)
	}
	return typedFormField(cfg.Label, cfg.Name, id, "text", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, attrs)
}

// NumberFieldConfig configures a labelled native number field. Pointer bounds
// distinguish an explicit zero from an omitted constraint.
type NumberFieldConfig struct {
	Name        string
	Label       string
	ID          string
	Value       string
	Placeholder string
	Help        string
	Error       string
	Class       string
	Required    bool
	Disabled    bool
	Min         *float64
	Max         *float64
	Step        *float64
}

// NumberField renders a FormField containing an input[type=number]. For the
// larger touch-friendly +/- control, use NumberInput instead.
func NumberField(cfg NumberFieldConfig) render.HTML {
	id := fieldID("NumberField", cfg.Name, cfg.Label, cfg.ID)
	attrs := fieldAttrs(cfg.Required, cfg.Disabled)
	for name, value := range map[string]*float64{"min": cfg.Min, "max": cfg.Max, "step": cfg.Step} {
		if value != nil {
			attrs[name] = strconv.FormatFloat(*value, 'f', -1, 64)
		}
	}
	return typedFormField(cfg.Label, cfg.Name, id, "number", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, attrs)
}

// DateFieldConfig configures a labelled native date field. Min, Max, and Value
// use the HTML date format (YYYY-MM-DD); browsers enforce the concrete value.
type DateFieldConfig struct {
	Name        string
	Label       string
	ID          string
	Value       string
	Placeholder string
	Help        string
	Error       string
	Class       string
	Required    bool
	Disabled    bool
	Min         string
	Max         string
}

// DateField renders a FormField containing an input[type=date].
func DateField(cfg DateFieldConfig) render.HTML {
	id := fieldID("DateField", cfg.Name, cfg.Label, cfg.ID)
	attrs := fieldAttrs(cfg.Required, cfg.Disabled)
	if cfg.Min != "" {
		attrs["min"] = cfg.Min
	}
	if cfg.Max != "" {
		attrs["max"] = cfg.Max
	}
	return typedFormField(cfg.Label, cfg.Name, id, "date", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, attrs)
}

func fieldID(api, name, label, id string) string {
	if name == "" {
		panic(fmt.Sprintf("ui: %s requires Name", api))
	}
	if label == "" {
		panic(fmt.Sprintf("ui: %s requires Label", api))
	}
	if id == "" {
		return name
	}
	return id
}

func fieldAttrs(required, disabled bool) html.Attrs {
	attrs := html.Attrs{}
	if required {
		attrs["required"] = ""
	}
	if disabled {
		attrs["disabled"] = ""
	}
	return attrs
}

func typedFormField(label, name, id, inputType, value, placeholder, help, fieldError, class string, required bool, attrs html.Attrs) render.HTML {
	input := html.Input(html.InputConfig{
		Type: inputType, Name: name, ID: id, Value: value,
		Placeholder: placeholder, ExtraAttrs: attrs,
	})
	return FormField(FormFieldConfig{
		Label: label, For: id, Help: help, Error: fieldError,
		Required: required, Input: input, Class: class,
	})
}
