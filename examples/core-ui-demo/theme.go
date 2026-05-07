package main

import (
	"github.com/gofastr/gofastr/core-ui/style"
)

func createTheme() style.Theme {
	base := style.DefaultTheme()
	custom := style.Theme{
		Colors: style.Colors{
			"primary":   "#6366F1", // indigo
			"secondary": "#8B5CF6", // violet
		},
	}
	return style.MergeThemes(base, custom)
}

// createStyleSheet builds the complete demo stylesheet using Go + theme tokens.
// This is the dog-food: all visual styles defined in Go, not a .css file.
func createStyleSheet(theme style.Theme) string {
	ss := style.NewStyleSheet(theme)

	// Reset
	ss.Rule("*, *::before, *::after").
		Set("box-sizing", "border-box", "margin", "0", "padding", "0").
		End()

	// Body
	ss.Rule("body").
		Set(
			"font-family", "{fonts.body}",
			"color", "{colors.text}",
			"background", "{colors.background}",
			"line-height", "1.6",
			"min-height", "100vh",
			"display", "flex",
			"flex-direction", "column",
		).
		End()

	// Skip link
	ss.Rule(".skip-link").
		Set(
			"position", "absolute",
			"top", "-100%",
			"left", "0",
			"background", "{colors.primary}",
			"color", "white",
			"padding", "{spacing.sm} {spacing.md}",
			"z-index", "100",
			"text-decoration", "none",
			"border-radius", "{radii.md}",
			"font-weight", "600",
		).
		Pseudo(":focus",
			"top", "{spacing.md}",
		).
		End()

	// Layout
	ss.Rule(".layout-main").
		Set("display", "flex", "flex-direction", "column", "min-height", "100vh").
		End()

	// Header
	ss.Rule(".layout-main > [role=\"banner\"]").
		Set(
			"background", "white",
			"border-bottom", "1px solid {colors.border}",
			"padding", "{spacing.md} {spacing.lg}",
		).
		Child("> [role=\"banner\"]",
			"border", "none",
			"padding", "0",
		).
		Child("nav",
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.lg}",
		).
		End()

	ss.Rule(".layout-main > [role=\"banner\"] nav > div").
		Set("display", "flex", "gap", "{spacing.md}").
		End()

	ss.Rule(".layout-main > [role=\"banner\"] nav > div a").
		Set(
			"color", "{colors.text}",
			"text-decoration", "none",
			"font-weight", "500",
			"padding", "{spacing.xs} {spacing.sm}",
			"border-radius", "{radii.sm}",
			"transition", "background 0.15s",
		).
		Pseudo(":hover, :focus",
			"background", "{colors.background}",
			"color", "{colors.primary}",
		).
		End()

	ss.Rule(".layout-main > [role=\"banner\"] nav > div a[aria-label=\"Home\"]").
		Set("font-weight", "700", "font-size", "1.125rem", "color", "{colors.primary}").
		End()

	// Layout body
	ss.Rule(".layout-body").
		Set("display", "flex", "flex", "1").
		End()

	// Main content
	ss.Rule("[role=\"main\"]").
		Set(
			"flex", "1",
			"padding", "{spacing.xl}",
			"max-width", "1200px",
			"margin", "0 auto",
			"width", "100%",
		).
		Child("> [role=\"main\"]",
			"padding", "0",
			"max-width", "none",
		).
		End()

	// Footer
	ss.Rule(".layout-main > [role=\"contentinfo\"]").
		Set(
			"background", "white",
			"border-top", "1px solid {colors.border}",
			"padding", "{spacing.md} {spacing.lg}",
			"text-align", "center",
			"color", "{colors.text-muted}",
			"font-size", "0.875rem",
		).
		Child("> [role=\"contentinfo\"]",
			"border", "none",
			"padding", "0",
		).
		End()

	// Hero section
	ss.Rule("[aria-label=\"Hero\"]").
		Set(
			"text-align", "center",
			"padding", "{spacing.3xl} {spacing.xl}",
			"background", "linear-gradient(135deg, {colors.primary}, {colors.secondary})",
			"border-radius", "{radii.lg}",
			"color", "white",
			"margin-bottom", "{spacing.xl}",
		).
		Child("h1",
			"font-size", "2.5rem",
			"font-weight", "800",
			"margin-bottom", "{spacing.md}",
			"font-family", "{fonts.heading}",
		).
		Child("p",
			"font-size", "1.25rem",
			"margin-bottom", "{spacing.lg}",
			"opacity", "0.9",
		).
		End()

	// CTA button
	ss.Rule(".cta-button").
		Set(
			"display", "inline-block",
			"background", "white",
			"color", "{colors.primary}",
			"padding", "{spacing.md} {spacing.xl}",
			"border-radius", "{radii.lg}",
			"text-decoration", "none",
			"font-weight", "700",
			"font-size", "1.125rem",
			"transition", "transform 0.15s, box-shadow 0.15s",
		).
		Pseudo(":hover",
			"transform", "translateY(-2px)",
			"box-shadow", "0 4px 12px rgba(0,0,0,0.15)",
		).
		End()

	// Product grid
	ss.Rule(".product-grid").
		Set(
			"display", "grid",
			"grid-template-columns", "repeat(auto-fill, minmax(280px, 1fr))",
			"gap", "{spacing.lg}",
			"margin-top", "{spacing.lg}",
		).
		End()

	// Product card
	ss.Rule(".product-card").
		Set(
			"background", "white",
			"border-radius", "{radii.lg}",
			"border", "1px solid {colors.border}",
			"overflow", "hidden",
			"transition", "box-shadow 0.15s, transform 0.15s",
		).
		Pseudo(":hover",
			"box-shadow", "0 4px 20px rgba(0,0,0,0.08)",
			"transform", "translateY(-2px)",
		).
		Child("img",
			"width", "100%",
			"height", "200px",
			"object-fit", "cover",
			"background", "{colors.background}",
		).
		Child("h3",
			"padding", "{spacing.md} {spacing.md} 0",
			"font-size", "1.125rem",
		).
		Child("p",
			"padding", "{spacing.xs} {spacing.md}",
			"color", "{colors.primary}",
			"font-weight", "700",
			"font-size", "1.25rem",
		).
		Child("button",
			"width", "calc(100% - {spacing.lg})",
			"margin", "{spacing.md}",
			"padding", "{spacing.md}",
			"background", "{colors.primary}",
			"color", "white",
			"border", "none",
			"border-radius", "{radii.md}",
			"font-weight", "600",
			"cursor", "pointer",
			"transition", "background 0.15s",
		).
		End()

	ss.Rule(".product-card button:hover").
		Set("background", "#4F46E5").
		End()

	// Search form
	ss.Rule("form").
		Set("display", "flex", "gap", "{spacing.md}", "margin-bottom", "{spacing.lg}").
		End()

	ss.Rule("form label").
		Set("font-weight", "600", "display", "flex", "align-items", "center", "margin-right", "{spacing.sm}", "white-space", "nowrap").
		End()

	ss.Rule("form input[type=\"search\"]").
		Set(
			"flex", "1",
			"padding", "{spacing.md} {spacing.lg}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"font-size", "1rem",
			"font-family", "{fonts.body}",
		).
		Pseudo(":focus",
			"outline", "2px solid {colors.primary}",
			"outline-offset", "-2px",
		).
		End()

	ss.Rule("form button[type=\"submit\"]").
		Set(
			"padding", "{spacing.md} {spacing.lg}",
			"background", "{colors.primary}",
			"color", "white",
			"border", "none",
			"border-radius", "{radii.md}",
			"font-weight", "600",
			"cursor", "pointer",
		).
		End()

	// About page sections
	ss.Rule("[aria-label=\"Our mission\"], [aria-label=\"Our team\"], [aria-label=\"Contact\"]").
		Set("margin-bottom", "{spacing.xl}").
		End()

	// Headings
	ss.Rule("h1").
		Set("font-size", "2rem", "font-weight", "800", "margin-bottom", "{spacing.lg}", "color", "{colors.text}").
		End()

	ss.Rule("h2").
		Set("font-size", "1.5rem", "font-weight", "700", "margin-bottom", "{spacing.md}", "color", "{colors.text}").
		End()

	// Paragraph & list
	ss.Rule("p").
		Set("margin-bottom", "{spacing.md}").
		End()

	ss.Rule("ul").
		Set("list-style", "none", "padding", "0").
		End()

	ss.Rule("ul li").
		Set("padding", "{spacing.sm} 0", "padding-left", "{spacing.lg}", "position", "relative").
		Pseudo("::before",
			"content", `"→"`,
			"position", "absolute",
			"left", "0",
			"color", "{colors.primary}",
		).
		End()

	// Drawer
	ss.Rule(".drawer").
		Set(
			"background", "white",
			"border-radius", "{radii.lg}",
			"padding", "{spacing.xl}",
			"box-shadow", "0 8px 32px rgba(0,0,0,0.12)",
			"max-width", "400px",
		).
		Child("h2",
			"margin-bottom", "{spacing.md}",
		).
		End()

	// Cart badge
	ss.Rule(".cart-badge").
		Set(
			"display", "inline-block",
			"background", "{colors.primary}",
			"color", "white",
			"padding", "{spacing.xs} {spacing.md}",
			"border-radius", "{radii.full}",
			"font-weight", "700",
			"font-size", "0.875rem",
		).
		End()

	// Close cart button
	ss.Rule(".close-cart").
		Set(
			"margin-top", "{spacing.lg}",
			"padding", "{spacing.md} {spacing.lg}",
			"background", "{colors.border}",
			"border", "none",
			"border-radius", "{radii.md}",
			"font-weight", "600",
			"cursor", "pointer",
			"width", "100%",
		).
		End()

	// Buttons
	ss.Rule("button").
		Set("font-family", "{fonts.body}").
		End()

	// Counter
	ss.Rule(".counter-display").
		Set(
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.md}",
			"justify-content", "center",
			"margin", "{spacing.lg} 0",
		).
		End()

	ss.Rule(".counter-value").
		Set(
			"font-size", "3rem",
			"font-weight", "800",
			"min-width", "80px",
			"text-align", "center",
			"color", "{colors.primary}",
		).
		End()

	ss.Rule(".counter-btn").
		Set(
			"width", "48px",
			"height", "48px",
			"border-radius", "{radii.full}",
			"border", "2px solid {colors.primary}",
			"background", "white",
			"color", "{colors.primary}",
			"font-size", "1.5rem",
			"font-weight", "700",
			"cursor", "pointer",
			"display", "flex",
			"align-items", "center",
			"justify-content", "center",
			"transition", "all 0.15s",
		).
		Pseudo(":hover",
			"background", "{colors.primary}",
			"color", "white",
		).
		End()

	// Toast
	ss.Rule(".toast").
		Set(
			"position", "fixed",
			"bottom", "{spacing.xl}",
			"right", "{spacing.xl}",
			"background", "{colors.success}",
			"color", "white",
			"padding", "{spacing.md} {spacing.lg}",
			"border-radius", "{radii.md}",
			"font-weight", "600",
			"box-shadow", "0 4px 12px rgba(0,0,0,0.15)",
			"z-index", "1000",
			"transition", "opacity 0.3s",
		).
		End()

	ss.Rule(".toast-fade").
		Set("opacity", "0").
		End()

	// Island update flash
	ss.Rule(".island-updated").
		Set("animation", "island-flash 1s ease-out").
		End()

	// Keyframes
	ss.Keyframes("island-flash",
		style.Step("0%", "background", "rgba(99, 102, 241, 0.15)"),
		style.Step("100%", "background", "transparent"),
	)

	// Live feed
	ss.Rule(".live-feed").
		Set(
			"background", "white",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}",
			"padding", "{spacing.lg}",
			"margin-top", "{spacing.xl}",
		).
		Child("h3",
			"font-size", "1rem",
			"text-transform", "uppercase",
			"letter-spacing", "0.05em",
			"color", "{colors.text-muted}",
			"margin-bottom", "{spacing.md}",
		).
		End()

	ss.Rule(".feed-list").
		Set("list-style", "none", "padding", "0").
		End()

	ss.Rule(".feed-list li").
		Set("padding", "{spacing.sm} 0", "border-bottom", "1px solid {colors.border}", "padding-left", "0").
		Pseudo("::before", "content", "none").
		End()

	// Product detail page
	ss.Rule(".product-detail").
		Set("max-width", "900px", "margin", "0 auto", "padding", "{spacing.xl}").
		End()

	ss.Rule(".back-link").
		Set(
			"display", "inline-block",
			"margin-bottom", "{spacing.lg}",
			"color", "{colors.primary}",
			"text-decoration", "none",
			"font-size", "0.95rem",
		).
		Pseudo(":hover",
			"text-decoration", "underline",
		).
		End()

	ss.Rule(".product-detail-content").
		Set(
			"display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.2xl}",
			"align-items", "start",
		).
		Media("max-width: 640px", func(ss *style.StyleSheet) {
			ss.Rule(".product-detail-content").
				Set("grid-template-columns", "1fr").
				End()
		}).
		End()

	ss.Rule(".product-detail-image").
		Set(
			"width", "100%",
			"border-radius", "{radii.md}",
			"aspect-ratio", "1",
			"object-fit", "contain",
			"background", "{colors.background}",
			"padding", "{spacing.lg}",
		).
		End()

	ss.Rule(".product-detail-info h1").
		Set("margin-bottom", "{spacing.sm}").
		End()

	ss.Rule(".product-detail-price").
		Set(
			"font-size", "1.5rem",
			"font-weight", "700",
			"color", "{colors.primary}",
			"margin-bottom", "{spacing.lg}",
		).
		End()

	// Product card link (wraps card content for detail nav)
	ss.Rule(".product-card-link").
		Set("text-decoration", "none", "color", "inherit", "display", "block").
		Pseudo(":hover", "opacity", "0.9").
		End()

	return ss.CSS()
}
