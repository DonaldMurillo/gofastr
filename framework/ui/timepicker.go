package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── TimePicker ─────────────────────────────────────────────────────
//
// Styled wrapper around <input type="time">. Browser handles the
// native time UI; we own the label, the 44px touch-target, and the
// focus ring. Twin of ColorPicker — both wrap a native picker.

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
	cls := "ui-time-picker"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":       "time",
		"name":       cfg.Name,
		"id":         id,
		"class":      "ui-time-picker__input",
		"aria-label": cfg.Label,
	}
	if cfg.Value != "" {
		inputAttrs["value"] = cfg.Value
	}
	if cfg.Min != "" {
		inputAttrs["min"] = cfg.Min
	}
	if cfg.Max != "" {
		inputAttrs["max"] = cfg.Max
	}
	if cfg.Step > 0 {
		// browsers spec: step in seconds.
		inputAttrs["step"] = strconv.Itoa(cfg.Step)
	}
	if cfg.Required {
		inputAttrs["required"] = ""
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		inputAttrs["aria-describedby"] = id + "-help"
	}

	children := []render.HTML{
		render.Tag("label", map[string]string{
			"for":   id,
			"class": "ui-time-picker__label",
		}, render.Text(cfg.Label)),
		render.Tag("input", inputAttrs),
	}
	if cfg.Error != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id":    id + "-error",
			"class": "ui-time-picker__error",
			"role":  "alert",
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id":    id + "-help",
			"class": "ui-time-picker__help",
		}, render.Text(cfg.Help)))
	}

	return timePickerStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

var timePickerStyle = registry.RegisterStyle("ui-time-picker", timePickerCSS)

func timePickerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-time-picker"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
  max-inline-size: 16rem;
}
[data-fui-comp="ui-time-picker"] .ui-time-picker__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-time-picker"] .ui-time-picker__input {
  font: inherit;
  font-size: 0.95rem;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 10px var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-time-picker"] .ui-time-picker__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-time-picker"] .ui-time-picker__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-time-picker"] .ui-time-picker__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-time-picker"].is-error .ui-time-picker__input {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
[data-fui-comp="ui-time-picker"].is-disabled .ui-time-picker__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
