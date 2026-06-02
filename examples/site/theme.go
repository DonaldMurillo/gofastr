package main

// =============================================================================
// Theme — v2 design tokens expressed as a typed style.Theme so the rest of
// the framework's pipeline (StyleSheet token resolution, framework/ui
// component CSS, the color-scheme bootstrap, the catalog endpoint) all see
// the same source of truth.
//
// The v2 design uses oklch() throughout for perceptual uniformity. style.Color
// accepts any CSS color string in .Value, so oklch flows through unchanged
// and the framework's --color-* CSS variables carry the oklch values to
// every component below.
//
// Tokens with no canonical slot in style.ColorSet (line-faint, accent-2,
// accent-dim, the syntax-highlight palette, the higher spacing steps) are
// added as extra :root rules in createStyleSheet — they're still defined
// once, still referenced everywhere via var(--…), still typed at point of
// authoring.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// createTheme returns the v2 site theme. Built on theme.Default so framework
// components get sensible base values for everything we don't override.
func createTheme() style.Theme {
	t := theme.Default(theme.Overrides{
		// Warm near-black surface ladder, off-white text. All from v2.css.
		Background:   "oklch(0.135 0.005 75)", // --bg
		Surface:      "oklch(0.17 0.006 75)",  // --bg-2 (surface)
		SurfaceSoft:  "oklch(0.21 0.007 75)",  // --bg-3 (elevated / code chip)
		Border:       "oklch(0.28 0.006 75)",  // --line
		BorderStrong: "oklch(0.38 0.008 75)",  // --line-strong
		Text:         "oklch(0.96 0.006 80)",  // --fg
		TextMuted:    "oklch(0.78 0.008 75)",  // --fg-2
		TextSubtle:   "oklch(0.66 0.010 70)",  // --fg-3 (bumped 0.58->0.66 for WCAG AA on surface)

		// Amber accent. Used for the brand square, the hero highlight, links,
		// the primary CTA, the live-dot. One chromatic accent on the page.
		// Picked so the page reads as a dev tool that isn't another red/blue site.
		Primary:   "oklch(0.82 0.155 78)", // --accent
		PrimaryFg: "oklch(0.14 0.005 75)", // --on-accent  (text on amber)
		Accent:    "oklch(0.82 0.155 78)", // same — single accent

		// Code-block surface — uses the bg-3 chip so code reads as inset.
		CodeSurface: "oklch(0.21 0.007 75)",
		CodeText:    "oklch(0.96 0.006 80)",
		CodeBorder:  "oklch(0.28 0.006 75)",

		// System stack instead of Google Fonts so the page works under the
		// framework's `default-src 'self'` CSP. Geist/JetBrains Mono in the
		// prototype become the closest system equivalents. Design intent of
		// "sans + mono only" survives the substitution.
		FontBody:    "-apple-system, BlinkMacSystemFont, Inter, 'Segoe UI', system-ui, sans-serif",
		FontHeading: "-apple-system, BlinkMacSystemFont, Inter, 'Segoe UI', system-ui, sans-serif",
		FontMono:    "ui-monospace, SFMono-Regular, 'JetBrains Mono', Menlo, Consolas, monospace",

		// Radii — v2 uses 4/6/10 in tightening order.
		RadiusSm: 4,
		RadiusMd: 6,
		RadiusLg: 10,
	})

	// Spacing — v2 ladder is 4/8/12/16/24/32/48 mapped to XS..XXXL.
	// Larger steps (64/96/128) get added as raw :root vars in the stylesheet
	// since there's no canonical slot above XXXL.
	t.Spacing.XS = style.Spacing{Name: "xs", Value: 4}
	t.Spacing.SM = style.Spacing{Name: "sm", Value: 8}
	t.Spacing.MD = style.Spacing{Name: "md", Value: 12}
	t.Spacing.LG = style.Spacing{Name: "lg", Value: 16}
	t.Spacing.XL = style.Spacing{Name: "xl", Value: 24}
	t.Spacing.XXL = style.Spacing{Name: "xxl", Value: 32}
	t.Spacing.XXXL = style.Spacing{Name: "xxxl", Value: 48}

	return t
}
