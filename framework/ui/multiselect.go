package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── MultiSelect ───────────────────────────────────────────────────
//
// Renders headless.MultiSelect dressed with the fui-multiselect class
// map: a checkbox group inside a native disclosure with a chips strip
// above it. The submit contract is the plain form — every checkbox
// shares the field name and a page with script blocked submits the
// checked options as repeated keys — and the chips (rebuilt from the
// checkboxes' own state by the registered headless-multiselect module)
// are the enhancement. The disclosure itself is ui.Collapsible's
// primitive (headless.Disclosure): Escape to close with focus
// returned, the aria-expanded mirror.

// MultiSelectOption is one checkbox option: Value is the form-submit
// value, Label the visible text, Selected the first-paint state,
// Disabled the greyed-out unsubmitting state.
type MultiSelectOption = headless.MultiSelectOption

// MultiSelectConfig configures one multiselect.
type MultiSelectConfig struct {
	// Name is the form-field name every checkbox shares. Required.
	Name string
	// Label is the group's accessible name and the disclosure's
	// summary. Required.
	Label string
	// Placeholder is what the chips strip says when nothing is
	// picked. Defaults to "Choose…" through the Strings table.
	Placeholder string
	// Options are the choices, in order. Required.
	Options []MultiSelectOption
	// Open renders the disclosure expanded.
	Open bool

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the wrapper. Keys
	// the component owns are dropped: class and id (use Class / ID).
	ExtraAttrs html.Attrs

	// Ctx resolves the Strings table through the request's translator.
	Ctx context.Context
}

// MultiSelect renders the checkbox-group disclosure.
func MultiSelect(cfg MultiSelectConfig) render.HTML {
	classes := headless.Classes{
		headless.PartRoot:             "fui-multiselect",
		headless.PartMultiSelectChips: "fui-multiselect__chips",
		headless.PartSummary:          "fui-multiselect__summary",
		headless.PartPanel:            "fui-multiselect__panel",
		headless.PartMultiSelectGroup: "fui-multiselect__group",
		headless.PartMultiSelectRow:   "fui-multiselect__row",
		headless.PartControl:          "fui-multiselect__check",
		headless.PartLabel:            "fui-multiselect__row-label",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.MultiSelect(headless.MultiSelectProps{
		Name:        cfg.Name,
		Label:       cfg.Label,
		Options:     cfg.Options,
		Open:        cfg.Open,
		Placeholder: cfg.Placeholder,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Strings:     StringsFor(cfg.Ctx),
	}, classes)
	return multiselectStyle.WrapHTML(out)
}

var multiselectStyle = registry.RegisterStyle("ui-multiselect", multiselectCSS)

func multiselectCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-multiselect"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
  max-inline-size: 32rem;
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__chips {
  display: flex;
  gap: var(--spacing-xs, 2px);
  flex-wrap: wrap;
  min-block-size: 28px;
  align-items: center;
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__chips:empty::before {
  content: attr(data-hui-multiselect-placeholder);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  font-style: italic;
}
/* The chips the module builds carry their own hooks: class-free
   modules, sheet-owned looks. */
[data-fui-comp="ui-multiselect"] [data-hui-multiselect-chip] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px) var(--spacing-sm, 4px) 10px;
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  border-radius: 999px;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 500;
}
[data-fui-comp="ui-multiselect"] [data-hui-multiselect-remove] {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--text-lg, 1.125rem);
  line-height: 1;
}
[data-fui-comp="ui-multiselect"] [data-hui-multiselect-remove]:hover {
  background: color-mix(in srgb, var(--color-primary-fg, #FFFFFF) 25%, transparent);
}
[data-fui-comp="ui-multiselect"] details {
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__summary {
  display: flex;
  align-items: center;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 8px);
  font-weight: 500;
  color: var(--color-text, #18181B);
  cursor: pointer;
  user-select: none;
  list-style: none;
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__summary::-webkit-details-marker {
  display: none;
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__summary::before {
  content: "▾";
  margin-inline-end: var(--spacing-sm, 4px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  transition: transform 120ms ease;
}
[data-fui-comp="ui-multiselect"] details[open] .fui-multiselect__summary::before {
  transform: rotate(180deg);
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__group {
  display: grid;
  gap: 0;
  border: 0;
  padding: 0;
  border-top: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  min-block-size: var(--spacing-touch-target, 44px);
  cursor: pointer;
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__row:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-multiselect"] .fui-multiselect__check:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
