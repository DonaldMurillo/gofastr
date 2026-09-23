package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Combobox ──────────────────────────────────────────────────────
//
// An input that owns a listbox of suggestions, rendered through
// headless.Combobox dressed with the fui-combobox class map: the field
// look (label, bordered input) the retired core-ui/patterns/combobox
// shipped, on the primitive's accessibility contract. The label is
// visible (LabelHidden folds it into the visually-hidden recipe); the
// result-count status region is always visually hidden — it exists for
// the reader, not the eye.
//
// Options is the static shape: the headless-combobox module filters the
// inline rows client-side, no round-trip. Island + NoScriptAction is
// the typed shape: every keystroke debounces an RPC that re-renders the
// listbox through the signal, and a reader without script gets the
// same-origin GET form.
//
// The style marker rides the wrapper this component renders around the
// primitive (the primitive's own outermost tag changes shape with the
// no-script form), so the root rules below are compound and every
// other rule scopes under the wrapper.

// ComboboxConfig configures a Combobox.
type ComboboxConfig struct {
	// ID is the input element id (the listbox takes <ID>-listbox).
	// Required, page-unique.
	ID string
	// Name is the form-submit name on the input. Required.
	Name string
	// Label is the visible label text. Required.
	Label string
	// LabelHidden folds the label into the visually-hidden recipe (the
	// placeholder or surrounding chrome already names the field).
	LabelHidden bool
	// Placeholder for the input.
	Placeholder string

	// Options is a static list the module filters client-side. Takes
	// precedence over Island.
	Options []headless.ComboboxOption

	// Island is the typed in-page results contract: the endpoint that
	// re-renders the listbox and the signal the region is bound to.
	Island *headless.Island
	// NoScriptAction is the form's action URL, the no-script
	// destination: same-origin, a GET that submits the query. Required
	// when Island is set; refused when it is "#".
	NoScriptAction string

	// DebounceMs bounds the input debounce. Default 250.
	DebounceMs int

	// Class rides the wrapper beside the component's own class.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the combobox's root.
	// Keys the component owns are dropped, as everywhere.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the
	// combobox's sentences. When nil, English fallbacks apply.
	Ctx context.Context
}

// Combobox renders the suggestion input with its listbox.
func Combobox(cfg ComboboxConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	labelClass := "fui-combobox__label"
	if cfg.LabelHidden {
		labelClass += " fui-visually-hidden"
	}
	classes := headless.Classes{
		headless.PartLabel:           labelClass,
		headless.PartComboboxForm:    "fui-combobox__form",
		headless.PartComboboxInput:   "fui-combobox__input",
		headless.PartComboboxListbox: "fui-combobox__listbox",
		headless.PartComboboxOption:  "fui-combobox__option",
		headless.PartComboboxStatus:  "fui-visually-hidden",
		headless.PartText:            "fui-combobox__option-label",
	}
	out := headless.Combobox(headless.ComboboxProps{
		ID:             cfg.ID,
		Name:           cfg.Name,
		Label:          cfg.Label,
		Placeholder:    cfg.Placeholder,
		Island:         cfg.Island,
		NoScriptAction: cfg.NoScriptAction,
		DebounceMS:     cfg.DebounceMs,
		Options:        cfg.Options,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs),
		Strings:        StringsFor(ctx),
	}, classes)
	cls := "fui-combobox"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	wrapAttrs := html.Attrs{"class": cls}
	return comboboxStyle.WrapHTML(render.Tag("div", wrapAttrs, out))
}

var comboboxStyle = registry.RegisterStyle("ui-combobox", comboboxCSS)

func comboboxCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-combobox"].fui-combobox {
  position: relative;
  display: block;
  inline-size: 100%;
  max-inline-size: 24rem;
}
[data-fui-comp="ui-combobox"] .fui-combobox__label {
  display: block;
  margin-block-end: var(--spacing-xs, 2px);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #4b5563);
}
[data-fui-comp="ui-combobox"] .fui-combobox__form {
  display: block;
  margin: 0;
}
[data-fui-comp="ui-combobox"] .fui-combobox__input {
  inline-size: 100%;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111);
  font: inherit;
  font-size: var(--text-base, 1rem);
  box-sizing: border-box;
}
[data-fui-comp="ui-combobox"] .fui-combobox__input:focus-visible {
  outline: none;
  border-color: var(--color-primary, #4F46E5);
  box-shadow: 0 0 0 3px rgba(79, 70, 229, 0.18);
}
[data-fui-comp="ui-combobox"] .fui-combobox__listbox {
  position: absolute;
  inset-inline-start: 0;
  inset-inline-end: 0;
  margin: var(--spacing-sm, 4px) 0 0 0;
  padding: var(--spacing-sm, 4px) 0;
  list-style: none;
  background: var(--color-surface, #fff);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 8px);
  box-shadow: 0 8px 24px rgba(0,0,0,0.12);
  max-block-size: 18rem;
  overflow-y: auto;
  z-index: 50;
}
[data-fui-comp="ui-combobox"] .fui-combobox__listbox[hidden] { display: none; }
[data-fui-comp="ui-combobox"] .fui-combobox__option[hidden] {
  /* Author origin beats the UA's [hidden]{display:none} regardless of
     specificity, so the option rules below (display:block, and
     display:flex under pointer:coarse) would override the attribute and
     static-option filtering would paint every row. This guard must
     out-specify BOTH option rules: it carries an extra [hidden]
     attribute over theirs. */
  display: none;
}
[data-fui-comp="ui-combobox"] .fui-combobox__option {
  display: block;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  color: var(--color-text, #111);
  cursor: pointer;
  user-select: none;
}
[data-fui-comp="ui-combobox"] .fui-combobox__option.is-active {
  background: var(--color-surface-soft, #f1f1f3);
}
[data-fui-comp="ui-combobox"] .fui-combobox__option[aria-disabled="true"] {
  color: var(--color-text-muted, #6b7280);
  cursor: default;
}
@media (pointer: coarse) {
  [data-fui-comp="ui-combobox"] .fui-combobox__option {
    min-block-size: var(--spacing-touch-target, 44px);
    display: flex;
    align-items: center;
  }
}
/* Scoped copy of the visually-hidden recipe: the status live region
   (and a hidden label) must not be seen on a page that loads only this
   sheet, and must stay in the accessibility tree (clipped, never
   display: none). */
[data-fui-comp="ui-combobox"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}`
}
