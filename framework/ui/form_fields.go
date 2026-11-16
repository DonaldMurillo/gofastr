package ui

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TextFieldConfig configures a TextField; other input types use Control.
type TextFieldConfig struct {
	// Ctx carries the render ID scope.
	Ctx          context.Context
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

	// ExtraAttrs lands on the <input>; keys the input owns are dropped.
	ExtraAttrs html.Attrs
	// LabelHidden hides the label from view but not from assistive tech.
	LabelHidden bool
}

// TextField renders a FormField containing an input[type=text].
func TextField(cfg TextFieldConfig) render.HTML {
	id := fieldID(cfg.Ctx, "TextField", cfg.Name, cfg.Label, cfg.ID)
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
		cfg.Help, cfg.Error, labelHiddenClass(cfg.LabelHidden, cfg.Class), cfg.Required, cfg.Disabled, extra, nil)
}

// NumberFieldConfig configures a NumberField; a nil bound is omitted, a zero one is not.
type NumberFieldConfig struct {
	Ctx         context.Context
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

	ExtraAttrs  html.Attrs
	LabelHidden bool
}

// NumberField renders a FormField containing an input[type=number].
func NumberField(cfg NumberFieldConfig) render.HTML {
	id := fieldID(cfg.Ctx, "NumberField", cfg.Name, cfg.Label, cfg.ID)
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
		cfg.Help, cfg.Error, labelHiddenClass(cfg.LabelHidden, cfg.Class), cfg.Required, cfg.Disabled, extra, owned)
}

// DateFieldConfig configures a DateField; Min, Max and Value are YYYY-MM-DD.
type DateFieldConfig struct {
	Ctx         context.Context
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

	ExtraAttrs  html.Attrs
	LabelHidden bool
}

// DateField renders a FormField containing an input[type=date].
func DateField(cfg DateFieldConfig) render.HTML {
	id := fieldID(cfg.Ctx, "DateField", cfg.Name, cfg.Label, cfg.ID)
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
		cfg.Help, cfg.Error, labelHiddenClass(cfg.LabelHidden, cfg.Class), cfg.Required, cfg.Disabled, extra, owned)
}

// DateTimeFieldConfig configures a DateTimeField; values read 2026-07-22T09:30.
type DateTimeFieldConfig struct {
	Ctx         context.Context
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
	// Step is the precision in seconds; zero, negative or infinite draws none.
	Step float64

	ExtraAttrs  html.Attrs
	LabelHidden bool
}

// DateTimeField renders a FormField containing an input[type=datetime-local].
func DateTimeField(cfg DateTimeFieldConfig) render.HTML {
	id := fieldID(cfg.Ctx, "DateTimeField", cfg.Name, cfg.Label, cfg.ID)
	owned := html.Attrs{}
	if cfg.Min != "" {
		owned["min"] = cfg.Min
	}
	if cfg.Max != "" {
		owned["max"] = cfg.Max
	}
	if cfg.Step > 0 && !math.IsInf(cfg.Step, 1) {
		owned["step"] = strconv.FormatFloat(cfg.Step, 'f', -1, 64)
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "name", "value", "placeholder", "min", "max", "step",
		"required", "disabled", "aria-invalid", "aria-describedby")
	return typedFormField(cfg.Label, cfg.Name, id, "datetime-local", cfg.Value, cfg.Placeholder,
		cfg.Help, cfg.Error, labelHiddenClass(cfg.LabelHidden, cfg.Class), cfg.Required, cfg.Disabled, extra, owned)
}

func labelHiddenClass(hidden bool, class string) string {
	return strings.TrimSpace(modifierClass("fui-field--label-hidden", hidden) + " " + class)
}

func fieldID(ctx context.Context, api, name, label, id string) string {
	if name == "" {
		panic(fmt.Sprintf("ui: %s requires Name", api))
	}
	if label == "" {
		panic(fmt.Sprintf("ui: %s requires Label", api))
	}
	return scopedControlID(ctx, id, name, "-hint", "-error")
}

func scopedControlID(ctx context.Context, explicit, derived string, family ...string) string {
	if explicit != "" {
		render.ReserveID(ctx, explicit, family...)
		return explicit
	}
	return render.UniqueID(ctx, derived, family...)
}

func typedFormField(label, name, id, inputType, value, placeholder, help, fieldError, class string, required, disabled bool, extra, owned html.Attrs) render.HTML {
	return FormField(FormFieldConfig{
		Label: label, For: id, Help: help, Error: fieldError,
		Required: required, Class: class,
		Input: func(c headless.FieldControl) render.HTML {
			return headless.Own(inputHTML(headless.InputProps{
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
			}, ""))
		},
	})
}
