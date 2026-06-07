package main

// Theme — the GoFastr brand design language (dark-first, warm near-black
// oklch surface ladder, a single amber accent, sans + mono). Built on
// theme.Default so every framework/ui component the admin renders
// (DataTable, Button, Form, Select, PageHeader, Sidebar) is retuned through
// the --color-* / --font-* CSS variables. oklch flows through unchanged.
//
// This mirrors examples/site's tokens so the admin reads as the same product.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

func createTheme() style.Theme {
	t := theme.Default(theme.Overrides{
		Background:   "oklch(0.135 0.005 75)",
		Surface:      "oklch(0.17 0.006 75)",
		SurfaceSoft:  "oklch(0.21 0.007 75)",
		Border:       "oklch(0.28 0.006 75)",
		BorderStrong: "oklch(0.38 0.008 75)",
		Text:         "oklch(0.96 0.006 80)",
		TextMuted:    "oklch(0.78 0.008 75)",
		TextSubtle:   "oklch(0.66 0.010 70)",

		// One chromatic accent — amber, not the red/blue every admin picks.
		Primary:   "oklch(0.82 0.155 78)",
		PrimaryFg: "oklch(0.14 0.005 75)",
		Accent:    "oklch(0.82 0.155 78)",

		CodeSurface: "oklch(0.21 0.007 75)",
		CodeText:    "oklch(0.96 0.006 80)",
		CodeBorder:  "oklch(0.28 0.006 75)",

		FontBody:    "-apple-system, BlinkMacSystemFont, Inter, 'Segoe UI', system-ui, sans-serif",
		FontHeading: "-apple-system, BlinkMacSystemFont, Inter, 'Segoe UI', system-ui, sans-serif",
		FontMono:    "ui-monospace, SFMono-Regular, 'JetBrains Mono', Menlo, Consolas, monospace",

		RadiusSm: 4,
		RadiusMd: 6,
		RadiusLg: 10,
	})
	t.Spacing.XS = style.Spacing{Name: "xs", Value: 4}
	t.Spacing.SM = style.Spacing{Name: "sm", Value: 8}
	t.Spacing.MD = style.Spacing{Name: "md", Value: 12}
	t.Spacing.LG = style.Spacing{Name: "lg", Value: 16}
	t.Spacing.XL = style.Spacing{Name: "xl", Value: 24}
	t.Spacing.XXL = style.Spacing{Name: "xxl", Value: 32}
	t.Spacing.XXXL = style.Spacing{Name: "xxxl", Value: 48}
	return t
}
