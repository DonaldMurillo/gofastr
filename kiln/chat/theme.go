package chat

import (
	"github.com/gofastr/gofastr/core-ui/style"
)

// pageTheme is the theme used by every kiln-rendered page (the
// agent-built UI). Built on top of core-ui's DefaultTheme so framework
// tokens stay consistent across kiln + plain gofastr apps.
//
// Tokens are exposed as `:root` CSS custom properties via
// theme.CSSCustomProperties(), and the utility classes below reference
// them with `var(--<token>)`. Same indirection means a single token
// override (eventually via world.App.Theme) re-skins every page.
//
// Naming convention: `kiln-page-…` for tokens this theme owns, plain
// names (primary, success, etc.) re-export the framework defaults so
// agents and pages can rely on either.
func pageTheme() style.Theme {
	base := style.DefaultTheme()
	overrides := style.Theme{
		Name: "kiln-page",
		Colors: style.Colors{
			"kiln-page-bg":           "#FAF9F6",
			"kiln-page-surface":      "#FFFFFF",
			"kiln-page-surface-soft": "#F5F4F1",
			"kiln-page-border":       "#E5E1DA",
			"kiln-page-fg":           "#0F172A",
			"kiln-page-fg-soft":      "#37352F",
			"kiln-page-fg-muted":     "#6B7280",
			"kiln-page-fg-subtle":    "#9B9A97",
			"kiln-page-primary":      "#0F172A",
			"kiln-page-primary-fg":   "#FFFFFF",
			"kiln-page-accent":       "#4F8CFF",
		},
		Spacing: style.Spacing{
			"kiln-pg-xs": 4,
			"kiln-pg-sm": 8,
			"kiln-pg-md": 12,
			"kiln-pg-lg": 24,
			"kiln-pg-xl": 48,
		},
		Radii: style.Radii{
			"kiln-pg-sm": 4,
			"kiln-pg-md": 8,
			"kiln-pg-lg": 14,
		},
		Fonts: style.Fonts{
			"kiln-page": "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Inter, Roboto, sans-serif",
			"kiln-mono": "ui-monospace, SFMono-Regular, \"SF Mono\", Menlo, Consolas, monospace",
		},
	}
	return style.MergeThemes(base, overrides)
}

