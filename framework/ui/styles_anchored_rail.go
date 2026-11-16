package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func anchoredRailCSS(t style.Theme) string {
	return style.NewComponentSheet("ui-anchored-rail", t).
		Rule("&").
		Set(
			"position", "sticky",
			"top", "calc(var(--ui-sticky-top, 0px) + var(--spacing-lg, 16px))",
			"align-self", "start",
			"font-size", "var(--text-sm, 0.875rem)",
		).End().
		Rule(".fui-anchored-rail__label").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "var(--text-xs, 0.75rem)",
			"color", "{colors.text-subtle}",
			"font-weight", "{font-weight.normal}",
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
			"padding", "calc(var(--spacing-sm, 4px) * 1.5) 0",
			"color", "{colors.text-muted}",
			"text-decoration", "none",
			"line-height", "var(--leading-snug, 1.4)",
		).End().
		Rule(".fui-anchored-rail__list a:hover").
		Set("color", "{colors.text}").End().
		Rule(".fui-anchored-rail__eyebrow").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "var(--text-xs, 0.75rem)",
			"color", "{colors.text-subtle}",
			"text-transform", "var(--ui-anchored-rail-eyebrow-case, none)",
		).End().
		Rule(".fui-anchored-rail__count").
		Set(
			"font-family", "{fonts.mono}",
			"font-size", "calc(var(--text-xs, 0.75rem) * 0.833)",
			"color", "{colors.text-subtle}",
			"text-align", "end",
		).End().
		Rule(`.fui-anchored-rail__list a.is-active, .fui-anchored-rail__list a[aria-current="true"]`).
		Set("color", "{colors.text}").End().
		Rule(`.fui-anchored-rail__list a.is-active .fui-anchored-rail__eyebrow, .fui-anchored-rail__list a[aria-current="true"] .fui-anchored-rail__eyebrow`).
		Set("color", "{colors.primary}").End().
		MustBuild() + anchoredRailSheetCSS
}

const anchoredRailSheetCSS = `
:where(.cui-phone-sheet__sheet:popover-open) [data-cui-comp="ui-anchored-rail"] { position: static; }
.cui-phone-sheet__sheet:popover-open [data-cui-comp="ui-anchored-rail"] .fui-anchored-rail__label { display: none; }
.cui-phone-sheet__sheet:popover-open [data-cui-comp="ui-anchored-rail"] .fui-anchored-rail__list a { padding-block: var(--spacing-md, 8px); }
`
