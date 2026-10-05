package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── PasswordInput ──────────────────────────────────────────────────
//
// The affix-shell password control, rendered through headless.Password:
// a div carrying data-hui-affix with a borderless input and a reveal
// button inside, the shell owning the one border. The headless
// behaviour module (not a component of its own) binds the reveal
// button's data-hui-reveal: it retypes the input, swaps the button's
// visible word and its accessible name from the four data-hui-*
// attributes, and keeps the caret where the reader left it. SSR ships
// the hidden state.
//
// The component is a control, not a field: it renders no message of
// its own. Wrap it in a FormField and let the field own the error;
// this is what the Field wiring below carries.

// PasswordInputConfig configures a PasswordInput.
type PasswordInputConfig struct {
	// Name is the form-field name (required).
	Name string
	// ID is the input element's id — the id a surrounding label's
	// for= points at. Required unless Field carries one.
	ID string
	// Placeholder renders the native placeholder.
	Placeholder string
	// Required marks the field required.
	Required bool
	// Autocomplete sets the autocomplete attribute (e.g. "current-password", "new-password").
	Autocomplete string
	// Class adds extra CSS classes to the shell.
	Class string
	// ExtraAttrs forwards additional attributes to the shell's root
	// element — the same contract every component's ExtraAttrs
	// carries (data-* test hooks, analytics markers). Keys the
	// component owns are dropped: class (use Class), id, the input's
	// type, name, placeholder, required, autocomplete, aria-invalid
	// and aria-describedby (the field's wiring owns them), plus every
	// data-cui-* and data-hui-* key — the reveal hooks are the
	// runtime's contract, not a caller's to forge. Autocomplete has
	// its own field because it belongs to the input that submits.
	ExtraAttrs map[string]string

	// Ctx carries the per-request context used to resolve the reveal
	// button's words (ui.StringsFor: ShowPassword, HidePassword,
	// RevealShow, RevealHide). When nil, the English defaults are
	// used.
	Ctx context.Context

	// Field is the wiring an enclosing FormField handed its builder
	// (the headless.FieldControl its Input closure received). Applied
	// to the inner input — the id the outer label points at, the
	// described-by chain, the invalid state and the required flag —
	// and it wins over the config's own: two sources for one fact is
	// how they drift. Zero value means standalone.
	Field headless.FieldControl
}

// PasswordInput renders a password field with a show/hide reveal
// button bound by the headless behaviour module.
func PasswordInput(cfg PasswordInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: PasswordInput requires Name")
	}
	id := cfg.ID
	if cfg.Field.ID != "" {
		id = cfg.Field.ID
	}
	if id == "" {
		panic("ui: PasswordInput requires ID (or a Field carrying one)")
	}

	// Autocomplete is an attribute of the inner input (it tells the
	// browser what to fill), so it travels the headless Extra seam the
	// props document for exactly that. A caller's ExtraAttrs go to
	// the shell's root, the contract every component's ExtraAttrs
	// carries — never the input that submits.
	inputExtra := html.Attrs{}
	if cfg.Autocomplete != "" {
		inputExtra["autocomplete"] = cfg.Autocomplete
	}
	// A nil Ctx resolves through i18nui's English defaults rather
	// than the bridge's nil short-circuit, the behaviour the
	// component always had: the bytes are the same (the bridge pins
	// them), and a host that swaps the default table is heard.
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	return passwordInputStyle.WrapHTML(headless.Password(headless.PasswordProps{
		Name:        cfg.Name,
		Placeholder: cfg.Placeholder,
		Required:    cfg.Required || cfg.Field.Required,
		Invalid:     cfg.Field.Invalid,
		ID:          id,
		DescribedBy: cfg.Field.DescribedBy,
		Extra:       inputExtra,
		Parts: headless.Parts{Attrs: headless.PartAttrs{
			headless.PartRoot: html.SafeExtraAttrs(cfg.ExtraAttrs,
				"type", "name", "placeholder", "required", "autocomplete",
				"aria-invalid", "aria-describedby"),
		}},
		Strings: StringsFor(ctx),
	}, withRootClass(passwordClasses, cfg.Class)))
}

var passwordInputStyle = registry.RegisterStyle("ui-password-input", passwordInputCSS)

func passwordInputCSS(_ style.Theme) string {
	return `.fui-password {
  display: flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
}
/* The shell is the control: typing in the input rings the whole field. */
.fui-password:has(.fui-password__input:focus-visible) {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: 2px;
}
.fui-password__input {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  padding: var(--ui-control-padding-y, 10px) 12px;
  min-block-size: var(--fui-density-control-h);
  min-inline-size: 0;
}
.fui-password__input::placeholder { color: var(--color-text-subtle); }
.fui-password__input:focus-visible {
  outline: none;
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) { .fui-password__input { font-size: var(--text-base, 1rem); } }
/* A ghost button inset in the field, not a gray slab bolted on. */
.fui-password__reveal {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  align-self: center;
  margin-inline-end: var(--spacing-sm, 4px);
  min-block-size: calc(var(--fui-density-control-h) - 2 * var(--spacing-sm, 4px));
  min-inline-size: var(--spacing-touch-target, 44px);
  padding-inline: var(--spacing-md, 8px);
  background: transparent;
  border: 0;
  border-radius: var(--radii-sm, 6px);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  user-select: none;
}
.fui-password__reveal:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
}
.fui-password__reveal:focus-visible {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: 0;
}
.fui-password__reveal:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
/* The invalid state arrives as data-invalid on the shell (the input
   inside has no border of its own to colour). */
.fui-password[data-invalid] {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}`
}
