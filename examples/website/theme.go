package main

import (
	"github.com/gofastr/gofastr/core-ui/patterns/accordion"
	"github.com/gofastr/gofastr/core-ui/patterns/breadcrumbs"
	"github.com/gofastr/gofastr/core-ui/patterns/pagination"
	"github.com/gofastr/gofastr/core-ui/patterns/progress"
	"github.com/gofastr/gofastr/core-ui/patterns/skeleton"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core-ui/patterns/tabs"
	"github.com/gofastr/gofastr/framework/ui"
	"github.com/gofastr/gofastr/framework/ui/theme"
)

// createTheme builds the website's visual language. The website
// dogfoods framework/ui/theme — every page renders against the same
// canonical token set every consumer would use.
func createTheme() style.Theme {
	t := theme.Default(theme.Overrides{
		Primary: "#2563EB", // blue-600 — GoFastr brand
		Accent:  "#10B981", // emerald-500
	})
	// Used by the hero gradient. Keeps the canonical Secondary slot.
	t.Colors.Secondary = style.Color{Name: "secondary", Value: "#0F172A"} // slate-900
	return t
}

// createStyleSheet emits all the site's CSS as a string. Generated via
// the Go DSL so the whole codebase stays in one language.
func createStyleSheet(theme style.Theme) string {
	ss := style.NewStyleSheet(theme)

	// Reset.
	ss.Rule("*, *::before, *::after").
		Set("box-sizing", "border-box", "margin", "0", "padding", "0").
		End()
	ss.Rule("body").
		Set("font-family", "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
			"font-size", "16px", "line-height", "1.6",
			"color", "{colors.text}", "background", "{colors.background}").
		End()
	ss.Rule("a").
		Set("color", "{colors.primary}", "text-decoration", "none").
		End()
	ss.Rule("a:hover").Set("text-decoration", "underline").End()
	ss.Rule(".skip-link").
		Set("position", "absolute", "left", "-9999px",
			"background", "{colors.primary}", "color", "white",
			"padding", "{spacing.sm} {spacing.md}").
		End()
	ss.Rule(".skip-link:focus").Set("left", "{spacing.sm}", "top", "{spacing.sm}").End()

	// Header.
	ss.Rule(".site-header").
		Set("display", "flex", "align-items", "center", "justify-content", "space-between",
			"padding", "{spacing.md} {spacing.xl}",
			"border-bottom", "1px solid {colors.border}",
			"background", "{colors.surface}", "position", "sticky", "top", "0", "z-index", "10").
		End()
	ss.Rule(".site-header .brand").
		Set("font-weight", "700", "font-size", "1.125rem", "color", "{colors.secondary}").
		End()
	ss.Rule(".site-header nav").
		Set("display", "flex", "gap", "{spacing.lg}").End()
	ss.Rule(".site-header nav a").
		Set("color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".site-header nav a[aria-current='page']").
		Set("color", "{colors.primary}").End()

	// Footer.
	ss.Rule(".site-footer").
		Set("padding", "{spacing.xl}", "text-align", "center",
			"color", "{colors.text-muted}",
			"border-top", "1px solid {colors.border}",
			"margin-top", "{spacing.3xl}").
		End()

	// Hero.
	ss.Rule(".hero").
		Set("padding", "{spacing.3xl} {spacing.xl}", "text-align", "center",
			"background", "linear-gradient(135deg, {colors.primary} 0%, {colors.secondary} 100%)",
			"color", "white").
		End()
	ss.Rule(".hero h1").
		Set("font-size", "2.5rem", "font-weight", "800", "margin-bottom", "{spacing.md}").End()
	ss.Rule(".hero .subtitle").
		Set("font-size", "1.125rem", "max-width", "640px", "margin", "0 auto {spacing.lg}",
			"opacity", "0.9").End()
	ss.Rule(".hero .cta-row").
		Set("display", "flex", "justify-content", "center", "gap", "{spacing.md}").End()
	ss.Rule(".cta-button").
		Set("display", "inline-block", "padding", "{spacing.sm} {spacing.lg}",
			"border-radius", "{radii.md}", "font-weight", "600",
			"background", "white", "color", "{colors.primary}").End()
	ss.Rule(".cta-button.secondary").
		Set("background", "transparent", "color", "white",
			"border", "1px solid white").End()

	// Generic content container.
	ss.Rule("main").
		Set("max-width", "880px", "margin", "0 auto", "padding", "{spacing.xl}").End()

	// Feature grid.
	ss.Rule(".feature-grid").
		Set("display", "grid", "grid-template-columns", "repeat(auto-fit, minmax(240px, 1fr))",
			"gap", "{spacing.lg}", "margin", "{spacing.xl} 0").End()
	ss.Rule(".feature-card").
		Set("padding", "{spacing.lg}", "background", "{colors.surface}",
			"border", "1px solid {colors.border}", "border-radius", "{radii.md}").End()
	ss.Rule(".feature-card h3").
		Set("font-size", "1.125rem", "color", "{colors.secondary}", "margin-bottom", "{spacing.sm}").End()
	ss.Rule(".feature-card p").
		Set("color", "{colors.text-muted}", "font-size", "0.95rem").End()

	// Doc list / index.
	ss.Rule(".doc-list").
		Set("display", "grid", "gap", "{spacing.sm}", "margin", "{spacing.lg} 0").End()
	ss.Rule(".doc-list a").
		Set("display", "block", "padding", "{spacing.md}",
			"background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}", "color", "{colors.text}").End()
	ss.Rule(".doc-list a:hover").
		Set("border-color", "{colors.primary}", "text-decoration", "none").End()
	ss.Rule(".doc-list a strong").
		Set("display", "block", "color", "{colors.primary}",
			"margin-bottom", "{spacing.xs}").End()
	ss.Rule(".doc-list a span").
		Set("color", "{colors.text-muted}", "font-size", "0.9rem").End()

	// Markdown body — covers what core/markdown emits.
	ss.Rule(".doc-body h1").
		Set("font-size", "2rem", "margin", "{spacing.xl} 0 {spacing.md}",
			"color", "{colors.secondary}").End()
	ss.Rule(".doc-body h2").
		Set("font-size", "1.5rem", "margin", "{spacing.xl} 0 {spacing.md}",
			"color", "{colors.secondary}",
			"border-bottom", "1px solid {colors.border}", "padding-bottom", "{spacing.sm}").End()
	ss.Rule(".doc-body h3").
		Set("font-size", "1.2rem", "margin", "{spacing.lg} 0 {spacing.sm}",
			"color", "{colors.secondary}").End()
	ss.Rule(".doc-body p").
		Set("margin", "{spacing.md} 0").End()
	ss.Rule(".doc-body ul, .doc-body ol").
		Set("padding-left", "{spacing.xl}", "margin", "{spacing.md} 0").End()
	ss.Rule(".doc-body li").Set("margin", "{spacing.xs} 0").End()
	ss.Rule(".doc-body code").
		Set("background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"padding", "1px 6px", "border-radius", "{radii.sm}",
			"font-size", "0.9em",
			"font-family", "ui-monospace, 'SF Mono', Menlo, monospace").End()
	ss.Rule(".doc-body pre").
		Set("background", "{colors.secondary}", "color", "white",
			"padding", "{spacing.md}", "border-radius", "{radii.md}",
			"overflow-x", "auto", "margin", "{spacing.md} 0").End()
	ss.Rule(".doc-body pre code").
		Set("background", "transparent", "border", "0",
			"color", "white", "padding", "0", "font-size", "0.9em").End()
	ss.Rule(".doc-body blockquote").
		Set("border-left", "4px solid {colors.primary}",
			"padding", "{spacing.sm} {spacing.md}",
			"color", "{colors.text-muted}", "margin", "{spacing.md} 0",
			"background", "{colors.surface}").End()
	ss.Rule(".doc-body table").
		Set("width", "100%", "border-collapse", "collapse", "margin", "{spacing.md} 0").End()
	ss.Rule(".doc-body th, .doc-body td").
		Set("border", "1px solid {colors.border}", "padding", "{spacing.sm}").End()
	ss.Rule(".doc-body th").
		Set("background", "{colors.surface}", "font-weight", "600").End()
	ss.Rule(".doc-body hr").
		Set("border", "0", "border-top", "1px solid {colors.border}", "margin", "{spacing.xl} 0").End()
	ss.Rule(".doc-body a").Set("color", "{colors.primary}").End()

	// Doc-page back link.
	ss.Rule(".doc-back").
		Set("display", "inline-block", "color", "{colors.text-muted}",
			"font-size", "0.9rem", "margin-bottom", "{spacing.lg}").End()

	// Component index — tag chip on each card.
	ss.Rule(".component-tag").
		Set("display", "inline-block", "margin-left", "{spacing.sm}",
			"padding", "2px 8px", "border-radius", "{radii.full}",
			"background", "{colors.background}", "color", "{colors.text-muted}",
			"font-size", "0.75rem", "font-style", "normal",
			"font-family", "ui-monospace, 'SF Mono', Menlo, monospace").End()
	ss.Rule(".lede").
		Set("font-size", "1.05rem", "color", "{colors.text-muted}",
			"margin", "{spacing.md} 0 {spacing.xl}").End()

	// Live-demo frame for component pages.
	ss.Rule(".demo-frame").
		Set("display", "grid", "grid-template-columns", "1fr",
			"gap", "{spacing.md}", "margin", "{spacing.lg} 0 {spacing.xl}",
			"border", "1px solid {colors.border}", "border-radius", "{radii.md}",
			"overflow", "hidden", "background", "{colors.surface}").End()
	ss.Media("(min-width: 880px)", func(ss *style.StyleSheet) {
		ss.Rule(".demo-frame").
			Set("grid-template-columns", "1fr 1fr").End()
	})
	ss.Rule(".demo-live").
		Set("padding", "{spacing.lg}", "background", "{colors.background}").End()
	ss.Rule(".demo-source").
		Set("padding", "{spacing.lg}", "background", "{colors.secondary}", "color", "white",
			"overflow-x", "auto").End()
	ss.Rule(".demo-source pre, .demo-source code").
		Set("background", "transparent", "color", "white", "border", "0",
			"padding", "0", "font-size", "0.85em",
			"font-family", "ui-monospace, 'SF Mono', Menlo, monospace").End()
	ss.Rule(".demo-label").
		Set("display", "inline-block", "padding", "2px 8px", "margin-bottom", "{spacing.sm}",
			"border-radius", "{radii.sm}", "font-size", "0.7rem",
			"font-weight", "700", "letter-spacing", "0.05em", "text-transform", "uppercase").End()
	ss.Rule(".demo-live .demo-label").
		Set("background", "{colors.surface}", "color", "{colors.text-muted}",
			"border", "1px solid {colors.border}").End()
	ss.Rule(".demo-source .demo-label").
		Set("background", "rgba(255,255,255,0.12)", "color", "white").End()

	// Framework-UI demo helpers — small layout shims used only on the
	// /framework-ui/ page to align rows nicely.
	ss.Rule(".demo-stat-row").
		Set("display", "grid",
			"grid-template-columns", "repeat(auto-fit, minmax(180px, 1fr))",
			"gap", "{spacing.md}").End()
	ss.Rule(".demo-avatar-row, .demo-badge-row, .demo-trigger-row").
		Set("display", "flex", "gap", "{spacing.md}",
			"align-items", "center", "flex-wrap", "wrap").End()

	// Demo-page layout helpers (used by example screens to avoid
	// inline style attributes — a strict-CSP environment blocks any
	// style="…" attribute on rendered HTML).
	ss.Rule(".demo-stack").
		Set("display", "grid", "gap", "1rem").End()
	ss.Rule(".demo-stack--sm").Set("gap", "0.75rem").End()
	ss.Rule(".demo-stack--lg").Set("gap", "1.25rem").End()
	ss.Rule(".demo-stack--toast").
		Set("display", "grid", "gap", "0.75rem", "max-inline-size", "28rem").End()
	ss.Rule(".demo-row-flex").
		Set("display", "flex", "gap", "0.5rem", "align-items", "center", "flex-wrap", "wrap").End()
	ss.Rule(".demo-row-tight").
		Set("display", "flex", "gap", "0.75rem", "align-items", "center").End()
	ss.Rule(".demo-flex-1").Set("flex", "1").End()
	ss.Rule(".demo-row-label").
		Set("display", "block", "margin-bottom", "0.5rem",
			"font-size", "0.85rem", "color", "{colors.text-muted}").End()

	// CSS loading demo helpers.
	ss.Rule(".demo-css-card-slot").
		Set("margin-top", "{spacing.md}", "min-height", "1px").End()
	ss.Rule(".demo-watch-hint").
		Set("margin", "0 0 {spacing.sm} 0", "font-size", "0.85rem",
			"color", "{colors.text-muted}").End()
	ss.Rule(".demo-watch-hint code").
		Set("padding", "1px 6px", "border-radius", "{radii.sm}",
			"background", "{colors.surface-soft}",
			"color", "{colors.text}",
			"font-family", "{fonts.mono}",
			"font-size", "0.85em").End()
	ss.Rule(".demo-table-scroll").
		Set("overflow-x", "auto").End()

	// DataTable search demo.
	ss.Rule(".demo-search-form").
		Set("display", "flex", "gap", "{spacing.sm}",
			"align-items", "center").End()
	ss.Rule(".demo-search-input").
		Set("flex", "1", "max-inline-size", "24rem",
			"padding", "{spacing.sm} {spacing.md}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}",
			"color", "{colors.text}",
			"font", "inherit",
			"font-size", "0.95rem").End()
	ss.Rule(".demo-search-input:focus-visible").
		Set("outline", "2px solid {colors.primary}",
			"outline-offset", "1px").End()

	// CRUD demo extras.
	ss.Rule(".ui-button--small").
		Set("padding", "2px {spacing.sm}", "font-size", "0.8rem").End()
	ss.Rule(".ui-link").
		Set("color", "{colors.primary}", "text-decoration", "none",
			"font-weight", "500").End()
	ss.Rule(".ui-link:hover").Set("text-decoration", "underline").End()

	// Theme-swap demo: radios + :has() override --color-primary on the
	// preview wrapper. Pure CSS — no JS — proves the "one-token swap"
	// re-skin claim viscerally.
	ss.Rule(".theme-swap").
		Set("display", "grid", "gap", "{spacing.lg}",
			"grid-template-columns", "1fr",
			"padding", "{spacing.lg}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}").End()
	ss.Media("(min-width: 880px)", func(ss *style.StyleSheet) {
		ss.Rule(".theme-swap").
			Set("grid-template-columns", "240px 1fr").End()
	})
	ss.Rule(".theme-swap__picker").
		Set("display", "grid", "gap", "{spacing.sm}",
			"border", "0", "padding", "0").End()
	ss.Rule(".theme-swap__picker legend").
		Set("font-size", "0.75rem", "font-weight", "700",
			"text-transform", "uppercase", "letter-spacing", "0.06em",
			"color", "{colors.text-muted}", "margin-bottom", "{spacing.sm}").End()
	ss.Rule(".theme-swap__option").
		Set("display", "flex", "gap", "{spacing.sm}", "align-items", "center").End()
	ss.Rule(".theme-swap__option label").
		Set("color", "{colors.text}", "cursor", "pointer").End()
	ss.Rule(".theme-swap__preview").
		Set("display", "grid", "gap", "{spacing.md}").End()
	ss.Rule(".theme-swap__row").
		Set("display", "flex", "gap", "{spacing.md}",
			"align-items", "center", "flex-wrap", "wrap").End()
	// :has() rules: clicking a radio overrides --color-primary on the
	// nearest .theme-swap__preview sibling. CSS variables cascade, so
	// every component inside re-skins.
	ss.Rule(`.theme-swap:has(input[value="teal"]:checked) .theme-swap__preview`).
		Set("--color-primary", "#14B8A6", "--color-info", "#14B8A6").End()
	ss.Rule(`.theme-swap:has(input[value="rose"]:checked) .theme-swap__preview`).
		Set("--color-primary", "#F43F5E", "--color-info", "#F43F5E").End()
	ss.Rule(`.theme-swap:has(input[value="amber"]:checked) .theme-swap__preview`).
		Set("--color-primary", "#D97706", "--color-info", "#D97706").End()

	// core-ui + framework/ui component CSS (appended verbatim — each
	// uses CSS custom properties from the theme above).
	return ss.CSS() +
		accordion.BaseCSS() +
		tabs.BaseCSS() +
		progress.BaseCSS() +
		skeleton.BaseCSS() +
		breadcrumbs.BaseCSS() +
		pagination.BaseCSS() +
		ui.BaseCSS()
}