// pageCSS builds the page-side stylesheet. Every kiln-rendered page
// links it at /kiln/theme.css; class-based styling is the supported
// path for agents (inline `style=` is dropped at the renderer).
//
// What's emitted:
//
//	1. :root CSS custom properties for every theme token
//	2. body.kiln-app reset + base typography
//	3. Layout primitives:  kiln-container[-{sm,md,lg}], kiln-section[-{soft,inverse}]
//	4. Stacks/rows/grids:  kiln-stack[-{sm,lg}], kiln-row[-{end,between,wrap}], kiln-grid-{2,3,4,auto}
//	5. Components:         kiln-card[-soft], kiln-button[-{secondary,ghost}], kiln-nav,
//	                        kiln-footer, kiln-pill, kiln-quote, kiln-hero, kiln-eyebrow
//	6. Default styles for native form controls + tables on body.kiln-app
func pageCSS() string {
	theme := pageTheme()
	ss := style.NewStyleSheet(theme)

	// ---- Reset + base ---------------------------------------------------
	ss.Rule("*, *::before, *::after").Set("box-sizing", "border-box").End()

	ss.Rule("body.kiln-app").
		Set(
			"margin", "0",
			"background", "{colors.kiln-page-bg}",
			"color", "{colors.kiln-page-fg}",
			"font-family", "{fonts.kiln-page}",
			"font-size", "16px",
			"line-height", "1.5",
			"-webkit-font-smoothing", "antialiased",
		).
		End()
	ss.Rule("body.kiln-app a").
		Set("color", "{colors.kiln-page-fg}", "text-decoration", "none").
		End()
	ss.Rule("body.kiln-app a:hover").Set("text-decoration", "underline").End()
	ss.Rule("body.kiln-app h1, body.kiln-app h2, body.kiln-app h3, body.kiln-app h4, body.kiln-app h5, body.kiln-app h6").
		Set("margin", "0", "line-height", "1.2", "letter-spacing", "-0.02em").
		End()

	// ---- Layout containers ---------------------------------------------
	ss.Rule(".kiln-page").Set("min-height", "100vh").End()

	for _, c := range []struct {
		cls   string
		width string
	}{
		{"kiln-container", "1200px"},
		{"kiln-container-sm", "720px"},
		{"kiln-container-md", "960px"},
		{"kiln-container-lg", "1200px"},
	} {
		ss.Rule("." + c.cls).
			Set(
				"max-width", c.width,
				"margin", "0 auto",
				"padding", "0 {spacing.kiln-pg-lg}",
			).
			End()
	}

	ss.Rule(".kiln-section").
		Set("padding", "{spacing.kiln-pg-xl} {spacing.kiln-pg-lg}").
		End()
	ss.Rule(".kiln-section-soft").Set("background", "{colors.kiln-page-surface-soft}").End()
	ss.Rule(".kiln-section-inverse").
		Set("background", "{colors.kiln-page-primary}", "color", "{colors.kiln-page-primary-fg}").
		End()
	ss.Rule(".kiln-section-inverse a").Set("color", "{colors.kiln-page-primary-fg}").End()

	// ---- Stacks / rows / grids -----------------------------------------
	for _, s := range []struct {
		cls, gap string
	}{
		{"kiln-stack", "{spacing.kiln-pg-md}"},
		{"kiln-stack-sm", "{spacing.kiln-pg-sm}"},
		{"kiln-stack-lg", "{spacing.kiln-pg-lg}"},
	} {
		ss.Rule("." + s.cls).
			Set("display", "flex", "flex-direction", "column", "gap", s.gap).
			End()
	}
	for _, r := range []struct {
		cls, justify string
	}{
		{"kiln-row", ""},
		{"kiln-row-end", "flex-end"},
		{"kiln-row-between", "space-between"},
		{"kiln-row-wrap", ""}, // wrap added below
	} {
		props := []string{"display", "flex", "align-items", "center", "gap", "{spacing.kiln-pg-md}"}
		if r.justify != "" {
			props = append(props, "justify-content", r.justify)
		}
		if r.cls == "kiln-row-wrap" {
			props = append(props, "flex-wrap", "wrap")
		}
		ss.Rule("." + r.cls).Set(props...).End()
	}
	ss.Rule(".kiln-grid-2").Set("display", "grid", "grid-template-columns", "repeat(2, 1fr)", "gap", "{spacing.kiln-pg-lg}").End()
	ss.Rule(".kiln-grid-3").Set("display", "grid", "grid-template-columns", "repeat(3, 1fr)", "gap", "{spacing.kiln-pg-lg}").End()
	ss.Rule(".kiln-grid-4").Set("display", "grid", "grid-template-columns", "repeat(4, 1fr)", "gap", "{spacing.kiln-pg-md}").End()
	ss.Rule(".kiln-grid-auto").Set("display", "grid", "grid-template-columns", "repeat(auto-fit, minmax(220px, 1fr))", "gap", "{spacing.kiln-pg-md}").End()

	// ---- Typography helpers --------------------------------------------
	ss.Rule(".kiln-center").Set("text-align", "center").End()
	ss.Rule(".kiln-muted").Set("color", "{colors.kiln-page-fg-muted}").End()
	ss.Rule(".kiln-subtle").Set("color", "{colors.kiln-page-fg-subtle}").End()
	ss.Rule(".kiln-eyebrow").
		Set(
			"text-transform", "uppercase",
			"letter-spacing", "0.08em",
			"font-size", "0.75rem",
			"font-weight", "700",
			"color", "{colors.kiln-page-fg}",
			"margin", "0 0 {spacing.kiln-pg-sm}",
		).
		End()
	ss.Rule(".kiln-display").Set("font-size", "3rem", "font-weight", "700").End()
	ss.Rule(".kiln-title").Set("font-size", "2.25rem", "font-weight", "700").End()
	ss.Rule(".kiln-h2").Set("font-size", "1.5rem", "font-weight", "700").End()

	// ---- Hero ----------------------------------------------------------
	ss.Rule(".kiln-hero").
		Set(
			"text-align", "center",
			"padding", "calc({spacing.kiln-pg-xl} * 1.5) {spacing.kiln-pg-lg} {spacing.kiln-pg-xl}",
		).
		End()
	ss.Rule(".kiln-hero h1").
		Set("font-size", "3rem", "font-weight", "700", "max-width", "24ch", "margin", "0 auto {spacing.kiln-pg-md}").
		End()
	ss.Rule(".kiln-hero p").
		Set("font-size", "1.125rem", "color", "{colors.kiln-page-fg-soft}", "max-width", "60ch", "margin", "0 auto {spacing.kiln-pg-lg}").
		End()

	// ---- Cards ---------------------------------------------------------
	ss.Rule(".kiln-card").
		Set(
			"background", "{colors.kiln-page-surface}",
			"border", "1px solid {colors.kiln-page-border}",
			"border-radius", "{radii.kiln-pg-lg}",
			"padding", "{spacing.kiln-pg-lg}",
			"box-shadow", "0 1px 2px rgba(15, 23, 42, 0.06)",
		).
		End()
	ss.Rule(".kiln-card-soft").
		Set(
			"background", "{colors.kiln-page-surface-soft}",
			"border", "1px solid {colors.kiln-page-border}",
			"border-radius", "{radii.kiln-pg-lg}",
			"padding", "{spacing.kiln-pg-lg}",
		).
		End()

	// ---- Buttons -------------------------------------------------------
	ss.Rule(".kiln-button").
		Set(
			"display", "inline-flex",
			"align-items", "center",
			"gap", "{spacing.kiln-pg-sm}",
			"background", "{colors.kiln-page-primary}",
			"color", "{colors.kiln-page-primary-fg}",
			"border", "1px solid {colors.kiln-page-primary}",
			"padding", "10px 18px",
			"border-radius", "{radii.kiln-pg-md}",
			"font-weight", "600",
			"cursor", "pointer",
			"text-decoration", "none",
		).
		End()
	ss.Rule(".kiln-button:hover").Set("filter", "brightness(1.08)", "text-decoration", "none").End()
	ss.Rule(".kiln-button-secondary").
		Set("background", "{colors.kiln-page-surface}", "color", "{colors.kiln-page-fg}", "border-color", "{colors.kiln-page-border}").
		End()
	ss.Rule(".kiln-button-ghost").
		Set("background", "transparent", "color", "{colors.kiln-page-fg}", "border-color", "transparent").
		End()

	// ---- Nav / footer --------------------------------------------------
	ss.Rule(".kiln-nav").
		Set(
			"display", "flex",
			"align-items", "center",
			"justify-content", "space-between",
			"gap", "{spacing.kiln-pg-lg}",
			"padding", "{spacing.kiln-pg-md} {spacing.kiln-pg-lg}",
			"border-bottom", "1px solid {colors.kiln-page-border}",
		).
		End()
	ss.Rule(".kiln-nav-links").
		Set("display", "flex", "gap", "{spacing.kiln-pg-lg}", "align-items", "center").
		End()
	ss.Rule(".kiln-nav-links a").Set("color", "{colors.kiln-page-fg-soft}").End()

	ss.Rule(".kiln-footer").
		Set(
			"border-top", "1px solid {colors.kiln-page-border}",
			"background", "{colors.kiln-page-surface-soft}",
			"padding", "{spacing.kiln-pg-xl} {spacing.kiln-pg-lg} {spacing.kiln-pg-lg}",
		).
		End()

	// ---- Pills / badges ------------------------------------------------
	ss.Rule(".kiln-pill").
		Set(
			"display", "inline-flex",
			"align-items", "center",
			"gap", "6px",
			"background", "{colors.kiln-page-surface-soft}",
			"border", "1px solid {colors.kiln-page-border}",
			"border-radius", "999px",
			"padding", "4px 10px",
			"font-size", "0.75rem",
			"font-weight", "600",
			"color", "{colors.kiln-page-fg-soft}",
		).
		End()
	ss.Rule(".kiln-badge-success").Set("color", "{colors.success}", "font-weight", "600").End()
	ss.Rule(".kiln-badge-warning").Set("color", "{colors.warning}", "font-weight", "600").End()
	ss.Rule(".kiln-badge-danger").Set("color", "{colors.danger}", "font-weight", "600").End()

	// ---- Quote ---------------------------------------------------------
	ss.Rule(".kiln-quote").
		Set(
			"font-size", "1.5rem",
			"line-height", "1.5",
			"color", "{colors.kiln-page-fg}",
			"font-weight", "500",
			"max-width", "60ch",
			"margin", "0 auto {spacing.kiln-pg-lg}",
			"text-align", "center",
		).
		End()

	// ---- Native form controls / tables ---------------------------------
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
			"background", "{colors.kiln-page-surface}",
			"border", "1px solid {colors.kiln-page-border}",
			"border-radius", "{radii.kiln-pg-md}",
			"padding", "8px 12px",
			"color", "{colors.kiln-page-fg}",
			"font", "inherit",
		).
		End()
	ss.Rule(`body.kiln-app input:focus, body.kiln-app textarea:focus, body.kiln-app select:focus`).
		Set("outline", "none", "border-color", "{colors.kiln-page-accent}").
		End()
	ss.Rule("body.kiln-app table").Set("width", "100%", "border-collapse", "collapse").End()
	ss.Rule("body.kiln-app th, body.kiln-app td").
		Set(
			"padding", "{spacing.kiln-pg-sm} {spacing.kiln-pg-md}",
			"border-bottom", "1px solid {colors.kiln-page-border}",
			"text-align", "left",
		).
		End()
	ss.Rule("body.kiln-app th").
		Set("font-weight", "600", "color", "{colors.kiln-page-fg-soft}", "background", "{colors.kiln-page-surface-soft}").
		End()

	return theme.CSSCustomProperties() + "\n" + ss.CSS()
}
