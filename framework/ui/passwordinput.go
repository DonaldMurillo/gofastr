package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── PasswordInput ──────────────────────────────────────────────────
//
// Password input with a toggle button that flips between hidden and
// visible text. SSR renders the password field in its hidden state;
// the runtime JS module (core-ui/runtime/src/passwordinput.js) wires
// the toggle button to flip input type between "password" and "text".

// PasswordInputConfig configures a PasswordInput.
type PasswordInputConfig struct {
	// Name is the form-field name (required).
	Name string
	// ID is the input element's id (required).
	ID string
	// Placeholder renders the native placeholder.
	Placeholder string
	// Required marks the field required.
	Required bool
	// Autocomplete sets the autocomplete attribute (e.g. "current-password", "new-password").
	Autocomplete string
	// Error overrides with an error message + aria-invalid.
	Error string
	// Class adds extra CSS classes to the wrapper.
	Class string
	// Attrs lets callers attach additional attributes.
	ExtraAttrs map[string]string

	// Ctx carries the per-request context used to resolve i18n strings
	// (show/hide toggle aria-label). When nil, context.Background() is
	// used and English fallbacks are returned — preserving today's behaviour.
	Ctx context.Context
}

// PasswordInput renders a password field with a show/hide toggle button.
func PasswordInput(cfg PasswordInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: PasswordInput requires Name")
	}
	if cfg.ID == "" {
		panic("ui: PasswordInput requires ID")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	cls := "ui-password-input"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":  "password",
		"name":  cfg.Name,
		"id":    cfg.ID,
		"class": "ui-password-input__input",
	}
	if cfg.Placeholder != "" {
		inputAttrs["placeholder"] = cfg.Placeholder
	}
	if cfg.Required {
		inputAttrs["required"] = ""
	}
	if cfg.Autocomplete != "" {
		inputAttrs["autocomplete"] = cfg.Autocomplete
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = cfg.ID + "-error"
	}
	for k, v := range cfg.ExtraAttrs {
		inputAttrs[k] = v
	}
	// Protect critical attrs from Attrs override.
	inputAttrs["type"] = "password"
	inputAttrs["name"] = cfg.Name
	inputAttrs["id"] = cfg.ID

	toggleAttrs := map[string]string{
		"type":         "button",
		"class":        "ui-password-input__toggle",
		"aria-label":   i18nui.T(ctx, i18nui.KeyPasswordInputShow),
		"aria-pressed": "false",
	}

	children := []render.HTML{
		render.VoidTag("input", inputAttrs),
		render.Tag("button", toggleAttrs, render.Text("⊙")),
	}

	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:         cfg.ID + "-error",
			Class:      "ui-password-input__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	}

	wrapper := render.Tag("div",
		map[string]string{"class": cls},
		children...)

	return passwordInputStyle.WrapHTML(wrapper)
}

var passwordInputStyle = registry.RegisterStyle("ui-password-input", passwordInputCSS)

func passwordInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-password-input"] {
  display: flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
}
[data-fui-comp="ui-password-input"] .ui-password-input__input {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 0.95rem);
  padding: 10px var(--spacing-md, 12px);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-password-input"] .ui-password-input__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-password-input"] .ui-password-input__toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  background: var(--color-surface-soft, #F4F4F5);
  border: 0;
  border-left: 1px solid var(--color-border, #E4E4E7);
  font-size: var(--text-lg, 1.1rem);
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  user-select: none;
}
[data-fui-comp="ui-password-input"] .ui-password-input__toggle:hover {
  background: var(--color-border, #E4E4E7);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-password-input"] .ui-password-input__toggle:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-password-input"].is-error {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
[data-fui-comp="ui-password-input"] .ui-password-input__error {
  margin: 0;
  font-size: var(--text-sm, 0.85rem);
  color: var(--color-danger, #DC2626);
}`
}
