// Package theme provides the framework's default page theme — the
// visual identity for any app built via core-ui (or its consumers like
// kiln). Hosts get token-driven palette/spacing/typography out of the
// box, plus utility classes (kiln-section, kiln-card, kiln-button,
// kiln-grid-*, …) that reference the tokens.
//
// A host can override individual tokens by passing a custom Theme to
// PageTheme — every utility class re-resolves to the new value, so a
// single token swap re-skins the whole app.
package theme

import "github.com/gofastr/gofastr/core-ui/style"

// PageTheme returns the page theme, optionally merged with overrides.
// Override entries replace base tokens of the same name.
//
// Tokens are exposed as `:root` CSS custom properties via
// theme.CSSCustomProperties() (e.g. --color-page-bg). The utility
// classes below reference them with `var(--<token>)` indirection so
// any future override re-skins all pages without touching rules.
func PageTheme(overrides ...style.Theme) style.Theme {
	base := style.DefaultTheme()
	t := style.MergeThemes(base, defaultPageOverlay())
	for _, o := range overrides {
		t = style.MergeThemes(t, o)
	}
	return t
}

func defaultPageOverlay() style.Theme {
	return style.Theme{
		Name: "page",
		Colors: style.Colors{
			"page-bg":           "#FAF9F6",
			"page-surface":      "#FFFFFF",
			"page-surface-soft": "#F5F4F1",
			"page-border":       "#E5E1DA",
			"page-fg":           "#0F172A",
			"page-fg-soft":      "#37352F",
			"page-fg-muted":     "#6B7280",
			"page-fg-subtle":    "#9B9A97",
			"page-primary":      "#0F172A",
			"page-primary-fg":   "#FFFFFF",
			"page-accent":       "#4F8CFF",
		},
		Spacing: style.Spacing{
			"page-xs": 4,
			"page-sm": 8,
			"page-md": 12,
			"page-lg": 24,
			"page-xl": 48,
		},
		Radii: style.Radii{
			"page-sm": 4,
			"page-md": 8,
			"page-lg": 14,
		},
		Fonts: style.Fonts{
			"page": "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Inter, Roboto, sans-serif",
			"mono": "ui-monospace, SFMono-Regular, \"SF Mono\", Menlo, Consolas, monospace",
		},
	}
}

