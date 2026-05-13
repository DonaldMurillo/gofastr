package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func createTheme() style.Theme {
	t := style.DefaultTheme()
	t.Colors.Primary = style.Color{Name: "primary", Value: "#6366F1"}     // indigo
	t.Colors.Secondary = style.Color{Name: "secondary", Value: "#8B5CF6"} // violet
	return t
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
		Transition("opacity 0.2s ease").
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

	// Container queries: product cards reflow based on parent width, not viewport.
	// When a product-grid is wide enough, cards go horizontal (image left, text right).
	ss.Rule(".product-grid").
		Container("product-grid", "(min-width: 600px)", func(ss *style.StyleSheet) {
			ss.Rule(".product-card").
				Set("display", "flex", "flex-direction", "row").
				End()
			ss.Rule(".product-card img").
				Set("width", "50%", "height", "auto", "min-height", "200px").
				End()
			ss.Rule(".product-card h3").
				Set("padding", "{spacing.md} {spacing.md} 0").
				End()
			ss.Rule(".product-card p").
				Set("padding", "{spacing.xs} {spacing.md}").
				End()
			ss.Rule(".product-card button").
				Set("width", "auto", "margin", "{spacing.md}").
				End()
		}).
		Container("product-grid", "(min-width: 900px)", func(ss *style.StyleSheet) {
			ss.Rule(".product-card img").
				Set("width", "40%").
				End()
		}).
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

	// Page transition: entering state
	ss.Rule("[role=\"main\"].page-entering").
		Set("opacity", "0").
		End()

	// -----------------------------------------------------------------------
	// Overlay backdrop — shared by all overlay types (dialog, sheet, drawer)
	// -----------------------------------------------------------------------
	// The backdrop is a full-screen fixed element that dims the page.
	// The content (.dialog / .sheet / .drawer) is a child inside it.
	// Click on backdrop → close. Click on content → stays open.
	ss.Rule(".overlay-backdrop").
		Set("position", "fixed", "top", "0", "left", "0", "right", "0", "bottom", "0",
			"background", "rgba(0, 0, 0, 0.5)", "z-index", "1000",
			"display", "flex", "align-items", "center", "justify-content", "center").
		Transition("opacity 0.3s ease").
		End()

	ss.Rule(".backdrop-closing").
		Set("opacity", "0").
		End()

	// -----------------------------------------------------------------------
	// Dialog — centered modal card
	// -----------------------------------------------------------------------
	ss.Rule(".dialog-overlay").
		Set("align-items", "center", "justify-content", "center").
		End()

	ss.Rule(".dialog").
		Set("background", "white", "border-radius", "{radii.lg}", "padding", "{spacing.xl}",
			"max-width", "480px", "width", "90%", "max-height", "90vh", "overflow-y", "auto",
			"box-shadow", "0 20px 60px rgba(0,0,0,0.2)", "position", "relative").
		Transition("transform 0.2s ease, opacity 0.2s ease").
		Pseudo(".dialog-opening", "transform", "scale(0.95)", "opacity", "0").
		Pseudo(".dialog-closing", "transform", "scale(0.95)", "opacity", "0").
		End()

	// -----------------------------------------------------------------------
	// Sheet — slides up from bottom
	// -----------------------------------------------------------------------
	ss.Rule(".sheet-backdrop").
		Set("align-items", "flex-end", "justify-content", "stretch").
		End()

	ss.Rule(".sheet").
		Set("background", "white", "border-radius", "{radii.lg} {radii.lg} 0 0",
			"padding", "{spacing.xl}", "max-height", "70vh", "overflow-y", "auto",
			"box-shadow", "0 -4px 20px rgba(0,0,0,0.1)",
			"width", "100%", "position", "relative",
			"padding-bottom", "60px").
		Transition("transform 0.3s ease").
		Pseudo(".sheet-opening", "transform", "translateY(100%)").
		Pseudo(".sheet-closing", "transform", "translateY(100%)").
		End()

	ss.Rule(".sheet-handle").
		Set("width", "40px", "height", "4px", "background", "{colors.border}",
			"border-radius", "2px", "margin", "0 auto {spacing.md}").
		End()

	// -----------------------------------------------------------------------
	// Drawer — slides in from left
	// -----------------------------------------------------------------------
	ss.Rule(".drawer-backdrop").
		Set("align-items", "stretch", "justify-content", "flex-start").
		End()

	ss.Rule(".drawer").
		Set("width", "320px", "max-width", "85vw", "height", "100%",
			"background", "white",
			"padding", "{spacing.xl}", "overflow-y", "auto",
			"box-shadow", "4px 0 20px rgba(0,0,0,0.1)", "position", "relative").
		Transition("transform 0.3s ease").
		Pseudo(".drawer-opening", "transform", "translateX(-100%)").
		Pseudo(".drawer-closing", "transform", "translateX(-100%)").
		End()

	// -----------------------------------------------------------------------
	// Shared overlay controls
	// -----------------------------------------------------------------------
	ss.Rule(".overlay-close").
		Set("position", "absolute", "top", "{spacing.md}", "right", "{spacing.md}",
			"background", "none", "border", "none", "font-size", "1.5rem",
			"cursor", "pointer", "color", "{colors.text-muted}", "line-height", "1").
		Pseudo(":hover", "color", "{colors.text}").
		End()

	ss.Rule(".sheet-close-btn").
		Set("margin-top", "{spacing.lg}", "padding", "{spacing.md}",
			"background", "{colors.border}", "border", "none",
			"border-radius", "{radii.md}", "font-weight", "600",
			"cursor", "pointer", "width", "100%", "font-size", "1rem",
			"color", "{colors.text}", "position", "sticky", "bottom", "0",
			"z-index", "2", "display", "block").
		Pseudo(":hover", "background", "{colors.text-muted}").
		End()

	ss.Rule(".drawer-close-btn").
		Set("margin-top", "{spacing.lg}", "padding", "{spacing.md}",
			"background", "{colors.border}", "border", "none",
			"border-radius", "{radii.md}", "font-weight", "600",
			"cursor", "pointer", "width", "100%", "font-size", "1rem",
			"color", "{colors.text}").
		Pseudo(":hover", "background", "{colors.text-muted}").
		End()

	// Overlay demo buttons (home page)
	ss.Rule(".overlay-demo-buttons").
		Set("display", "flex", "gap", "{spacing.md}", "flex-wrap", "wrap", "margin-top", "{spacing.md}").
		End()

	// Drawer content
	ss.Rule(".drawer-content").
		Set("padding", "{spacing.sm}").
		End()
	ss.Rule(".drawer-nav-list").
		Set("list-style", "none", "padding", "0", "margin", "{spacing.md} 0").
		End()
	ss.Rule(".drawer-nav-list li").
		Set("padding", "{spacing.sm}", "0", "border-bottom", "1px solid {colors.border}").
		End()
	ss.Rule(".drawer-nav-list a").
		Set("color", "{colors.primary}", "text-decoration", "none", "font-weight", "500").
		Pseudo(":hover", "text-decoration", "underline").
		End()

	// Sheet content
	ss.Rule(".sheet-content").
		Set("padding", "{spacing.sm}").
		End()
	ss.Rule(".sheet-product-preview").
		Set("padding", "{spacing.md}", "background", "{colors.surface}",
			"border-radius", "{radii.md}", "margin-top", "{spacing.md}").
		End()

	// Dialog actions
	ss.Rule(".dialog-actions").
		Set("display", "flex", "gap", "{spacing.md}", "justify-content", "flex-end",
			"margin-top", "{spacing.lg}").
		End()

	// Cart items list
	ss.Rule(".cart-items").
		Set("list-style", "none", "padding", "0", "margin", "{spacing.md} 0").
		End()
	ss.Rule(".cart-items li").
		Set("padding", "{spacing.md}", "border-bottom", "1px solid {colors.border}").
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

	// Error boundary demo
	ss.Rule(".error-boundary-result").
		Set("margin", "{spacing.lg} 0").
		End()

	// DI Showcase
	ss.Rule(".di-showcase").
		Set("max-width", "700px", "margin", "0 auto", "padding", "{spacing.xl}").
		End()
	ss.Rule(".di-card-grid").
		Set("display", "grid", "grid-template-columns", "repeat(auto-fit, minmax(200px, 1fr))", "gap", "{spacing.md}", "margin", "{spacing.lg} 0").
		End()
	ss.Rule(".di-card").
		Set("background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}", "padding", "{spacing.lg}",
			"text-align", "center").
		End()
	ss.Rule(".di-card-icon").
		Set("font-size", "2rem", "margin-bottom", "{spacing.sm}").
		End()
	ss.Rule(".di-card-label").
		Set("font-size", "0.875rem", "color", "{colors.muted}", "font-weight", "600", "text-transform", "uppercase",
			"letter-spacing", "0.05em", "margin-bottom", "{spacing.xs}").
		End()
	ss.Rule(".di-card-value").
		Set("font-size", "1.125rem", "font-weight", "600", "color", "{colors.text}").
		End()
	ss.Rule(".di-code-block").
		Set("background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}", "padding", "{spacing.lg}",
			"overflow-x", "auto", "margin-top", "{spacing.lg}").
		End()
	ss.Rule(".di-code-block pre").
		Set("margin", "0", "font-size", "0.8rem", "line-height", "1.6",
			"white-space", "pre-wrap", "font-family", "monospace").
		End()
	ss.Rule(".di-actions").
		Set("margin", "{spacing.lg} 0", "text-align", "center").
		End()
	ss.Rule(".di-bump-btn").
		Set("padding", "12px 24px", "font-size", "1rem").
		End()
	ss.Rule(".di-details").
		Set("margin-top", "{spacing.lg}").
		End()
	ss.Rule(".di-details summary").
		Set("cursor", "pointer", "font-weight", "600", "color", "{colors.primary}",
			"padding", "{spacing.sm} 0").
		End()
	ss.Rule(".di-card-hint").
		Set("font-size", "0.75rem", "color", "{colors.muted}", "margin-top", "{spacing.xs}").
		End()
	ss.Rule(".di-proof").
		Set("margin-top", "{spacing.lg}", "padding", "{spacing.md}",
			"background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}", "font-size", "0.9rem",
			"color", "{colors.text}", "line-height", "1.5").
		End()

	// Todos
	ss.Rule(".todos-screen").
		Set("max-width", "640px", "margin", "0 auto", "padding", "{spacing.xl}").
		End()
	ss.Rule(".todo-form").
		Set("display", "flex", "gap", "{spacing.sm}", "margin", "{spacing.lg} 0").
		End()
	ss.Rule(".todo-input").
		Set("flex", "1", "padding", "{spacing.sm} {spacing.md}",
			"border", "1px solid {colors.border}", "border-radius", "{radii.md}",
			"background", "{colors.surface}", "color", "{colors.text}",
			"font-size", "1rem").
		End()
	ss.Rule(".todo-input:focus").
		Set("outline", "2px solid {colors.primary}", "outline-offset", "2px",
			"border-color", "{colors.primary}").
		End()
	ss.Rule(".todo-stats").
		Set("display", "flex", "justify-content", "space-between", "align-items", "center",
			"margin", "{spacing.md} 0", "color", "{colors.muted}", "font-size", "0.9rem").
		End()
	ss.Rule(".todo-clear").
		Set("background", "transparent", "border", "1px solid {colors.border}",
			"color", "{colors.muted}", "padding", "{spacing.xs} {spacing.sm}",
			"border-radius", "{radii.sm}", "cursor", "pointer", "font-size", "0.85rem").
		End()
	ss.Rule(".todo-clear:hover").
		Set("color", "{colors.text}", "border-color", "{colors.primary}").
		End()
	ss.Rule(".todo-list").
		Set("list-style", "none", "padding", "0", "margin", "0",
			"display", "flex", "flex-direction", "column", "gap", "{spacing.xs}").
		End()
	ss.Rule(".todo-item").
		Set("display", "flex", "align-items", "center", "gap", "{spacing.sm}",
			"padding", "{spacing.sm} {spacing.md}",
			"background", "{colors.surface}", "border", "1px solid {colors.border}",
			"border-radius", "{radii.md}").
		End()
	ss.Rule(".todo-item.todo-done .todo-check span").
		Set("text-decoration", "line-through", "color", "{colors.muted}").
		End()
	ss.Rule(".todo-check").
		Set("flex", "1", "display", "flex", "align-items", "center", "gap", "{spacing.sm}",
			"cursor", "pointer", "color", "{colors.text}").
		End()
	ss.Rule(".todo-check input").
		Set("width", "18px", "height", "18px", "accent-color", "{colors.primary}").
		End()
	ss.Rule(".todo-delete").
		Set("background", "transparent", "border", "0", "color", "{colors.muted}",
			"font-size", "1.4rem", "line-height", "1", "cursor", "pointer",
			"padding", "0 {spacing.sm}", "border-radius", "{radii.sm}").
		End()
	ss.Rule(".todo-delete:hover").
		Set("color", "{colors.danger}", "background", "{colors.surface}").
		End()
	ss.Rule(".todo-empty").
		Set("padding", "{spacing.lg}", "text-align", "center",
			"color", "{colors.muted}", "font-style", "italic").
		End()

	return ss.CSS()
}
