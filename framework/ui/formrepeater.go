package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── DynamicFormRepeater ────────────────────────────────────────────
//
// Add/remove repeating field groups, over headless.Repeater.
// Server-driven: the "Add" and "Remove" buttons are named submit
// controls (name="<Name>_add" / name="<Name>_remove") riding the
// surrounding form, so the no-script page works and the server
// re-renders with the updated Items list.

// FormRepeaterConfig configures a dynamic repeating field group.
type FormRepeaterConfig struct {
	// Name is the repeater group name (used as prefix for field
	// indexing). Required.
	Name string

	// Items is the current list of rendered item groups.
	// Each item is a slice of render.HTML representing one row's fields.
	Items [][]render.HTML

	// MinItems prevents removal below this count. Default 0.
	MinItems int

	// MaxItems prevents addition above this count. Default 0 = unlimited.
	MaxItems int

	// AddLabel is the "Add" button text. Default "Add item".
	AddLabel string

	// RemoveLabel is the "Remove" button text. Default "Remove".
	RemoveLabel string

	Class string

	// ExtraAttrs forwards additional attributes to the repeater's
	// root div. Keys the component owns are dropped: class and id
	// (use Class), data-fui-*, aria-label, and aria-live.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve i18n labels
	// (Add / Remove). When nil, English fallbacks apply.
	Ctx context.Context
}

// formRepeaterClasses dresses headless.Repeater's parts in this
// package's own vocabulary — the names the registered
// ui-form-repeater sheet matches.
var formRepeaterClasses = headless.Classes{
	headless.PartRoot:           "fui-form-repeater",
	headless.PartLabel:          "fui-visually-hidden",
	headless.PartRepeaterItems:  "fui-form-repeater__items",
	headless.PartRepeaterItem:   "fui-form-repeater__item",
	headless.PartRepeaterFields: "fui-form-repeater__item-fields",
	headless.PartActions:        "fui-form-repeater__item-actions",
	headless.PartDismiss:        "fui-form-repeater__remove",
	headless.PartRepeaterAdd:    "fui-form-repeater__add",
	headless.PartStatus:         "fui-visually-hidden",
}

// FormRepeater renders a dynamic list of repeating field groups with
// add/remove controls.
//
// Server-driven: clicking "Add" submits name="<Name>_add" value="1",
// and clicking "Remove" submits name="<Name>_remove" value="<index>".
// The server processes these and re-renders with the updated Items.
func FormRepeater(cfg FormRepeaterConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FormRepeater requires Name")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// D-2: Reject impossible constraint: MinItems > MaxItems.
	if cfg.MaxItems > 0 && cfg.MinItems > cfg.MaxItems {
		panic("ui: FormRepeater MinItems (" + strconv.Itoa(cfg.MinItems) +
			") must not exceed MaxItems (" + strconv.Itoa(cfg.MaxItems) + ")")
	}

	items := make([]headless.RepeaterItem, len(cfg.Items))
	for i, fields := range cfg.Items {
		items[i] = headless.RepeaterItem{Fields: fields}
	}

	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}

	return formRepeaterStyle.WrapHTML(headless.Repeater(headless.RepeaterProps{
		Name:        cfg.Name,
		Items:       items,
		MinItems:    cfg.MinItems,
		MaxItems:    cfg.MaxItems,
		AddLabel:    cfg.AddLabel,
		RemoveLabel: cfg.RemoveLabel,
		// The submit names the surrounding form carries: the server
		// reads these to know which action was clicked.
		AddName:    cfg.Name + "_add",
		AddValue:   "1",
		RemoveName: cfg.Name + "_remove",
		ID:         cfg.Name,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-live"),
		Parts:      parts,
		Strings:    StringsFor(ctx),
	}, formRepeaterClasses))
}

func formRepeaterCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-form-repeater"] {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__items {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__item {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__item-fields {
  display: grid;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__item-actions {
  display: flex;
  justify-content: flex-end;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: 36px;
  padding: 0 var(--spacing-md, 8px);
  border: 1px solid var(--color-danger, #DC2626);
  border-radius: var(--radii-md, 8px);
  background: transparent;
  color: var(--color-danger, #DC2626);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 500;
  cursor: pointer;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__remove:hover:not(:disabled) {
  background: color-mix(in srgb, var(--color-danger, #DC2626) 10%, transparent);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__remove:focus-visible {
  outline: 2px solid var(--color-danger, #DC2626);
  outline-offset: 1px;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__remove:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  justify-self: start;
  min-block-size: 36px;
  padding: 0 var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 500;
  cursor: pointer;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__add:hover:not(:disabled) {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__add:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
[data-fui-comp="ui-form-repeater"] .fui-form-repeater__add:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
/* Scoped copy of the visually-hidden recipe: the group label and the
   status live region must not be seen on a page that loads only this
   sheet. */
[data-fui-comp="ui-form-repeater"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
`
}
