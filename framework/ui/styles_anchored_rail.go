package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Registered stylesheet for ui.AnchoredRail, which renders
// headless.Rail dressed with the fui-anchored-rail class map.
// Token-resolved via the typed StyleSheet DSL so apps re-skin via
// their theme (`--color-primary`, `--color-text`, `--spacing-md`,
// etc.) without overriding selectors.
//
// Active state targets both .is-active and aria-current="true" because
// the headless-rail module sets BOTH; supporting either lets apps
// style with the attribute selector (no JS state class required) or
// the class selector (more familiar to teams with custom CSS
// pipelines).

func anchoredRailCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-anchored-rail", t).
		Rule("&").
		Set(
			"position", "sticky",
			"top", "calc(var(--nav-h, 60px) + var(--spacing-lg, 16px))",
			"align-self", "start",
			"font-size", "var(--text-sm, 0.875rem)",
		).End().
		Rule(".fui-anchored-rail__label").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "var(--text-xs, 0.75rem)",
			"color", "{colors.text-subtle}",
			"font-weight", "400",
			"margin", "0 0 {spacing.md} 0",
		).End().
		Rule(".fui-anchored-rail__list").
		Set(
			"list-style", "none",
			"margin", "0",
			"padding", "0",
			"display", "grid",
			"gap", "var(--spacing-xs, 2px)",
		).End().
		Rule(".fui-anchored-rail__list li").
		Set("padding", "0").End().
		Rule(".fui-anchored-rail__list a").
		Set(
			"display", "grid",
			"grid-template-columns", "28px 1fr 28px",
			"gap", "var(--spacing-md, 8px)",
			"padding", "6px 0",
			"color", "{colors.text-muted}",
			"text-decoration", "none",
			"line-height", "1.4",
		).End().
		Rule(".fui-anchored-rail__list a:hover").
		Set("color", "{colors.text}").End().
		Rule(".fui-anchored-rail__eyebrow").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "var(--text-xs, 0.75rem)",
			"color", "{colors.text-subtle}",
		).End().
		Rule(".fui-anchored-rail__count").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "0.625rem",
			"color", "{colors.text-subtle}",
			"text-align", "right",
		).End().
		// Active state: the headless-rail module sets BOTH .is-active
		// and aria-current.
		Rule(`.fui-anchored-rail__list a.is-active, .fui-anchored-rail__list a[aria-current="true"]`).
		Set("color", "{colors.text}").End().
		Rule(`.fui-anchored-rail__list a.is-active .fui-anchored-rail__eyebrow, .fui-anchored-rail__list a[aria-current="true"] .fui-anchored-rail__eyebrow`).
		Set("color", "{colors.primary}").End().
		MustBuild()
}
