package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── NumberInput / Stepper ──────────────────────────────────────────
//
// Native <input type="number"> flanked by explicit −/+ buttons. Why:
// the spinner arrows shipped by browsers are tiny, hidden on touch,
// and disabled when type=number is in a form-validation error state.
// Explicit buttons are easier to hit, more discoverable, and we can
// theme + size them. Buttons fire a runtime increment that respects
// Step, Min, Max, and dispatches an `input` event so existing
// form-RPC pipelines see the change.

// NumberInputConfig configures a NumberInput.
type NumberInputConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required, used as <label for=…>).
	Label string
	// Min / Max bound the value. When both 0, no client-side bound is
	// applied (server is still authoritative).
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
	Error      string
	ID         string
	Class      string
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the Decrement and
	// Increment aria labels. When nil, English fallbacks apply.
	Ctx context.Context
}

// NumberInput renders a number field with explicit +/- buttons.
func NumberInput(cfg NumberInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: NumberInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: NumberInput requires Label")
	}

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	step := cfg.Step
	if step == 0 {
		step = 1
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-number-input"
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
		"type":  "number",
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-number-input__input",
		"step":  strconv.Itoa(step),
		"value": strconv.Itoa(cfg.Value),
	}
	if cfg.Min != 0 || cfg.Max != 0 {
		inputAttrs["min"] = strconv.Itoa(cfg.Min)
		inputAttrs["max"] = strconv.Itoa(cfg.Max)
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	if cfg.Required {
		inputAttrs["required"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		inputAttrs["aria-describedby"] = id + "-help"
	}
	for k, v := range cfg.ExtraAttrs {
		inputAttrs[k] = v
	}

	minusAttrs := map[string]string{
		"type":                 "button",
		"class":                "ui-number-input__step ui-number-input__step--minus",
		"aria-label":           i18nui.TVars(ctx, i18nui.KeyNumberDecrement, map[string]string{"label": cfg.Label}),
		"data-fui-number-step": "-" + strconv.Itoa(step),
		"data-fui-number-for":  id,
	}
	plusAttrs := map[string]string{
		"type":                 "button",
		"class":                "ui-number-input__step ui-number-input__step--plus",
		"aria-label":           i18nui.TVars(ctx, i18nui.KeyNumberIncrement, map[string]string{"label": cfg.Label}),
		"data-fui-number-step": strconv.Itoa(step),
		"data-fui-number-for":  id,
	}
	if cfg.Disabled {
		minusAttrs["disabled"] = ""
		plusAttrs["disabled"] = ""
	}

	row := render.Tag("div", map[string]string{"class": "ui-number-input__row"},
		render.Tag("button", minusAttrs, render.Text("−")),
		render.Tag("input", inputAttrs),
		render.Tag("button", plusAttrs, render.Text("+")),
	)

	children := []render.HTML{
		render.Tag("label", map[string]string{"for": id, "class": "ui-number-input__label"},
			render.Text(cfg.Label)),
		row,
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:         id + "-error",
			Class:      "ui-number-input__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-number-input__help",
		}, render.Text(cfg.Help)))
	}

	return numberInputStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

var numberInputStyle = registry.RegisterStyle("ui-number-input", numberInputCSS)

func numberInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-number-input"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-number-input"] .ui-number-input__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-number-input"] .ui-number-input__row {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
  width: fit-content;
}
[data-fui-comp="ui-number-input"] .ui-number-input__input {
  appearance: textfield;
  -moz-appearance: textfield;
  border: 0;
  background: transparent;
  text-align: center;
  font: inherit;
  font-size: var(--text-base, 0.95rem);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
  width: 5ch;
  padding: 0;
}
[data-fui-comp="ui-number-input"] .ui-number-input__input::-webkit-outer-spin-button,
[data-fui-comp="ui-number-input"] .ui-number-input__input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
[data-fui-comp="ui-number-input"] .ui-number-input__input:focus {
  outline: none;
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-number-input"] .ui-number-input__step {
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
[data-fui-comp="ui-number-input"] .ui-number-input__step:hover {
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-number-input"] .ui-number-input__step:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-number-input"] .ui-number-input__step:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
[data-fui-comp="ui-number-input"] .ui-number-input__help {
  margin: 0;
  font-size: var(--text-sm, 0.85rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-number-input"] .ui-number-input__error {
  margin: 0;
  font-size: var(--text-sm, 0.85rem);
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-number-input"].is-error .ui-number-input__row {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}`
}
