package ui

// Registered stylesheet for ui.AnchoredRail. Token-resolved via the typed
// StyleSheet DSL so apps re-skin via their theme (`--color-primary`,
// `--color-text`, `--spacing-md`, etc.) without overriding selectors.
//
// Active state targets both .is-active and aria-current="true" because
// scrollspy.js sets BOTH; supporting either lets apps style with the
// attribute selector (no JS state class required) or the class selector
// (more familiar to teams with custom CSS pipelines).

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

var anchoredRailStyle = registry.RegisterStyle("ui-anchored-rail", anchoredRailCSS)

func anchoredRailCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-anchored-rail", t).
		Rule("&").
		Set(
			"position", "sticky",
			"top", "calc(var(--nav-h, 60px) + var(--spacing-lg, 16px))",
			"align-self", "start",
			"font-size", "0.875rem",
		).End().
		Rule(".ui-anchored-rail__label").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "0.6875rem",
			"color", "{colors.text-subtle}",
			"font-weight", "400",
			"margin", "0 0 {spacing.md} 0",
		).End().
		Rule(".ui-anchored-rail__list").
		Set(
			"list-style", "none",
			"margin", "0",
			"padding", "0",
			"display", "grid",
			"gap", "2px",
		).End().
		Rule(".ui-anchored-rail__list li").
		Set("padding", "0").End().
		Rule(".ui-anchored-rail__list a").
		Set(
			"display", "grid",
			"grid-template-columns", "28px 1fr 28px",
			"gap", "8px",
			"padding", "6px 0",
			"color", "{colors.text-muted}",
			"text-decoration", "none",
			"line-height", "1.4",
		).End().
		Rule(".ui-anchored-rail__list a:hover").
		Set("color", "{colors.text}").End().
		Rule(".ui-anchored-rail__eyebrow").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "0.6875rem",
			"color", "{colors.text-subtle}",
		).End().
		Rule(".ui-anchored-rail__count").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "0.625rem",
			"color", "{colors.text-subtle}",
			"text-align", "right",
		).End().
		// Active state — scrollspy sets BOTH .is-active and aria-current.
		Rule(`.ui-anchored-rail__list a.is-active, .ui-anchored-rail__list a[aria-current="true"]`).
		Set("color", "{colors.text}").End().
		Rule(`.ui-anchored-rail__list a.is-active .ui-anchored-rail__eyebrow, .ui-anchored-rail__list a[aria-current="true"] .ui-anchored-rail__eyebrow`).
		Set("color", "{colors.primary}").End().
		MustBuild()
}
