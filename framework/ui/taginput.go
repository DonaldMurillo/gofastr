package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── TagInput ───────────────────────────────────────────────────────
//
// Free-form text → chips, over headless.TagInput. The committed values
// are visible chips with their own named remove controls on the first
// paint AND hidden inputs under one Name (the standard repeated-key
// pattern), so what a reader sees removed is what a submit stops
// carrying — with or without script. The module commits the draft on
// Enter or comma, removes on Backspace or the chip's ×, returns focus
// to the field, and announces through the sentences the component
// carries from its Strings.
//
// Different from MultiSelect: MultiSelect has a fixed option list;
// TagInput is open-ended free text.

// TagInputConfig configures a TagInput.
type TagInputConfig struct {
	// Name is the form-field name (required). Each tag is submitted
	// under this name (repeated key).
	Name string
	// Label is the accessible label (required).
	Label string
	// Values are the initial tags.
	Values []string
	// Placeholder for the text input.
	Placeholder string
	// MaxLength caps individual tag length (chars). 0 = no cap.
	MaxLength int
	// Help renders supporting text under the field.
	Help string
	// Disabled disables all interaction.
	Disabled bool
	ID       string
	Class    string

	// ExtraAttrs forwards additional attributes to the field's root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs
}

// tagInputClasses dresses headless.TagInput's parts in this package's
// own vocabulary — the names the registered ui-tag-input sheet
// matches.
var tagInputClasses = headless.Classes{
	headless.PartRoot:          "fui-tag-input",
	headless.PartLabel:         "fui-tag-input__label",
	headless.PartTagInputZone:  "fui-tag-input__zone",
	headless.PartTagInputList:  "fui-tag-input__list",
	headless.PartTagInputTag:   "fui-tag-input__chip",
	headless.PartDismiss:       "fui-tag-input__chip-remove",
	headless.PartTagInputField: "fui-tag-input__field",
	headless.PartTagInputAdd:   "fui-tag-input__add",
	headless.PartHint:          "fui-tag-input__help",
	headless.PartStatus:        "fui-visually-hidden",
}

// TagInput renders a free-form tag input bound to a chip strip.
func TagInput(cfg TagInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: TagInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: TagInput requires Label")
	}
	parts := headless.Parts{}
	if rootClass := strings.TrimSpace(modifierClass("is-disabled", cfg.Disabled) +
		" " + cfg.Class); rootClass != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(rootClass)}}
	}
	return tagInputStyle.WrapHTML(headless.TagInput(headless.TagInputProps{
		Name:        cfg.Name,
		Label:       cfg.Label,
		Values:      cfg.Values,
		Placeholder: cfg.Placeholder,
		MaxLength:   cfg.MaxLength,
		Help:        cfg.Help,
		Disabled:    cfg.Disabled,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:       parts,
		Strings:     StringsFor(nil),
	}, tagInputClasses))
}

var tagInputStyle = registry.RegisterStyle("ui-tag-input", tagInputCSS)

func tagInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-tag-input"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__zone {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 2px);
  align-items: center;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__zone:focus-within {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__list {
  display: contents;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__chip {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px) var(--spacing-xs, 2px) 10px;
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  border-radius: 999px;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 500;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__chip-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--text-base, 1rem);
  line-height: 1;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__chip-remove:hover {
  background: color-mix(in srgb, var(--color-primary-fg, #FFFFFF) 25%, transparent);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__field {
  flex: 1 1 8rem;
  border: 0;
  outline: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
  min-block-size: 28px;
  padding: 0;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: 28px;
  min-block-size: 28px;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, #F4F4F5);
  font: inherit;
  font-weight: 600;
  color: var(--color-text, #18181B);
  cursor: pointer;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__add:hover {
  background: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__add:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
[data-fui-comp="ui-tag-input"] .fui-tag-input__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-tag-input"].is-disabled .fui-tag-input__zone {
  opacity: 0.6;
  cursor: not-allowed;
}
/* Scoped copy of the visually-hidden recipe: the status live region
   must not be seen on a page that loads only this sheet. */
[data-fui-comp="ui-tag-input"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}`
}
