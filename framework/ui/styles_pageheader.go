package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// pageHeaderStyle registers PageHeader's CSS with the framework
// component registry. The handle wraps every PageHeader render so
// the runtime auto-loads /__gofastr/comp/ui-page-header.css on
// first appearance, dedup'd globally. LoadAlways because page
// headers appear on essentially every screen. Paying the eager
// link saves a flash of unstyled chrome on first paint.
var pageHeaderStyle = registry.RegisterStyle(
	"ui-page-header",
	pageHeaderCSS,
	registry.WithLoad(registry.LoadAlways),
)

func pageHeaderCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-page-header", t).
		Rule("&").
		Set(
			"display", "flex",
			"flex-wrap", "wrap",
			"align-items", "flex-start",
			"justify-content", "space-between",
			"gap", "var(--spacing-lg, 16px)",
			// No rule under the header: the title's weight and the gap to
			// the content separate them, and a hairline that every page
			// header drew competed with the card and table borders below.
			"padding", "var(--spacing-xl, 24px) 0 var(--spacing-sm, 4px)",
		).
		End().
		Rule(".fui-page-header__text").
		Set("display", "grid", "gap", "var(--spacing-sm, 4px)").
		End().
		Rule(".fui-page-header__title-row").
		Set("display", "flex", "flex-wrap", "wrap", "align-items", "center",
			"gap", "var(--spacing-md, 8px)", "min-inline-size", "0").
		End().
		Rule(".fui-page-header__title-row > .fui-page-header__title").
		Set("min-inline-size", "0", "overflow-wrap", "anywhere").
		End().
		Rule(".fui-page-header__eyebrow").
		Set(
			"margin", "0",
			"font-size", "var(--text-sm, 0.875rem)",
			"font-weight", "{font-weight.medium}",
			"color", "var(--color-text-muted, #52525B)",
			// Knob: --ui-page-header-eyebrow-case (none) sets the
			// eyebrow's letter case.
			"text-transform", "var(--ui-page-header-eyebrow-case, none)",
		).
		End().
		// Knobs: --ui-page-header-title-size/-leading/-tracking let a host
		// scale titles to editorial display type app-wide without
		// restyling the component's internals.
		Rule(".fui-page-header__title").
		Set(
			"margin", "0",
			"font-size", "var(--ui-page-header-title-size, var(--text-2xl, 1.5rem))",
			"font-weight", "{font-weight.semibold}",
			"line-height", "var(--ui-page-header-title-leading, 1.25)",
			"letter-spacing", "var(--ui-page-header-title-tracking, -0.02em)",
			"color", "var(--color-text, #18181B)",
		).
		End().
		Rule(".fui-page-header__subtitle").
		Set("margin", "0", "font-size", "var(--text-sm, 0.875rem)", "color", "var(--color-text-muted, #52525B)").
		End().
		// An h2 title is a section under the page's own h1 (a dashboard's
		// recent rows, a detail page's related list): one step down.
		Rule("h2.fui-page-header__title").
		Set("font-size", "var(--ui-page-header-title-size, var(--text-xl, 1.25rem))").
		End().
		Rule(".fui-page-header__actions").
		Set(
			"display", "flex",
			"flex-wrap", "wrap",
			"gap", "var(--spacing-sm, 4px)",
		).
		End().
		// A section header sits in its parent's rhythm (a stack gap), so
		// it drops the page header's top inset.
		Rule("&:has(h2.fui-page-header__title)").
		Set("padding-block-start", "0").
		End().
		Rule("&.fui-page-header--compact").
		Set("padding", "0", "border-bottom", "0").
		End().
		Rule("&.fui-page-header--compact h2.fui-page-header__title").
		Set("font-size", "var(--text-lg)", "line-height", "calc(var(--leading-snug, 1.4) - 0.1)").
		End().
		MustBuild()
}
