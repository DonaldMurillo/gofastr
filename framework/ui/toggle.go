package ui

import (
	"context"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/internal/idslug"
)

// ToggleConfig configures a Checkbox/Radio/Switch.
type ToggleConfig struct {
	// Ctx carries the render ID scope.
	Ctx context.Context
	// Name is the form-field name. Required.
	Name string
	// Label is the control's visible name. Required.
	Label string
	// LabelHidden hides a Checkbox's or Radio's label from view only.
	LabelHidden bool
	// ID is the input's id; empty derives Name, or Name-slug(Value) for a Radio.
	ID string
	// Value is the submitted value; a Radio requires one.
	Value    string
	Checked  bool
	Disabled bool
	Required bool
	Help     string
	// Error replaces Help and marks the input invalid.
	Error string
	// ExtraAttrs lands on the <input>; keys the component owns are dropped.
	ExtraAttrs html.Attrs
	Class      string
}

// Checkbox renders a single labelled checkbox.
func Checkbox(cfg ToggleConfig) render.HTML {
	return renderToggle("checkbox", cfg)
}

// Radio renders a single radio; radios sharing a Name form a group.
func Radio(cfg ToggleConfig) render.HTML {
	if cfg.Value == "" {
		panic("ui: Radio requires Value")
	}
	return renderToggle("radio", cfg)
}

// Switch renders a checkbox with role=switch, drawn as a toggle switch.
func Switch(cfg ToggleConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Switch requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Switch requires Label")
	}
	id := choiceID(cfg.Ctx, cfg.ID, "checkbox", cfg.Name, cfg.Value)
	classes := withRootClass(switchClasses, cfg.Class)
	if cfg.Error == "" && cfg.Help == "" {
		return toggleStyle.WrapHTML(headless.Switch(switchProps(cfg, id), classes))
	}
	p := switchProps(cfg, id)
	if cfg.Error != "" {
		p.Extra["aria-invalid"] = "true"
		p.Extra["aria-describedby"] = id + "-error"
	} else {
		p.Extra["aria-describedby"] = id + "-hint"
	}
	return toggleStyle.WrapHTML(erroredRun(
		headless.Own(headless.Switch(p, classes)),
		cfg.Help, cfg.Error, id))
}

func switchProps(cfg ToggleConfig, id string) headless.SwitchProps {
	extra := html.Attrs{}
	maps.Copy(extra, html.SafeExtraAttrs(cfg.ExtraAttrs))
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

func choiceID(ctx context.Context, callerID, inputType, name, value string) string {
	if inputType == "radio" && value != "" {
		name += "-" + idslug.Make(value)
	}
	return scopedControlID(ctx, callerID, name, "-hint", "-error")
}

func renderToggle(inputType string, cfg ToggleConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: " + inputType + " requires Name")
	}
	if cfg.Label == "" {
		panic("ui: " + inputType + " requires Label")
	}
	id := choiceID(cfg.Ctx, cfg.ID, inputType, cfg.Name, cfg.Value)
	hint := cfg.Help
	if cfg.Error != "" {
		hint = ""
	}
	extra := html.Attrs{}
	maps.Copy(extra, html.SafeExtraAttrs(cfg.ExtraAttrs))
	delete(extra, "for")
	if cfg.Required {
		extra["required"] = ""
	}
	if cfg.Error != "" {
		extra["aria-invalid"] = "true"
		extra["aria-describedby"] = id + "-error"
	}
	run := headless.Choice(headless.ChoiceProps{
		Type: inputType, Name: cfg.Name, Value: cfg.Value, Label: cfg.Label,
		Hint: hint, Checked: cfg.Checked, Disabled: cfg.Disabled,
		ID: id, Extra: extra,
	}, withRootClass(withRootClass(choiceTextClasses(cfg.LabelHidden), "fui-choice--"+inputType), cfg.Class))
	if cfg.Error == "" {
		return toggleStyle.WrapHTML(run)
	}
	return toggleStyle.WrapHTML(erroredRun(headless.Own(run), "", cfg.Error, id))
}

func choiceTextClasses(hidden bool) headless.Classes {
	if !hidden {
		return choiceClasses
	}
	out := maps.Clone(choiceClasses)
	out[headless.PartText] += " fui-visually-hidden"
	return out
}

func erroredRun(run render.HTML, helpText, errText, id string) render.HTML {
	switch {
	case errText != "":
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run,
			render.Tag("p", map[string]string{
				"id":                   id + "-error",
				"class":                "fui-choice-field__error",
				"data-cui-internal":    "",
				"data-hui-field-error": "",
			}, render.Text(errText)))
	case helpText != "":
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run,
			render.Tag("p", map[string]string{
				"id":                id + "-hint",
				"class":             "fui-choice-field__hint",
				"data-cui-internal": "",
			}, render.Text(helpText)))
	default:
		return render.Tag("div", map[string]string{"class": "fui-choice-field"}, run)
	}
}

// RadioGroupOption describes one radio button in a RadioGroup.
type RadioGroupOption struct {
	Value    string
	Label    string
	Checked  bool
	Disabled bool
}

