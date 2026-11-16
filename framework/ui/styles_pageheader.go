package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

var pageHeaderStyle = registry.RegisterStyle(
	"ui-page-header",
	pageHeaderCSS,
	registry.WithLoad(registry.LoadPrewarm),
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
			"padding", "var(--ui-page-header-inset, var(--spacing-xl, 24px)) 0 var(--spacing-sm, 4px)",
		).
		End().
		Rule(".fui-page-header__text").
		Set("display", "grid", "gap", "var(--spacing-sm, 4px)").
		End().
		Rule(".fui-page-header__title-row").
		Set("display", "flex", "flex-wrap", "wrap", "align-items", "center",
			"gap", "var(--spacing-md, 8px)", "min-inline-size", "0").
		End().
		Rule(".fui-page-header__title").
		Set("min-inline-size", "0", "overflow-wrap", "anywhere").
		End().
		Rule(".fui-page-header__eyebrow").
		Set(
			"margin", "0",
			"font-size", "var(--text-sm, 0.875rem)",
			"font-weight", "{font-weight.medium}",
			"color", "var(--color-text-muted, #52525B)",
			"text-transform", "var(--ui-page-header-eyebrow-case, none)",
		).
		End().
		Rule(".fui-page-header__title").
		Set(
			"margin", "0",
			"font-size", "var(--ui-page-header-title-size, var(--text-2xl, 1.5rem))",
			"font-weight", "{font-weight.semibold}",
			"line-height", "var(--ui-page-header-title-leading, 1.25)",
			"letter-spacing", "var(--ui-page-header-title-tracking, -0.02em)",
			"color", "var(--color-text, #09090B)",
		).
		End().
		Rule(".fui-page-header__subtitle").
		Set("margin", "0", "font-size", "var(--text-sm, 0.875rem)", "color", "var(--color-text-muted, #52525B)",
			"overflow-wrap", "anywhere").
		End().
		Rule("h2.fui-page-header__title").
		Set("font-size", "var(--ui-page-header-section-title-size, var(--text-xl, 1.25rem))").
		End().
		Rule(":is(h3, h4, h5, h6).fui-page-header__title").
		Set("font-size", "var(--ui-page-header-subsection-title-size, var(--text-base, 1rem))",
			"letter-spacing", "normal").
		End().
		Rule(".fui-page-header__actions").
		Set(
			"display", "flex",
			"flex-wrap", "wrap",
			"gap", "var(--spacing-sm, 4px)",
		).
		End().
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
