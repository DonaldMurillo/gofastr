package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Selection ──────────────────────────────────────────────────────
//
// A body of selectable rows with the bar that acts on them: a list's
// bulk-action form over its table. The bar shows only while a checkbox
// in the body is checked, so a list nobody is selecting from does not
// carry an action form above its rows. The rule is CSS (:has), so it
// needs no script; a browser without :has() shows the bar always.

// SelectionConfig configures a Selection.
type SelectionConfig struct {
	// Bar acts on the selection: a bulk-action form whose checkboxes
	// live in Body (the inputs name it through form=). Required.
	Bar render.HTML
	// Body holds the selectable rows and their checkboxes. Required.
	Body render.HTML

	ID    string
	Class string
	// ExtraAttrs land on the root; class and id are the component's.
	ExtraAttrs html.Attrs
}

// Selection renders the bar over the body.
func Selection(cfg SelectionConfig) render.HTML {
	if strings.TrimSpace(string(cfg.Bar)) == "" {
		panic("ui: Selection requires Bar — with nothing to act on the selection, render Body alone")
	}
	if strings.TrimSpace(string(cfg.Body)) == "" {
		panic("ui: Selection requires Body — the rows the bar acts on")
	}
	return selectionStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls("fui-selection", cfg.Class),
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "class", "id"),
	},
		html.Div(html.DivConfig{Class: "fui-selection__bar"}, cfg.Bar),
		html.Div(html.DivConfig{Class: "fui-selection__body"}, cfg.Body),
	))
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
  [data-cui-comp="ui-selection"]:not(:has(> .fui-selection__body input[type="checkbox"]:checked)) > .fui-selection__bar {
    display: none;
  }
}`
}
