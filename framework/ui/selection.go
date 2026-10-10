package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Selection ──────────────────────────────────────────────────────
//
// A body of selectable rows with the bar that acts on them: a list's
// bulk-action form over its table. The bar shows only while a checkbox
// in the body is checked, so a list nobody is selecting from does not
// carry an action form above its rows. The rule is CSS (:has), so it
// needs no script; a browser without :has() shows the bar always. A
// table's select-all box is not a row: it neither keeps the bar up nor
// counts.
//
// Floating draws the bar after the rows as a pill held to the bottom of
// the screen, with the number of rows checked (kept by the headless
// behaviour: a table is a size container, whose style containment walls
// a CSS counter in) and, with Form, a button that clears them by
// resetting that form.

// SelectionConfig configures a Selection.
type SelectionConfig struct {
	// Bar acts on the selection: a bulk-action form whose checkboxes
	// live in Body (the inputs name it through form=). Required.
	Bar render.HTML
	// Body holds the selectable rows and their checkboxes. Required.
	Body render.HTML
	// Floating holds the bar to the bottom of the screen, after the
	// rows, with the count of rows checked.
	Floating bool
	// Form is the id of the form the row checkboxes join. With
	// Floating, the bar ends with a button that resets it, clearing
	// the selection.
	Form string
	// Ctx carries the request's language for the count and the clear
	// button's name.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs land on the root; class and id are the component's.
	ExtraAttrs html.Attrs
}

// Selection renders the rows and their bar through headless.Selection.
func Selection(cfg SelectionConfig) render.HTML {
	if strings.TrimSpace(string(cfg.Bar)) == "" {
		panic("ui: Selection requires Bar — with nothing to act on the selection, render Body alone")
	}
	if strings.TrimSpace(string(cfg.Body)) == "" {
		panic("ui: Selection requires Body — the rows the bar acts on")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return selectionStyle.WrapHTML(headless.Selection(headless.SelectionProps{
		Bar:        cfg.Bar,
		Body:       cfg.Body,
		Floating:   cfg.Floating,
		Form:       cfg.Form,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(strings.TrimSpace(modifierClass("fui-selection--floating", cfg.Floating) + " " + cfg.Class)),
		Strings:    StringsFor(ctx),
	}, selectionClasses))
}

// selectionClasses dresses headless.Selection's parts.
var selectionClasses = headless.Classes{
	headless.PartRoot:    "fui-selection",
	headless.PartBody:    "fui-selection__body",
	headless.PartActions: "fui-selection__bar",
	headless.PartStatus:  "fui-selection__count",
	headless.PartControl: "fui-selection__clear",
}

var selectionStyle = registry.RegisterStyle("ui-selection", selectionCSS)

// selectionCSS stacks the bar over the body and hides the bar while no
// checkbox in the body is checked. Knob: --ui-selection-gap (the lg
// spacing) between the bar and the body.
func selectionCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-selection"]).fui-selection {
  display: flex;
  flex-direction: column;
  gap: var(--ui-selection-gap, var(--spacing-lg, 16px));
  min-inline-size: 0;
}
@supports selector(:has(*)) {
  [data-cui-comp="ui-selection"]:not(:has(> .fui-selection__body input[type="checkbox"]:not([data-hui-table-select-all]):checked)) > .fui-selection__bar {
    display: none;
  }
}
/* Floating: an inverse pill held to the bottom of the screen while the
   rows scroll under it. */
[data-cui-comp="ui-selection"].fui-selection--floating > .fui-selection__bar {
  position: sticky;
  inset-block-end: var(--spacing-lg, 16px);
  z-index: var(--z-sticky, 200);
  align-self: center;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  max-inline-size: 100%;
  box-sizing: border-box;
  padding: var(--spacing-xs, 2px) var(--spacing-xs, 2px) var(--spacing-xs, 2px) var(--spacing-lg, 16px);
  background: var(--color-text);
  color: var(--color-background);
  border-radius: var(--radii-xl, 14px);
  box-shadow: var(--shadow-lg);
}
/* A narrow screen gives the pill the full width: the count and the
   clear button on one line, the bar's own controls on the next. */
@media (max-width: 40rem) {
  [data-cui-comp="ui-selection"].fui-selection--floating > .fui-selection__bar {
    align-self: stretch;
    justify-content: space-between;
    padding: var(--spacing-xs, 2px) var(--spacing-xs, 2px) var(--spacing-md, 8px) var(--spacing-lg, 16px);
  }
  [data-cui-comp="ui-selection"].fui-selection--floating > .fui-selection__bar > .fui-selection__count { order: 1; }
  [data-cui-comp="ui-selection"].fui-selection--floating > .fui-selection__bar > .fui-selection__clear { order: 2; }
  [data-cui-comp="ui-selection"].fui-selection--floating > .fui-selection__bar > :not(.fui-selection__count):not(.fui-selection__clear) {
    order: 3;
    flex-basis: 100%;
  }
}
[data-cui-comp="ui-selection"] .fui-selection__count {
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold, 600);
  white-space: nowrap;
}
[data-cui-comp="ui-selection"] .fui-selection__clear {
  font-size: var(--text-lg, 1.125rem);
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: var(--ui-touch-target, 44px);
  block-size: var(--ui-touch-target, 44px);
  padding: 0;
  border: 0;
  border-radius: var(--radii-lg, 10px);
  background: transparent;
  color: inherit;
  cursor: pointer;
}
[data-cui-comp="ui-selection"] .fui-selection__clear:hover {
  background: color-mix(in oklab, currentColor 14%, transparent);
}
[data-cui-comp="ui-selection"] .fui-selection__clear:focus-visible {
  outline: var(--stroke-focus, 2px) solid currentColor;
  outline-offset: var(--stroke-focus-offset, 2px);
}`
}
