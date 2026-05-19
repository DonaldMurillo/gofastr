package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// pageHeaderStyle registers PageHeader's CSS with the framework
// component registry. The handle wraps every PageHeader render so
// the runtime auto-loads /__gofastr/comp/ui-page-header.css on
// first appearance, dedup'd globally. LoadAlways because page
// headers appear on essentially every screen — paying the eager
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
			"padding", "var(--spacing-xl, 24px) 0 var(--spacing-lg, 16px)",
			"border-bottom", "1px solid var(--color-border, #E4E4E7)",
		).
		End().
		Rule(".ui-page-header__text").
		Set("display", "grid", "gap", "var(--spacing-xs, 2px)").
		End().
		Rule(".ui-page-header__eyebrow").
		Set(
			"margin", "0",
			"font-size", "0.75rem",
			"font-weight", "600",
			"text-transform", "uppercase",
			"letter-spacing", "0.06em",
			"color", "var(--color-text-subtle, #71717A)",
		).
		End().
		Rule(".ui-page-header__title").
		Set(
			"margin", "0",
			"font-size", "1.5rem",
			"font-weight", "700",
			"line-height", "1.25",
			"color", "var(--color-text, #18181B)",
		).
		End().
		Rule(".ui-page-header__subtitle").
		Set("margin", "0", "color", "var(--color-text-muted, #52525B)").
		End().
		Rule(".ui-page-header__actions").
		Set(
			"display", "flex",
			"flex-wrap", "wrap",
			"gap", "var(--spacing-sm, 4px)",
		).
		End().
		MustBuild()
}
