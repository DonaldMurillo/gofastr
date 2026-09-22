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

// ─── NumberInput / Stepper ──────────────────────────────────────────
//
// Native <input type="number"> flanked by explicit −/+ buttons, over
// headless.NumberInput. Why the buttons exist: the spinner arrows
// shipped by browsers are tiny, hidden on touch, and disabled when
// type=number is in a form-validation error state. The module steps
// the value inside the declared bounds and dispatches an `input` event
// so existing form-RPC pipelines see the change; without it the field
// is still a normal named number control.

// NumberInputConfig configures a NumberInput.
type NumberInputConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required, used as <label for=…>).
	Label string
	// Min / Max bound the value. When both 0, no client-side bound is
	// applied (server is still authoritative). When either is set, Min
	// is a real floor even at 0 (Max: 10 means 0..10); a caller that
	// allows negatives sets Min explicitly. Max is emitted only when
	// non-zero, so Min: 1 alone never fabricates an empty 1..0 range.
	Min int
	Max int
	// Step is the +/- button granularity. Default 1.
	Step int
	// Value is the initial value.
	Value int
	// Disabled disables interaction.
	Disabled bool
	// Required marks the field required.
	Required bool
	// Help renders supporting text under the field.
	Help string
	// Error overrides Help with an error message.
	Error string
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the Decrement
	// and Increment aria labels. When nil, English fallbacks apply.
	Ctx context.Context
}

// numberInputClasses dresses headless.NumberInput's parts in this
// package's own vocabulary — the names the registered ui-number-input
// sheet matches.
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

	// The root's modifier classes travel as part attrs, which append
	// to the class map's own root class rather than replacing it.
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
	}, numberInputClasses))
}

// modifierClass returns the class when on, "" when off, so a caller's
// own Class and the component's state classes merge in one string.
func modifierClass(name string, on bool) string {
	if !on {
		return ""
	}
	return name
}

var numberInputStyle = registry.RegisterStyle("ui-number-input", numberInputCSS)

func numberInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-number-input"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-number-input"] .fui-number-input__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-number-input"] .fui-number-input__row {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
  width: fit-content;
}
[data-fui-comp="ui-number-input"] .fui-number-input__input {
  appearance: textfield;
  -moz-appearance: textfield;
  border: 0;
  background: transparent;
  text-align: center;
  font: inherit;
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
  width: 5ch;
  padding: 0;
}
[data-fui-comp="ui-number-input"] .fui-number-input__input::-webkit-outer-spin-button,
[data-fui-comp="ui-number-input"] .fui-number-input__input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
[data-fui-comp="ui-number-input"] .fui-number-input__input:focus {
  outline: none;
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-number-input"] .fui-number-input__decrement,
[data-fui-comp="ui-number-input"] .fui-number-input__increment {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* WCAG 2.5.5 — each step button is independently tappable. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  background: var(--color-surface-soft, #F4F4F5);
  border: 0;
  font-size: var(--text-xl, 1.25rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
  cursor: pointer;
  user-select: none;
}
[data-fui-comp="ui-number-input"] .fui-number-input__decrement:hover,
[data-fui-comp="ui-number-input"] .fui-number-input__increment:hover {
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-number-input"] .fui-number-input__decrement:focus-visible,
[data-fui-comp="ui-number-input"] .fui-number-input__increment:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-number-input"] .fui-number-input__decrement:disabled,
[data-fui-comp="ui-number-input"] .fui-number-input__increment:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
[data-fui-comp="ui-number-input"] .fui-number-input__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-number-input"] .fui-number-input__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-number-input"].is-error .fui-number-input__row {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}`
}
