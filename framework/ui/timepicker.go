package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── TimePicker ─────────────────────────────────────────────────────
//
// A styled native time input over headless.Field + headless.Input:
// the browser owns the time UI; this layer owns the label, the 44px
// touch target and the focus ring. The input's type and accessible
// label do not change.

// TimePickerConfig configures a TimePicker.
type TimePickerConfig struct {
	// Name is the form field name (required).
	Name string
	// Label is the accessible label (required).
	Label string
	// Value is the initial value in HH:MM (24-hour) format. Empty
	// means no preselection.
	Value string
	// Min / Max bound the picker (e.g. "09:00", "17:00"). Empty
	// leaves them unset.
	Min  string
	Max  string
	Step int // step in seconds (default = 60). 1 → seconds visible.
	// Required marks the input required.
	Required bool
	// Disabled disables interaction.
	Disabled bool
	// Help renders supporting text under the picker.
	Help string
	// Error overrides Help with an error message + aria-invalid.
	Error string
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes to the input element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-cui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs
}

// timePickerClasses dresses headless.Field's parts; the control
// inside gets timePickerInputClasses, because Input renders through
// its own root part and the input's class is not the field's.
var timePickerClasses = headless.Classes{
	headless.PartRoot:  "fui-time-picker",
	headless.PartLabel: "fui-time-picker__label",
	headless.PartHint:  "fui-time-picker__help",
	headless.PartError: "fui-time-picker__error",
}

var timePickerInputClasses = headless.Classes{
	headless.PartRoot: "fui-time-picker__input",
}

// TimePicker renders a styled native time input with a label.
func TimePicker(cfg TimePickerConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: TimePicker requires Name")
	}
	if cfg.Label == "" {
		panic("ui: TimePicker requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(modifierClass("is-error", cfg.Error != "") +
		" " + modifierClass("is-disabled", cfg.Disabled) + " " + cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(rootClass)}}
	}

	// The type-specific attributes travel through Input's Owned seam —
	// the one place a numeric control's bounds may reach the input
	// without a caller being able to widen them.
	owned := html.Attrs{}
	if cfg.Min != "" {
		owned["min"] = cfg.Min
	}
	if cfg.Max != "" {
		owned["max"] = cfg.Max
	}
	if cfg.Step > 0 {
		// browsers spec: step in seconds.
		owned["step"] = strconv.Itoa(cfg.Step)
	}

	return timePickerStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label:      cfg.Label,
		For:        id,
		Hint:       cfg.Help,
		Error:      cfg.Error,
		Required:   cfg.Required,
		Parts:      parts,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
	}, timePickerClasses, func(c headless.FieldControl) render.HTML {
		// TimePicker builds this input itself from cfg's strings/bools;
		// it is never content TimePicker's own caller handed in, so the
		// input's root — headless.Field sees it only as its build
		// closure's caller-content return — takes the mark here.
		return headless.Own(headless.Input(headless.InputProps{
			Type:        "time",
			Name:        cfg.Name,
			ID:          c.ID,
			Value:       cfg.Value,
			Required:    c.Required,
			Disabled:    cfg.Disabled,
			Invalid:     c.Invalid,
			DescribedBy: c.DescribedBy,
			AriaLabel:   cfg.Label,
			Owned:       owned,
		}, timePickerInputClasses))
	}))
}

var timePickerStyle = registry.RegisterStyle("ui-time-picker", timePickerCSS)

func timePickerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-time-picker"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-time-picker"] .fui-time-picker__label {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-cui-comp="ui-time-picker"] .fui-time-picker__input {
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 calc(var(--spacing-sm, 4px) * 3);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-cui-comp="ui-time-picker"] .fui-time-picker__input:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) {
  [data-cui-comp="ui-time-picker"] .fui-time-picker__input { font-size: var(--text-base, 1rem); }
}
[data-cui-comp="ui-time-picker"].is-error .fui-time-picker__input {
  border-color: var(--color-danger, #DC2626);
}
[data-cui-comp="ui-time-picker"] .fui-time-picker__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-time-picker"] .fui-time-picker__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #DC2626);
}`
}