// RadioGroupConfig configures a group of radio buttons.
type RadioGroupConfig struct {
	Ctx context.Context
	// Name is the radios' shared name. Required.
	Name string
	// Legend is the group's <legend>. Required.
	Legend  string
	Options []RadioGroupOption
	Help    string
	// Error replaces Help.
	Error string
	// Required marks every radio required.
	Required bool
	ID       string
	Class    string
	// ExtraAttrs lands on the <fieldset>; role and aria-describedby are dropped.
	ExtraAttrs html.Attrs
}

// RadioGroup renders a <fieldset> of radios sharing one name.
func RadioGroup(cfg RadioGroupConfig) render.HTML {
	return renderToggleGroup(toggleGroupSpec{
		kind:     "RadioGroup",
		ctx:      cfg.Ctx,
		leafType: "radio",
		name:     cfg.Name,
		legend:   cfg.Legend,
		help:     cfg.Help,
		errText:  cfg.Error,
		id:       cfg.ID,
		class:    cfg.Class,
		required: cfg.Required,
		extra:    html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:  normalizeToggleOptions(cfg.Options),
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
	Ctx context.Context
	// Name is the checkboxes' shared name. Required.
	Name string
	// Legend is the group's <legend>. Required.
	Legend  string
	Options []CheckboxGroupOption
	Help    string
	// Error replaces Help.
	Error string
	// Required marks every checkbox required.
	Required bool
	ID       string
	Class    string
	// ExtraAttrs lands on the <fieldset>, as RadioGroup's does.
	ExtraAttrs html.Attrs
}

// CheckboxGroup renders a <fieldset> of checkboxes sharing one name.
func CheckboxGroup(cfg CheckboxGroupConfig) render.HTML {
	return renderToggleGroup(toggleGroupSpec{
		kind:     "CheckboxGroup",
		ctx:      cfg.Ctx,
		leafType: "checkbox",
		name:     cfg.Name,
		legend:   cfg.Legend,
		help:     cfg.Help,
		errText:  cfg.Error,
		id:       cfg.ID,
		class:    cfg.Class,
		required: cfg.Required,
		extra:    html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-describedby"),
		options:  normalizeToggleOptions(cfg.Options),
	})
}

type toggleOption struct {
	Value    string
	Label    string
	Checked  bool
	Disabled bool
}

type toggleGroupOption struct {
	value    string
	label    string
	checked  bool
	disabled bool
}

func normalizeToggleOptions[T RadioGroupOption | CheckboxGroupOption](in []T) []toggleGroupOption {
	opts := make([]toggleGroupOption, len(in))
	for i, option := range in {
		opt := toggleOption(option)
		opts[i] = toggleGroupOption{value: opt.Value, label: opt.Label, checked: opt.Checked, disabled: opt.Disabled}
	}
	return opts
}

type toggleGroupSpec struct {
	ctx      context.Context
	kind     string
	leafType string
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

func renderToggleGroup(spec toggleGroupSpec) render.HTML {
	if spec.name == "" {
		panic("ui: " + spec.kind + " requires Name")
	}
	if spec.legend == "" {
		panic("ui: " + spec.kind + " requires Legend")
	}

	family := []string{"-hint", "-error"}
	for index := range spec.options {
		family = append(family, fmt.Sprintf("-%d", index))
	}
	id := scopedControlID(spec.ctx, spec.id, spec.name+"-group", family...)

	extra := spec.extra
	if extra == nil {
		extra = map[string]string{}
	}

	items := make([]render.HTML, 0, len(spec.options)+1)
	for index, opt := range spec.options {
		optID := fmt.Sprintf("%s-%d", id, index)
		leaf := headless.Own(headless.Choice(headless.ChoiceProps{
			Type: spec.leafType, Name: spec.name, Value: opt.value, Label: opt.label,
			Checked: opt.checked, Disabled: opt.disabled, ID: optID,
			Extra: choiceLeafExtra(spec.required),
		}, withRootClass(choiceClasses, "fui-choice--"+spec.leafType)))
		items = append(items, leaf)
	}
	var msg string
	var hasMsg bool
	if spec.errText != "" {
		msg = string(render.Tag("p", map[string]string{
			"id":                id + "-error",
			"class":             "fui-choice-group__error",
			"data-cui-internal": "",
		}, render.Text(spec.errText)))
		extra["aria-describedby"] = id + "-error"
		hasMsg = true
	} else if spec.help != "" {
		msg = string(render.Tag("p", map[string]string{
			"id":                id + "-hint",
			"class":             "fui-choice-group__hint",
			"data-cui-internal": "",
		}, render.Text(spec.help)))
		extra["aria-describedby"] = id + "-hint"
		hasMsg = true
	}
	if hasMsg {
		items = append(items, render.HTML(msg))
	}

	return toggleStyle.WrapHTML(headless.Group(headless.GroupProps{
		Legend:   spec.legend,
		Required: spec.required,
		ID:       id,
		Extra:    extra,
	}, withRootClass(choiceGroupClasses, spec.class), items...))
}

func choiceLeafExtra(required bool) html.Attrs {
	if !required {
		return nil
	}
	return html.Attrs{"required": ""}
}
