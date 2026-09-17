package ui

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TextFieldConfig configures a labelled native text field. The wrapper owns
// label association, help/error ARIA wiring, and the common typed attributes;
// for the input types this field does not name (email, password,
// datetime-local, file, tel, url, search) use ui.Control inside a
// FormField builder.
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, pattern and title) onto the field's <input>.
	// Keys the input owns are dropped: class and id (use ID),
	// data-fui-*, type, name, value, placeholder, autocomplete,
	// minlength, maxlength, required, disabled, aria-invalid, and
	// aria-describedby.
	ExtraAttrs html.Attrs
}

// TextField renders a FormField containing an input[type=text].
func TextField(cfg TextFieldConfig) render.HTML {
	id := fieldID("TextField", cfg.Name, cfg.Label, cfg.ID)
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "name", "value", "placeholder", "autocomplete", "minlength", "maxlength",
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
	return typedFormField(cfg.Label, cfg.Name, id, "text", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, cfg.Disabled, extra, nil)
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) onto the field's <input>. Keys the input owns
	// are dropped: class and id (use ID), data-fui-*, type, name,
	// value, placeholder, min, max, step, required, disabled,
	// aria-invalid, and aria-describedby.
	ExtraAttrs html.Attrs
}

// NumberField renders a FormField containing an input[type=number]. For the
// larger touch-friendly +/- control, use NumberInput instead.
func NumberField(cfg NumberFieldConfig) render.HTML {
	id := fieldID("NumberField", cfg.Name, cfg.Label, cfg.ID)
	owned := html.Attrs{}
	for name, value := range map[string]*float64{"min": cfg.Min, "max": cfg.Max, "step": cfg.Step} {
		if value != nil {
			owned[name] = strconv.FormatFloat(*value, 'f', -1, 64)
		}
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "name", "value", "placeholder", "min", "max", "step",
		"required", "disabled", "aria-invalid", "aria-describedby")
	return typedFormField(cfg.Label, cfg.Name, id, "number", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, cfg.Disabled, extra, owned)
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) onto the field's <input>. Keys the input owns
	// are dropped: class and id (use ID), data-fui-*, type, name,
	// value, placeholder, min, max, required, disabled,
	// aria-invalid, and aria-describedby.
	ExtraAttrs html.Attrs
}

// DateField renders a FormField containing an input[type=date].
func DateField(cfg DateFieldConfig) render.HTML {
	id := fieldID("DateField", cfg.Name, cfg.Label, cfg.ID)
	owned := html.Attrs{}
	if cfg.Min != "" {
		owned["min"] = cfg.Min
	}
	if cfg.Max != "" {
		owned["max"] = cfg.Max
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "name", "value", "placeholder", "min", "max",
		"required", "disabled", "aria-invalid", "aria-describedby")
	return typedFormField(cfg.Label, cfg.Name, id, "date", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, cfg.Class, cfg.Required, cfg.Disabled, extra, owned)
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

// typedFormField renders one labelled input through FormField's builder:
// the field hands its wiring to the closure, the closure hands it to
// headless.Input, and the id scheme, the description chain and the
// invalid state arrive at the control by construction. owned carries
// the type-specific bounds (min, max, step) through headless's Owned
// seam, where nothing else can widen them.
func typedFormField(label, name, id, inputType, value, placeholder, help, fieldError, class string, required, disabled bool, extra, owned html.Attrs) render.HTML {
	return FormField(FormFieldConfig{
		Label: label, For: id, Help: help, Error: fieldError,
		Required: required, Class: class,
		Input: func(c headless.FieldControl) render.HTML {
			return inputHTML(headless.InputProps{
				Type:        inputType,
				DescribedBy: c.DescribedBy,
				Name:        name,
				Value:       value,
				Placeholder: placeholder,
				Disabled:    disabled,
				ID:          c.ID,
				Invalid:     c.Invalid,
				Required:    c.Required,
				Extra:       extra,
				Owned:       owned,
			}, "")
		},
	})
}