// PageCSS builds the page stylesheet by resolving theme tokens through
// core-ui/style. Output:
//
//	1. :root CSS custom properties (every token in the theme)
//	2. body.kiln-app reset + base typography
//	3. Layout primitives:  kiln-container[-{sm,md,lg}], kiln-section[-{soft,inverse}]
//	4. Stacks/rows/grids:  kiln-stack[-{sm,lg}], kiln-row[-{end,between,wrap}], kiln-grid-{2,3,4,auto}
//	5. Components:         kiln-card[-soft], kiln-button[-{secondary,ghost}], kiln-nav,
//	                       kiln-footer, kiln-pill, kiln-quote, kiln-hero, kiln-eyebrow
//	6. Default styles for native form controls + tables on body.kiln-app
//
// Class names use the kiln- prefix for backwards compatibility with
// pages already authored against them. The framework owns these names
// going forward.
func PageCSS(t style.Theme) string {
	ss := style.NewStyleSheet(t)

	// Reset + base.
	ss.Rule("*, *::before, *::after").Set("box-sizing", "border-box").End()
	ss.Rule("body.kiln-app").
		Set(
			"margin", "0",
			"background", "{colors.page-bg}",
			"color", "{colors.page-fg}",
			"font-family", "{fonts.page}",
			"font-size", "16px",
			"line-height", "1.5",
			"-webkit-font-smoothing", "antialiased",
		).
		End()
	ss.Rule("body.kiln-app a").Set("color", "{colors.page-fg}", "text-decoration", "none").End()
	ss.Rule("body.kiln-app a:hover").Set("text-decoration", "underline").End()
	ss.Rule("body.kiln-app h1, body.kiln-app h2, body.kiln-app h3, body.kiln-app h4, body.kiln-app h5, body.kiln-app h6").
		Set("margin", "0", "line-height", "1.2", "letter-spacing", "-0.02em").
		End()

	// Layout containers.
	ss.Rule(".kiln-page").Set("min-height", "100vh").End()
	for _, c := range []struct{ cls, width string }{
		{"kiln-container", "1200px"},
		{"kiln-container-sm", "720px"},
		{"kiln-container-md", "960px"},
		{"kiln-container-lg", "1200px"},
	} {
		ss.Rule("." + c.cls).
			Set("max-width", c.width, "margin", "0 auto", "padding", "0 {spacing.page-lg}").
			End()
	}

	ss.Rule(".kiln-section").Set("padding", "{spacing.page-xl} {spacing.page-lg}").End()
	ss.Rule(".kiln-section-soft").Set("background", "{colors.page-surface-soft}").End()
	ss.Rule(".kiln-section-inverse").
		Set("background", "{colors.page-primary}", "color", "{colors.page-primary-fg}").
		End()
	ss.Rule(".kiln-section-inverse a").Set("color", "{colors.page-primary-fg}").End()

	// Stacks / rows / grids.
	for _, s := range []struct{ cls, gap string }{
		{"kiln-stack", "{spacing.page-md}"},
		{"kiln-stack-sm", "{spacing.page-sm}"},
		{"kiln-stack-lg", "{spacing.page-lg}"},
	} {
		ss.Rule("." + s.cls).Set("display", "flex", "flex-direction", "column", "gap", s.gap).End()
	}
	for _, r := range []struct{ cls, justify string }{
		{"kiln-row", ""},
		{"kiln-row-end", "flex-end"},
		{"kiln-row-between", "space-between"},
		{"kiln-row-wrap", ""},
	} {
		props := []string{"display", "flex", "align-items", "center", "gap", "{spacing.page-md}"}
		if r.justify != "" {
			props = append(props, "justify-content", r.justify)
		}
		if r.cls == "kiln-row-wrap" {
			props = append(props, "flex-wrap", "wrap")
		}
		ss.Rule("." + r.cls).Set(props...).End()
	}
	ss.Rule(".kiln-grid-2").Set("display", "grid", "grid-template-columns", "repeat(2, 1fr)", "gap", "{spacing.page-lg}").End()
	ss.Rule(".kiln-grid-3").Set("display", "grid", "grid-template-columns", "repeat(3, 1fr)", "gap", "{spacing.page-lg}").End()
	ss.Rule(".kiln-grid-4").Set("display", "grid", "grid-template-columns", "repeat(4, 1fr)", "gap", "{spacing.page-md}").End()
	ss.Rule(".kiln-grid-auto").Set("display", "grid", "grid-template-columns", "repeat(auto-fit, minmax(220px, 1fr))", "gap", "{spacing.page-md}").End()

	// Typography helpers.
	ss.Rule(".kiln-center").Set("text-align", "center").End()
	ss.Rule(".kiln-muted").Set("color", "{colors.page-fg-muted}").End()
	ss.Rule(".kiln-subtle").Set("color", "{colors.page-fg-subtle}").End()
	ss.Rule(".kiln-eyebrow").
		Set(
			"text-transform", "uppercase",
			"letter-spacing", "0.08em",
			"font-size", "0.75rem",
			"font-weight", "700",
			"color", "{colors.page-fg}",
			"margin", "0 0 {spacing.page-sm}",
		).
		End()
	ss.Rule(".kiln-display").Set("font-size", "3rem", "font-weight", "700").End()
	ss.Rule(".kiln-title").Set("font-size", "2.25rem", "font-weight", "700").End()
	ss.Rule(".kiln-h2").Set("font-size", "1.5rem", "font-weight", "700").End()

	// Hero.
	ss.Rule(".kiln-hero").
		Set("text-align", "center", "padding", "calc({spacing.page-xl} * 1.5) {spacing.page-lg} {spacing.page-xl}").
		End()
	ss.Rule(".kiln-hero h1").
		Set("font-size", "3rem", "font-weight", "700", "max-width", "24ch", "margin", "0 auto {spacing.page-md}").
		End()
	ss.Rule(".kiln-hero p").
		Set("font-size", "1.125rem", "color", "{colors.page-fg-soft}", "max-width", "60ch", "margin", "0 auto {spacing.page-lg}").
		End()

	// Cards.
	ss.Rule(".kiln-card").
		Set(
			"background", "{colors.page-surface}",
			"border", "1px solid {colors.page-border}",
			"border-radius", "{radii.page-lg}",
			"padding", "{spacing.page-lg}",
			"box-shadow", "0 1px 2px rgba(15, 23, 42, 0.06)",
		).
		End()
	ss.Rule(".kiln-card-soft").
		Set(
			"background", "{colors.page-surface-soft}",
			"border", "1px solid {colors.page-border}",
			"border-radius", "{radii.page-lg}",
			"padding", "{spacing.page-lg}",
		).
		End()

	// Buttons.
	ss.Rule(".kiln-button").
		Set(
			"display", "inline-flex",
			"align-items", "center",
			"gap", "{spacing.page-sm}",
			"background", "{colors.page-primary}",
			"color", "{colors.page-primary-fg}",
			"border", "1px solid {colors.page-primary}",
			"padding", "10px 18px",
			"border-radius", "{radii.page-md}",
			"font-weight", "600",
			"cursor", "pointer",
			"text-decoration", "none",
		).
		End()
	ss.Rule(".kiln-button:hover").Set("filter", "brightness(1.08)", "text-decoration", "none").End()
	ss.Rule(".kiln-button-secondary").
		Set("background", "{colors.page-surface}", "color", "{colors.page-fg}", "border-color", "{colors.page-border}").
		End()
	ss.Rule(".kiln-button-ghost").
		Set("background", "transparent", "color", "{colors.page-fg}", "border-color", "transparent").
		End()

	// Nav / footer.
	ss.Rule(".kiln-nav").
		Set(
			"display", "flex",
			"align-items", "center",
			"justify-content", "space-between",
			"gap", "{spacing.page-lg}",
			"padding", "{spacing.page-md} {spacing.page-lg}",
			"border-bottom", "1px solid {colors.page-border}",
		).
		End()
	ss.Rule(".kiln-nav-links").
		Set("display", "flex", "gap", "{spacing.page-lg}", "align-items", "center").
		End()
	ss.Rule(".kiln-nav-links a").Set("color", "{colors.page-fg-soft}").End()
	ss.Rule(".kiln-footer").
		Set(
			"border-top", "1px solid {colors.page-border}",
			"background", "{colors.page-surface-soft}",
			"padding", "{spacing.page-xl} {spacing.page-lg} {spacing.page-lg}",
		).
		End()

	// Pills / badges.
	ss.Rule(".kiln-pill").
		Set(
			"display", "inline-flex",
			"align-items", "center",
			"gap", "6px",
			"background", "{colors.page-surface-soft}",
			"border", "1px solid {colors.page-border}",
			"border-radius", "999px",
			"padding", "4px 10px",
			"font-size", "0.75rem",
			"font-weight", "600",
			"color", "{colors.page-fg-soft}",
		).
		End()
	ss.Rule(".kiln-badge-success").Set("color", "{colors.success}", "font-weight", "600").End()
	ss.Rule(".kiln-badge-warning").Set("color", "{colors.warning}", "font-weight", "600").End()
	ss.Rule(".kiln-badge-danger").Set("color", "{colors.danger}", "font-weight", "600").End()

	// Quote.
	ss.Rule(".kiln-quote").
		Set(
			"font-size", "1.5rem",
			"line-height", "1.5",
			"color", "{colors.page-fg}",
			"font-weight", "500",
			"max-width", "60ch",
			"margin", "0 auto {spacing.page-lg}",
			"text-align", "center",
		).
		End()

	// Native form controls + tables.
	formInputs := `body.kiln-app input[type="text"],` +
		`body.kiln-app input[type="email"],` +
		`body.kiln-app input[type="number"],` +
		`body.kiln-app input[type="search"],` +
		`body.kiln-app input[type="password"],` +
		`body.kiln-app textarea,` +
		`body.kiln-app select`
	ss.Rule(formInputs).
		Set(
			"width", "100%",
			"background", "{colors.page-surface}",
			"border", "1px solid {colors.page-border}",
			"border-radius", "{radii.page-md}",
			"padding", "8px 12px",
			"color", "{colors.page-fg}",
			"font", "inherit",
		).
		End()
	ss.Rule(`body.kiln-app input:focus, body.kiln-app textarea:focus, body.kiln-app select:focus`).
		Set("outline", "none", "border-color", "{colors.page-accent}").
		End()
	ss.Rule("body.kiln-app table").Set("width", "100%", "border-collapse", "collapse").End()
	ss.Rule("body.kiln-app th, body.kiln-app td").
		Set(
			"padding", "{spacing.page-sm} {spacing.page-md}",
			"border-bottom", "1px solid {colors.page-border}",
			"text-align", "left",
		).
		End()
	ss.Rule("body.kiln-app th").
		Set("font-weight", "600", "color", "{colors.page-fg-soft}", "background", "{colors.page-surface-soft}").
		End()

	return t.CSSCustomProperties() + "\n" + ss.CSS()
}
