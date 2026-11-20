package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ComboboxConfig configures a Combobox.
type ComboboxConfig struct {
	// ID, Name and Label are required; the listbox takes <ID>-listbox.
	ID    string
	Name  string
	Label string
	// LabelHidden keeps the label for readers only.
	LabelHidden bool
	Placeholder string

	// Options is a static list the module filters client-side. It
	// takes precedence over Island.
	Options []headless.ComboboxOption

	// Island is the endpoint that re-renders the listbox and its signal.
	Island *headless.Island
	// NoScriptAction is the same-origin GET a reader without script
	// submits the query to. Required with Island.
	NoScriptAction string

	// DebounceMs is the input debounce. Default 250.
	DebounceMs int

	// Class rides the wrapper beside the component's own class.
	Class string

	// ExtraAttrs forwards attributes to the combobox's root; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs

	// Ctx resolves the combobox's sentences. Nil means English.
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
		headless.PartComboboxMeta:    "fui-combobox__option-meta",
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
	out = headless.Own(out)

	cls := "fui-combobox"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	wrapAttrs := html.Attrs{"class": cls}
	return comboboxStyle.WrapHTML(render.Tag("div", wrapAttrs, out))
}

var comboboxStyle = registry.RegisterStyle("ui-combobox", comboboxCSS)

func comboboxCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-combobox"]).fui-combobox {
  position: relative;
  display: block;
  inline-size: 100%;
  max-inline-size: var(--ui-combobox-max-width, 24rem);
}
/* The fill variant takes its field's width: a Picker in a form. */
:where([data-cui-comp="ui-combobox"]).fui-combobox--fill {
  max-inline-size: none;
}
[data-cui-comp="ui-combobox"] .fui-combobox__label {
  display: block;
  margin-block-end: var(--spacing-sm, 4px);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-combobox"] .fui-combobox__form {
  display: block;
  margin: 0;
}
[data-cui-comp="ui-combobox"] .fui-combobox__input {
  inline-size: 100%;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 calc(var(--spacing-sm, 4px) * 3);
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #09090B);
  box-shadow: var(--shadow-xs);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  box-sizing: border-box;
}
[data-cui-comp="ui-combobox"] .fui-combobox__input::placeholder { color: var(--color-text-subtle); }
[data-cui-comp="ui-combobox"] .fui-combobox__input:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) {
  [data-cui-comp="ui-combobox"] .fui-combobox__input { font-size: var(--text-base, 1rem); }
}
[data-cui-comp="ui-combobox"] .fui-combobox__listbox {
  position: absolute;
  inset-inline-start: 0;
  inset-inline-end: 0;
  margin: var(--spacing-sm, 4px) 0 0 0;
  padding: var(--spacing-sm, 4px);
  list-style: none;
  background: var(--color-surface, #fff);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-md);
  max-block-size: var(--ui-combobox-listbox-max-height, 18rem);
  overflow-y: auto;
  z-index: var(--z-dropdown, 100);
}
[data-cui-comp="ui-combobox"] .fui-combobox__listbox[hidden] { display: none; }
[data-cui-comp="ui-combobox"] .fui-combobox__option[hidden] {
  /* Author origin beats the UA's [hidden]{display:none} regardless of
     specificity, so the option rules below (display:block, and
     display:flex under pointer:coarse) would override the attribute and
     static-option filtering would paint every row. This guard must
     out-specify BOTH option rules: it carries an extra [hidden]
     attribute over theirs. */
  display: none;
}
:where([data-cui-comp="ui-combobox"]) .fui-combobox__option {
  display: block;
  padding: calc(var(--spacing-sm, 4px) * 1.5) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 6px);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
  cursor: pointer;
  user-select: none;
}
/* An option's secondary line: its own part, so it no longer takes the
   option's padding and fill nested inside the option. */
[data-cui-comp="ui-combobox"] .fui-combobox__option-meta {
  display: block;
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-combobox"] .fui-combobox__option.is-active {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-combobox"] .fui-combobox__option[aria-disabled="true"] {
  color: var(--color-text-muted, #52525B);
  cursor: default;
}
@media (pointer: coarse) {
  :where([data-cui-comp="ui-combobox"]) .fui-combobox__option {
    min-block-size: var(--spacing-touch-target, 44px);
    display: flex;
    align-items: center;
  }
}
/* Scoped copy of the visually-hidden recipe: the status live region
   (and a hidden label) must not be seen on a page that loads only this
   sheet, and must stay in the accessibility tree (clipped, never
   display: none). */
[data-cui-comp="ui-combobox"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}`
}
