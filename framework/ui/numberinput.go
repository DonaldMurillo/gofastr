package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// NumberInputConfig configures a NumberInput.
type NumberInputConfig struct {
	// Name is the form-field name. Required.
	Name string
	// Label is the accessible label. Required.
	Label string
	// Min and Max bound the value: both zero is unbounded, otherwise Min
	// is a floor even at zero and Max applies only when non-zero.
	Min int
	Max int
	// Step is the buttons' granularity; zero is 1.
	Step     int
	Value    int
	Disabled bool
	Required bool
	Help     string
	Error    string
	ID       string
	Class    string
	// ExtraAttrs lands on the root; keys the component owns are dropped.
	ExtraAttrs html.Attrs
	// Ctx resolves the buttons' labels; nil reads English.
	Ctx context.Context
}

var numberInputClasses = headless.Classes{
	headless.PartRoot:            "fui-number-input",
	headless.PartLabel:           "fui-number-input__label",
	headless.PartFieldRow:        "fui-number-input__row",
	headless.PartControl:         "fui-number-input__input",
	headless.PartNumberDecrement: "fui-number-input__decrement",
	headless.PartNumberIncrement: "fui-number-input__increment",
	headless.PartHint:            "fui-number-input__help",
	headless.PartError:           "fui-number-input__error",
}

// NumberInput renders a number field with explicit +/- buttons.
func NumberInput(cfg NumberInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: NumberInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: NumberInput requires Label")
	}
	var min, max *int
	if cfg.Min != 0 || cfg.Max != 0 {
		m := cfg.Min
		min = &m
	}
	if cfg.Max != 0 {
		m := cfg.Max
		max = &m
	}

	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(modifierClass("is-error", cfg.Error != "") +
		" " + modifierClass("is-disabled", cfg.Disabled) + " " + cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(rootClass)}}
	}

	return numberInputStyle.WrapHTML(headless.NumberInput(headless.NumberInputProps{
		Name:     cfg.Name,
		Label:    cfg.Label,
		Value:    strconv.Itoa(cfg.Value),
		Min:      min,
		Max:      max,
		Step:     cfg.Step,
		Required: cfg.Required,
		Disabled: cfg.Disabled,
		Help:     cfg.Help,
		Error:    cfg.Error,
		ID:       cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label",
			"type", "name", "step", "value", "min", "max", "disabled", "required",
			"aria-invalid", "aria-describedby"),
		Parts:   parts,
		Strings: StringsFor(cfg.Ctx),
		Ctx:     cfg.Ctx,
	}, numberInputClasses))
}

func modifierClass(name string, on bool) string {
	if !on {
		return ""
	}
	return name
}

var numberInputStyle = registry.RegisterStyle("ui-number-input", numberInputCSS)

func numberInputCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-number-input"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-number-input"] .fui-number-input__label {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-number-input"] .fui-number-input__row {
  display: inline-flex;
  align-items: stretch;
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
  width: fit-content;
}
/* The row is the control: typing in the value rings the whole group. */
[data-cui-comp="ui-number-input"] .fui-number-input__row:has(.fui-number-input__input:focus-visible) {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-number-input"] .fui-number-input__input {
  appearance: textfield;
  -moz-appearance: textfield;
  border: 0;
  background: transparent;
  text-align: center;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  font-variant-numeric: tabular-nums;
  color: var(--color-text, #09090B);
  min-block-size: var(--spacing-touch-target, 44px);
  width: 5ch;
  padding: 0;
}
[data-cui-comp="ui-number-input"] .fui-number-input__input::-webkit-outer-spin-button,
[data-cui-comp="ui-number-input"] .fui-number-input__input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
[data-cui-comp="ui-number-input"] .fui-number-input__input:focus {
  outline: none;
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) {
  [data-cui-comp="ui-number-input"] .fui-number-input__input { font-size: var(--text-base, 1rem); }
}
[data-cui-comp="ui-number-input"] .fui-number-input__decrement,
[data-cui-comp="ui-number-input"] .fui-number-input__increment {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* WCAG 2.5.5 — each step button is independently tappable. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  /* Outline-button segments: hairline dividers, no gray fill. */
  background: transparent;
  border: 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-normal);
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  user-select: none;
}
[data-cui-comp="ui-number-input"] .fui-number-input__decrement { border-inline-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7); }
[data-cui-comp="ui-number-input"] .fui-number-input__increment { border-inline-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7); }
[data-cui-comp="ui-number-input"] .fui-number-input__decrement:hover,
[data-cui-comp="ui-number-input"] .fui-number-input__increment:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-number-input"] .fui-number-input__decrement:focus-visible,
[data-cui-comp="ui-number-input"] .fui-number-input__increment:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
}
[data-cui-comp="ui-number-input"] .fui-number-input__decrement:disabled,
[data-cui-comp="ui-number-input"] .fui-number-input__increment:disabled {
  opacity: var(--opacity-disabled, 0.5);
  cursor: not-allowed;
}
[data-cui-comp="ui-number-input"] .fui-number-input__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-number-input"] .fui-number-input__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #B91C1C);
}
[data-cui-comp="ui-number-input"].is-error .fui-number-input__row {
  border-color: var(--color-danger, #B91C1C);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-danger, #B91C1C);
}`
}
