package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
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

	// Dark-mode token overrides. The color-scheme bootstrap script
	// (served at /__gofastr/color-scheme.js, injected at the top of
	// <head>) sets <html data-color-scheme="dark|light"> based on the
	// user's localStorage preference + OS prefers-color-scheme. These
	// rules redefine the framework's CSS custom properties under that
	// scope so EVERY component reskins for free — no per-component
	// dark-mode code needed.
	ss.Rule(`html[data-color-scheme="dark"]`).
		Set(
			"--color-background", "#0f1115",
			"--color-surface", "#181a20",
			"--color-surface-soft", "#1f222a",
			"--color-border", "#2a2d36",
			"--color-border-strong", "#3a3d46",
			"--color-text", "#e7e7eb",
			"--color-text-muted", "#a0a0aa",
			"--color-text-subtle", "#8b8b96",
			"--color-muted", "#1f222a",
			"--color-primary", "#818cf8",
			"--color-primary-fg", "#0f1115",
			"--color-success", "#34d399",
			"--color-success-bg", "#0d2a1f",
			"--color-warning", "#fbbf24",
			"--color-danger", "#f87171",
			"--color-info", "#60a5fa",
			// Code surface stays an inkwell in dark mode too — shifted a
			// hair LIGHTER than the body background so the code panel
			// reads as an inset chip rather than blending into the page.
			"--color-code-surface", "#1a1d24",
			"--color-code-text", "#e4e4e7",
			"--color-code-border", "#2a2d36",
			"color-scheme", "dark",
		).End()
	// Explicit light mode: also defined so toggling back from dark
	// (or via the toggle's `set('light')`) reverts cleanly even on
	// pages where the :root tokens were already light.
	ss.Rule(`html[data-color-scheme="light"]`).
		Set("color-scheme", "light").End()

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
	// Inline links inside body copy need a non-color indicator so axe's
	// link-in-text-block rule passes (color contrast alone is not enough).
	ss.Rule(".seo-inline-link").Set("text-decoration", "underline").End()
	ss.Rule(".skip-link").
		Set("position", "absolute", "left", "-9999px",
			"background", "{colors.primary}", "color", "white",
			"padding", "{spacing.sm} {spacing.md}",
			// Must sit above .site-header (z-index: 10) so the focused
			// link is visible, not tucked under the sticky banner.
			"z-index", "100").
		End()
	ss.Rule(".skip-link:focus").Set("left", "{spacing.sm}", "top", "{spacing.sm}").End()

	// --- Component demo helpers (no inline styles — page CSP is strict).
	ss.Rule(".demo-button-row").
		Set("display", "flex", "gap", "{spacing.sm}", "flex-wrap", "wrap").
		End()
	ss.Rule(".demo-toast-grid").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, minmax(0, 1fr))",
			"gap", "{spacing.sm}").
		End()
	// Sidebar demo — app-shell mockup (sidebar | main pane). The
	// wide variant stacks live + source vertically even at desktop
	// widths so the app-shell has room to render in its native
	// horizontal layout.
	ss.Rule(".demo-frame.demo-frame--wide").
		Set("grid-template-columns", "1fr").End()
	ss.Media("(min-width: 880px)", func(ss *style.StyleSheet) {
		ss.Rule(".demo-frame.demo-frame--wide").
			Set("grid-template-columns", "1fr").End()
	})
	ss.Rule(".demo-app-shell").
		Set("display", "grid",
			"grid-template-columns", "240px 1fr",
			"min-height", "320px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}",
			"overflow", "hidden").
		End()
	ss.Rule(".demo-app-shell__sidebar").
		Set("border-right", "1px solid {colors.border}",
			"background", "{colors.surface-soft}",
			"overflow", "auto").
		End()
	ss.Rule(".demo-app-shell__main").
		Set("padding", "{spacing.lg}", "display", "grid",
			"gap", "{spacing.md}", "align-content", "start").
		End()
	ss.Rule(".demo-app-shell__main-header h3").
		Set("margin", "0 0 {spacing.xs}", "font-size", "1.125rem").
		End()
	ss.Rule(".demo-app-shell__main-header p").
		Set("margin", "0", "color", "{colors.text-muted}", "font-size", "0.875rem").
		End()
	ss.Rule(".demo-app-shell__cards").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, minmax(0, 1fr))",
			"gap", "{spacing.sm}").
		End()
	ss.Rule(".demo-stat-card").
		Set("background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"padding", "{spacing.md}", "display", "grid",
			"gap", "2px").
		End()
	ss.Rule(".demo-stat-card__label").
		Set("font-size", "0.75rem", "color", "{colors.text-muted}",
			"text-transform", "uppercase", "letter-spacing", "0.04em").
		End()
	ss.Rule(".demo-stat-card__value").
		Set("font-size", "1.25rem", "font-weight", "700").
		End()
	ss.Rule(".demo-stat-card__delta").
		Set("font-size", "0.75rem", "color", "{colors.text-muted}").
		End()
	// Responsive — mobile collapses the shell to a single column,
	// matches the sidebar's own < md drawer behaviour.
	ss.Media("(max-width: 47.99rem)", func(ss *style.StyleSheet) {
		ss.Rule(".demo-app-shell").
			Set("grid-template-columns", "1fr").End()
		ss.Rule(".demo-app-shell__sidebar").
			Set("display", "none").End()
	})
	ss.Media("(max-width: 30rem)", func(ss *style.StyleSheet) {
		ss.Rule(".demo-app-shell__cards").
			Set("grid-template-columns", "1fr").End()
	})
	// Variant cards.
	ss.Rule(".demo-variant-cards").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, minmax(0, 1fr))",
			"gap", "{spacing.md}",
			"margin", "{spacing.md} 0").
		End()
	ss.Rule(".demo-variant-card").
		Set("background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"padding", "{spacing.md}").
		End()
	ss.Rule(".demo-variant-card__header").
		Set("display", "flex", "align-items", "center",
			"justify-content", "space-between",
			"margin-bottom", "{spacing.xs}").
		End()
	ss.Rule(".demo-variant-card__status").
		Set("font-size", "0.7rem", "color", "{colors.text-muted}",
			"text-transform", "uppercase", "letter-spacing", "0.05em",
			"padding", "2px 6px",
			"background", "{colors.surface-soft}",
			"border-radius", "{radii.sm}").
		End()
	ss.Rule(".demo-variant-card p").
		Set("margin", "0", "font-size", "0.875rem",
			"color", "{colors.text-muted}").
		End()
	ss.Media("(max-width: 47.99rem)", func(ss *style.StyleSheet) {
		ss.Rule(".demo-variant-cards").
			Set("grid-template-columns", "1fr").End()
	})
	ss.Rule(".demo-modal-body").
		Set("padding", "{spacing.xl}", "display", "grid",
			"gap", "{spacing.md}", "min-width", "320px",
			"background", "{colors.surface}",
			"border-radius", "{radii.lg}", "box-shadow", "{shadows.lg}").
		End()
	ss.Rule(".demo-modal-body h2").
		Set("margin", "0", "font-size", "1.125rem", "font-weight", "600").
		End()
	ss.Rule(".demo-modal-body p").
		Set("margin", "0", "color", "{colors.text-muted}", "font-size", "0.95rem").
		End()
	ss.Rule(".demo-modal-actions").
		Set("display", "flex", "gap", "{spacing.sm}", "justify-content", "flex-end").
		End()
	ss.Rule(".demo-modal-field").
		Set("display", "grid", "gap", "{spacing.xs}", "font-size", "0.875rem").
		End()
	ss.Rule(".demo-modal-field input").
		Set("padding", "{spacing.sm}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"font", "inherit").
		End()
	ss.Rule(".demo-drawer-body").
		Set("padding", "{spacing.xl}", "display", "grid",
			"gap", "{spacing.md}", "height", "100%",
			"background", "{colors.surface}").
		End()
	ss.Rule(".demo-drawer-body h2").
		Set("margin", "0", "font-size", "1.125rem", "font-weight", "600").
		End()
	ss.Rule(".demo-drawer-body nav ul").
		Set("list-style", "none", "padding", "0", "margin", "0",
			"display", "grid", "gap", "{spacing.xs}").
		End()
	ss.Rule(".demo-drawer-body nav a").
		Set("display", "block", "padding", "{spacing.sm}",
			"border-radius", "{radii.sm}", "color", "{colors.text}").
		End()
	ss.Rule(".demo-drawer-body nav a:hover").
		Set("background", "{colors.surface-soft}", "text-decoration", "none").
		End()
	ss.Rule(".demo-drawer-spacer").
		Set("margin-top", "auto").
		End()
	ss.Rule(".demo-meta").
		Set("margin", "0", "color", "{colors.text-muted}",
			"font-size", "0.875rem").
		End()

	// Header.
	ss.Rule(".site-header").
		Set("display", "flex", "align-items", "center", "justify-content", "space-between",
			"flex-wrap", "wrap", // wrap on narrow viewports so nav doesn't overflow
			"gap", "{spacing.sm}",
			"padding", "{spacing.md} {spacing.xl}",
			"border-bottom", "1px solid {colors.border}",
			"background", "{colors.surface}", "position", "sticky", "top", "0", "z-index", "10").
		End()
	ss.Rule(".site-header .brand").
		// Use the canonical body-text color so contrast meets WCAG AA
		// in both light and dark themes. {colors.secondary} can be a
		// mid-gray on light bg / pale-gray on dark bg — both fail AA
		// against {colors.surface}. {colors.text} is the guaranteed
		// high-contrast token for the active background.
		Set("font-weight", "700", "font-size", "1.125rem", "color", "{colors.text}",
			// WCAG 2.5.5 — brand is a tappable link, must be >= 44×44.
			"display", "inline-flex", "align-items", "center",
			"min-height", "var(--spacing-touch-target)").
		End()
	ss.Rule(".site-header nav").
		Set("display", "flex", "flex-wrap", "wrap", "gap", "{spacing.lg}").End()
	ss.Rule(".site-header nav a").
		Set("color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".site-header nav a[aria-current='page']").
		Set("color", "{colors.primary}").End()
	// Two nav copies live in the header — the inline `.site-nav-desktop`
	// (visible ≥ 640px) and the `<details class="site-nav">` hamburger
	// (visible < 640px). Default: hide the desktop nav and show the
	// disclosure. Scoped to .site-header so the specificity beats the
	// generic `.site-header nav { display: flex }` rule above. The
	// @media (min-width: 640px) block below flips it.
	ss.Rule(".site-header .site-nav-desktop").Set("display", "none").End()

	// Mobile hamburger: <details class="site-nav"> wraps <summary> + <nav>.
	// Below 640px the summary is a 44px tap target and the nav stacks
	// vertically when details is open. `.site-header nav { display: flex }`
	// above overrides the native details-closed hide, so we force hide
	// explicitly when not [open].
	ss.Rule(".site-nav").Set("position", "relative").End()
	ss.Rule(".site-nav:not([open]) nav").Set("display", "none").End()
	ss.Rule(".site-nav__toggle").
		Set("display", "inline-flex", "align-items", "center", "justify-content", "center",
			"min-height", "var(--spacing-touch-target)",
			"min-width", "var(--spacing-touch-target)",
			"padding", "0 {spacing.md}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}",
			"color", "{colors.text}",
			"font-weight", "600",
			"cursor", "pointer",
			"list-style", "none",
			"-webkit-tap-highlight-color", "transparent").End()
	ss.Rule(".site-nav__toggle::-webkit-details-marker").
		Set("display", "none").End()
	// :focus-visible is critical — we hide the native disclosure marker
	// above, so without an explicit ring the keyboard user gets no
	// affordance on Safari/iOS where the UA default is suppressed.
	ss.Rule(".site-nav__toggle:focus-visible").
		Set("outline", "2px solid {colors.primary}",
			"outline-offset", "2px").End()
	ss.Rule(".site-nav[open] .site-nav__toggle").
		Set("background", "{colors.primary}", "color", "white",
			"border-color", "{colors.primary}").End()
	ss.Rule(".site-nav[open] nav").
		Set("position", "absolute", "top", "calc(100% + {spacing.xs})",
			"right", "0", "left", "auto",
			"flex-direction", "column", "align-items", "stretch",
			"min-width", "240px",
			"max-width", "calc(100vw - 2 * {spacing.md})",
			"padding", "{spacing.sm}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"box-shadow", "{shadows.md}",
			"z-index", "20").End()
	ss.Rule(".site-nav[open] nav a").
		Set("padding", "{spacing.sm} {spacing.md}",
			"min-height", "var(--spacing-touch-target)",
			"display", "flex", "align-items", "center").End()
	// >=640px: show the inline desktop nav, hide the hamburger details.
	// We render two nav copies in the DOM so we can avoid the long-
	// standing Chrome layout bug where a nav inside a closed <details>
	// (with details using display:contents) collapses to zero
	// inline-size. The two copies share the same link list but only
	// one is in the visual layout per viewport.
	ss.Media("(min-width: 640px)", func(ss *style.StyleSheet) {
		ss.Rule(".site-header .site-nav").Set("display", "none").End()
		ss.Rule(".site-header .site-nav-desktop").
			Set("display", "flex", "align-items", "center", "gap", "{spacing.lg}",
				"flex-wrap", "wrap").End()
		ss.Rule(".site-header .site-nav-desktop a").
			Set("padding", "0", "min-height", "0", "display", "inline-flex").End()
	})

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
	// CTA buttons appear both inside the hero gradient AND on regular
	// page backgrounds (e.g. the toast demo). Use the primary + primary-fg
	// token pair so contrast is guaranteed AA in both contexts (the
	// framework's Button component uses the same pair and is axe-clean).
	ss.Rule(".cta-button").
		Set("display", "inline-flex", "align-items", "center", "justify-content", "center",
			"min-height", "44px", // WCAG 2.5.5 tap target
			"padding", "10px {spacing.lg}",
			"border-radius", "{radii.md}", "font-weight", "600",
			"background", "{colors.primary}", "color", "{colors.primary-fg}",
			"border", "1px solid {colors.primary}").End()
	// Secondary lives ONLY on the hero gradient — white border + white
	// text on the indigo-to-slate gradient passes 8:1+ contrast.
	ss.Rule(".cta-button.secondary").
		Set("background", "transparent", "color", "white",
			"border", "1px solid white").End()
	// :focus-visible — both CTAs sit on the indigo/slate hero gradient,
	// so a white outline is the only high-contrast option (~12:1 vs
	// either gradient stop). WCAG 2.4.7 — focus indicator visible.
	ss.Rule(".cta-button:focus-visible, .cta-button.secondary:focus-visible").
		Set("outline", "2px solid white", "outline-offset", "2px").End()

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
	// Generic <pre> overflow guard — long code lines (esp. on home
	// page quickstart) must not drag the body wider than the viewport.
	// Each specialized .doc-body / .demo-source rule below can still
	// override visuals; this rule just guarantees containment.
	ss.Rule("main pre").Set("overflow-x", "auto", "max-width", "100%").End()
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
		Set("display", "inline-flex", "align-items", "center",
			"color", "{colors.text-muted}",
			"font-size", "0.9rem", "margin-bottom", "{spacing.lg}",
			// WCAG 2.5.5 tap target.
			"min-height", "var(--spacing-touch-target)").End()

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
	// .demo-source uses the near-black --color-text token instead of
	// --color-secondary (mid-gray). Mid-gray next to the light page
	// surface reads as "washed out" and triggers a Mach-band contrast
	// illusion that looks like a horizontal gradient even though the
	// pixels are uniform. Near-black gives the panel a stronger,
	// inkier feel and matches .ui-code-block.
	// .demo-source is now a thin wrapper around ui.CodeBlock — the
	// CodeBlock owns the code-surface background, padding, and font.
	// The wrapper just provides outer padding for the "Source" label
	// and uses the same code-surface token so the label chrome blends
	// seamlessly into the block below in BOTH color schemes.
	//
	// Previously: `background: var(--color-text)` + literal #E4E4E7,
	// which inverted (broken) in dark mode because Text flipped light.
	ss.Rule(".demo-source").
		Set("padding", "{spacing.lg}", "background", "{colors.code-surface}",
			"color", "{colors.code-text}", "overflow-x", "auto").End()
	// Strip the CodeBlock's own outer border + radius inside the
	// .demo-source wrapper so it visually merges with the wrapper
	// (the demo-frame already provides the outer border + radius).
	ss.Rule(`.demo-source [data-fui-comp="ui-code-block"]`).
		Set("background", "transparent", "border", "0", "padding", "0",
			"border-radius", "0", "color", "{colors.code-text}").End()
	ss.Rule(".demo-label").
		Set("display", "inline-block", "padding", "2px 8px", "margin-bottom", "{spacing.sm}",
			"border-radius", "{radii.sm}", "font-size", "0.7rem",
			"font-weight", "700", "letter-spacing", "0.05em", "text-transform", "uppercase").End()
	ss.Rule(".demo-live .demo-label").
		Set("background", "{colors.surface}", "color", "{colors.text-muted}",
			"border", "1px solid {colors.border}").End()
	ss.Rule(".demo-source .demo-label").
		Set("background", "rgba(255,255,255,0.12)", "color", "white").End()

	// Sticky demo helper — a fixed-height scrollable container so the
	// sticky element can be seen pinning to its edge.
	ss.Rule(".demo-sticky-scroll").
		Set("max-height", "300px",
			"overflow-y", "auto",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"padding", "{spacing.md}").End()
	ss.Rule(".demo-sticky-scroll > p").
		Set("padding", "{spacing.sm} 0",
			"color", "{colors.text-muted}").End()
	ss.Rule(".demo-sticky-scroll > ul").
		Set("list-style", "none",
			"padding", "0",
			"margin", "0").End()
	ss.Rule(".demo-sticky-scroll > ul > li").
		Set("padding", "{spacing.sm} {spacing.md}",
			"border-bottom", "1px solid {colors.border}").End()

	// AspectRatio demo grid
	ss.Rule(".demo-ar-grid").
		Set("display", "grid",
			"grid-template-columns", "repeat(auto-fill, minmax(220px, 1fr))",
			"gap", "{spacing.md}").End()
	ss.Rule(".demo-ar-box").
		Set("display", "flex",
			"align-items", "center",
			"justify-content", "center",
			"font-weight", "600",
			"color", "{colors.text-muted}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}").End()

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

	// InfiniteScroll demo: a fixed-height scroll container so the
	// feed has somewhere to actually scroll. Without this the page
	// scrolls and the drain loop fires every page on first paint.
	ss.Rule(".demo-infinite-frame").
		Set("max-block-size", "26rem", "overflow-y", "auto",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}").End()
	ss.Rule(".demo-infinite-frame .demo-feed-item").
		Set("padding", "{spacing.md} {spacing.lg}",
			"border-bottom", "1px solid {colors.border}").End()
	ss.Rule(".demo-infinite-frame .demo-feed-item:last-child").
		Set("border-bottom", "0").End()
	ss.Rule(".demo-infinite-frame .demo-feed-item h3").
		Set("margin", "0 0 {spacing.xs} 0", "font-size", "0.95rem").End()
	ss.Rule(".demo-infinite-frame .demo-feed-item p").
		Set("margin", "0", "color", "{colors.text-muted}",
			"font-size", "0.85rem").End()

	// AvatarGroup demo helpers: a row showing the four sizes, and a
	// trigger row that pairs an AvatarGroup with a "View team" button.
	ss.Rule(".demo-avatar-sizes").
		Set("display", "flex", "flex-direction", "column",
			"gap", "{spacing.md}", "align-items", "flex-start").End()
	ss.Rule(".demo-team-trigger").
		Set("display", "flex", "gap", "{spacing.md}",
			"align-items", "center", "flex-wrap", "wrap").End()
	ss.Rule(".demo-team-popover").
		Set("padding", "{spacing.md}", "min-inline-size", "18rem",
			"max-inline-size", "24rem").End()
	ss.Rule(".demo-team-title").
		Set("margin", "0 0 {spacing.sm} 0", "font-size", "0.85rem",
			"font-weight", "600", "color", "{colors.text-muted}",
			"text-transform", "uppercase", "letter-spacing", "0.05em").End()
	ss.Rule(".demo-team-list").
		Set("list-style", "none", "margin", "0", "padding", "0",
			"display", "grid", "gap", "{spacing.sm}").End()
	ss.Rule(".demo-team-row").
		Set("display", "grid",
			"grid-template-columns", "auto 1fr auto",
			"gap", "{spacing.sm}", "align-items", "center").End()
	ss.Rule(".demo-team-name").
		Set("font-weight", "600", "color", "{colors.text}").End()
	ss.Rule(".demo-team-role").
		Set("font-size", "0.8rem", "color", "{colors.text-muted}").End()

	// Popover demo — vertical spacer that pushes the next row down
	// near the viewport bottom so anchored popovers from those
	// triggers have to flip up to fit. Constrained to leave headroom
	// at 800h viewports (used by chromedp/e2e tests).
	ss.Rule(".popover-demo-spacer").
		Set("block-size", "40vh", "min-block-size", "320px").End()

	// .popover-demo-sides — four buttons centered, each demonstrating
	// a specific side. Spacing wide enough that the popover doesn't
	// overlap a sibling button.
	ss.Rule(".popover-demo-sides").
		Set("display", "grid", "grid-template-columns", "repeat(4, minmax(0, 1fr))",
			"gap", "{spacing.md}", "place-items", "center",
			"padding", "{spacing.lg} 0").End()
	// Narrower viewports: collapse to 2 columns so the row fits.
	ss.Media("(max-width: 720px)", func(ss *style.StyleSheet) {
		ss.Rule(".popover-demo-sides").
			Set("grid-template-columns", "repeat(2, minmax(0, 1fr))").End()
	})

	// .popover-demo-edges — tall frame with a trigger in each corner.
	// Frame height is sized so bottom-corner triggers actually sit
	// near the viewport bottom (so auto-flip flips them UP), not just
	// the page-middle. 90vh gives the demo room without forcing
	// overflow on portrait phones.
	ss.Rule(".popover-demo-edges").
		Set("padding", "{spacing.md} 0").End()
	ss.Rule(".popover-demo-edges__inner").
		Set("position", "relative",
			"block-size", "min(90vh, 720px)",
			"min-block-size", "420px",
			"max-inline-size", "100%",
			"background", "{colors.surface-soft}",
			"border", "1px dashed {colors.border}",
			"border-radius", "{radii.md}").End()
	ss.Rule(".popover-demo-edges__tl, .popover-demo-edges__tr, .popover-demo-edges__bl, .popover-demo-edges__br").
		Set("position", "absolute").End()
	ss.Rule(".popover-demo-edges__tl").
		Set("inset-block-start", "12px", "inset-inline-start", "12px").End()
	ss.Rule(".popover-demo-edges__tr").
		Set("inset-block-start", "12px", "inset-inline-end", "12px").End()
	ss.Rule(".popover-demo-edges__bl").
		Set("inset-block-end", "12px", "inset-inline-start", "12px").End()
	ss.Rule(".popover-demo-edges__br").
		Set("inset-block-end", "12px", "inset-inline-end", "12px").End()

	// Popover body — show "Opened from: X"
	ss.Rule(".popover-demo-body__from").
		Set("margin", "0 0 {spacing.sm} 0", "color", "{colors.text-muted}",
			"font-size", "0.85rem").End()
	ss.Rule(".popover-demo-body__from strong").
		Set("color", "{colors.primary}", "font-weight", "600").End()

	// Anchored-popover chrome, arrow, and trigger-active highlight now
	// ship as framework defaults in core-ui/widget/server.go's
	// widgetCSS. Apps that want a different look override those rules
	// here instead of redefining them.

	// Spinner demo — opt-in container that simulates
	// prefers-reduced-motion locally so users can see the
	// motion-reduced variant without changing their OS setting.
	// We slow the keyframe animation by overriding the duration via
	// a CSS custom property the spinner stylesheet reads.
	ss.Rule(".demo-spinner-reduced [data-fui-comp=\"ui-spinner\"] .ui-spinner__ring").
		Set("animation-duration", "3s").End()
	ss.Rule(".demo-spinner-reduced [data-fui-comp=\"ui-spinner\"] .ui-spinner__dot").
		Set("animation-duration", "3s").End()
	ss.Rule(".demo-spinner-reduced").
		Set("padding", "{spacing.md}", "background", "{colors.surface-soft}",
			"border", "1px dashed {colors.border}", "border-radius", "{radii.md}").End()

	// FileUpload demo — server-response island styling.
	ss.Rule(".demo-upload-result").
		Set("margin-top", "{spacing.md}", "padding", "{spacing.lg}",
			"background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}").End()
	ss.Rule(".demo-upload-result__empty").
		Set("margin", "0", "color", "{colors.text-muted}", "font-size", "0.9rem").End()
	ss.Rule(".demo-upload-result__error").
		Set("margin", "0", "color", "{colors.danger}", "font-weight", "600").End()
	ss.Rule(".demo-upload-result__title").
		Set("margin", "0 0 {spacing.sm} 0", "color", "{colors.text}",
			"font-weight", "600").End()
	ss.Rule(".demo-upload-result__list").
		Set("margin", "0", "padding-inline-start", "{spacing.lg}",
			"color", "{colors.text}", "font-size", "0.9rem").End()
	ss.Rule(".fileupload-hint").
		Set("color", "{colors.text-muted}", "font-size", "0.85rem").End()
	ss.Rule(".demo-stack--toast").
		Set("display", "grid", "gap", "0.75rem", "max-inline-size", "28rem").End()
	// Carousel slide demo chrome — keeps the example slides legible
	// against the colored SVG art without bleeding component styling
	// into the framework's Carousel component.
	ss.Rule(".demo-carousel-slide").
		Set("display", "grid", "gap", "{spacing.sm}",
			"padding", "{spacing.md}",
			"background", "{colors.surface}",
			"border-radius", "{radii.md}",
			"border", "1px solid {colors.border}").End()
	ss.Rule(".demo-carousel-slide__title").
		Set("margin", "0", "font-size", "1rem", "font-weight", "600").End()
	ss.Rule(".demo-carousel-slide__body").
		Set("margin", "0", "font-size", "0.85rem",
			"color", "{colors.text-muted}").End()
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

	// Themed section-override demo grid.
	ss.Rule(".themed-demo__grid").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.lg}",
			"margin-top", "{spacing.lg}").End()
	ss.Rule(".themed-demo__panel").
		Set("background", "{colors.background}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}",
			"padding", "{spacing.lg}").End()
	ss.Rule(".themed-demo__label").
		Set("margin", "0 0 {spacing.md} 0",
			"font-size", "0.75rem",
			"text-transform", "uppercase",
			"letter-spacing", "0.06em",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".themed-demo__sample").
		Set("display", "grid",
			"gap", "{spacing.md}").End()
	ss.Rule(".themed-demo__actions").
		Set("display", "flex",
			"gap", "{spacing.sm}",
			"margin-top", "{spacing.md}").End()
	ss.Rule(".themed-demo__grid").
		Media("(max-width: 768px)", func(ss *style.StyleSheet) {
			ss.Rule(".themed-demo__grid").
				Set("grid-template-columns", "1fr").End()
		}).End()

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
			"font-size", "0.95rem",
			// WCAG 2.5.5 — text inputs are tappable; meet the 44px floor.
			"min-block-size", "var(--spacing-touch-target)").End()
	ss.Rule(".demo-search-input:focus-visible").
		Set("outline", "2px solid {colors.primary}",
			"outline-offset", "1px").End()

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
		ui.BaseCSS()
}
