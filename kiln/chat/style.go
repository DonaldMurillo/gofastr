package chat

import "github.com/gofastr/gofastr/core-ui/style"

// widgetTheme tokens for the floating chat widget. Built on top of
// core-ui's DefaultTheme.
func widgetTheme() style.Theme {
	base := style.DefaultTheme()
	overrides := style.Theme{
		Colors: style.Colors{
			"kiln-fg":         "#e6e8eb",
			"kiln-fg-muted":   "rgba(230, 232, 235, 0.65)",
			"kiln-glass":      "rgba(14, 15, 18, 0.72)",
			"kiln-glass-edge": "rgba(255, 255, 255, 0.08)",
			"kiln-accent":     "#4f8cff",
			"kiln-user-bg":    "rgba(76, 88, 110, 0.55)",
			"kiln-assist-bg":  "rgba(38, 60, 110, 0.55)",
			"kiln-tool-fg":    "rgba(180, 200, 230, 0.55)",
			"kiln-bad":        "#ff6f6f",
		},
		Spacing: style.Spacing{
			"kiln-pad-sm": 8,
			"kiln-pad":    12,
			"kiln-pad-lg": 18,
		},
		Radii: style.Radii{
			"kiln-sm": 6,
			"kiln-md": 10,
			"kiln-lg": 14,
		},
	}
	return style.MergeThemes(base, overrides)
}

// widgetCSS builds the widget stylesheet using the framework's stylesheet
// builder. No bespoke .css files — everything goes through theme tokens.
//
// Translucent control-panel feel: backdrop-blur over a glassy background,
// soft borders, subtle elevation. Designed not to compete with the host
// page underneath.
func widgetCSS() string {
	ss := style.NewStyleSheet(widgetTheme())

	// --- Root container -------------------------------------------------
	ss.Rule(".kiln-widget").
		Set(
			"position", "fixed",
			"z-index", "2147483600",
			"font-family", "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, sans-serif",
			"font-size", "13px",
			"line-height", "1.45",
			"color", "{colors.kiln-fg}",
		).
		End()

	// --- Corners --------------------------------------------------------
	for _, c := range []struct {
		name, vert, horiz string
	}{
		{"bottom-right", "bottom", "right"},
		{"bottom-left", "bottom", "left"},
		{"top-right", "top", "right"},
		{"top-left", "top", "left"},
	} {
		ss.Rule(".kiln-widget.kiln-corner-" + c.name).
			Set(c.vert, "20px", c.horiz, "20px").
			End()
	}

	// --- Floating action button ----------------------------------------
	ss.Rule(".kiln-fab").
		Set(
			"width", "52px",
			"height", "52px",
			"border-radius", "50%",
			"border", "1px solid {colors.kiln-glass-edge}",
			"background", "{colors.kiln-glass}",
			"backdrop-filter", "blur(20px) saturate(140%)",
			"-webkit-backdrop-filter", "blur(20px) saturate(140%)",
			"color", "{colors.kiln-fg}",
			"font-size", "20px",
			"cursor", "pointer",
			"box-shadow", "0 8px 32px rgba(0,0,0,0.35)",
			"transition", "transform 0.15s ease, opacity 0.15s ease",
		).
		Pseudo(":hover", "transform", "scale(1.05)").
		End()

	ss.Rule(".kiln-fab.kiln-fab-hidden").
		Set(
			"opacity", "0",
			"pointer-events", "none",
			"transform", "scale(0.85)",
		).
		End()

	// --- Panel ---------------------------------------------------------
	ss.Rule(".kiln-panel").
		Set(
			"position", "absolute",
			"bottom", "0",
			"right", "0",
			"width", "min(380px, calc(100vw - 40px))",
			"max-height", "min(580px, calc(100vh - 40px))",
			"display", "none",
			"flex-direction", "column",
			"background", "{colors.kiln-glass}",
			"backdrop-filter", "blur(24px) saturate(140%)",
			"-webkit-backdrop-filter", "blur(24px) saturate(140%)",
			"border", "1px solid {colors.kiln-glass-edge}",
			"border-radius", "{radii.kiln-lg}",
			"box-shadow", "0 16px 48px rgba(0,0,0,0.45)",
			"overflow", "hidden",
		).
		End()

	ss.Rule(".kiln-panel.kiln-open").
		Set("display", "flex").
		End()

	// Anchor panel to the same corner as the widget.
	ss.Rule(".kiln-corner-top-right .kiln-panel, .kiln-corner-top-left .kiln-panel").
		Set("top", "0", "bottom", "auto").
		End()
	ss.Rule(".kiln-corner-top-left .kiln-panel, .kiln-corner-bottom-left .kiln-panel").
		Set("left", "0", "right", "auto").
		End()

	// --- Head ----------------------------------------------------------
	ss.Rule(".kiln-panel-head").
		Set(
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.kiln-pad-sm}",
			"padding", "{spacing.kiln-pad} {spacing.kiln-pad-lg}",
			"border-bottom", "1px solid {colors.kiln-glass-edge}",
		).
		End()

	ss.Rule(".kiln-panel-title").
		Set(
			"font-weight", "600",
			"letter-spacing", "0.4px",
		).
		End()

	ss.Rule(".kiln-panel-page").
		Set(
			"flex", "1",
			"font-family", "ui-monospace, monospace",
			"font-size", "11px",
			"color", "{colors.kiln-fg-muted}",
			"overflow", "hidden",
			"text-overflow", "ellipsis",
			"white-space", "nowrap",
		).
		End()

	ss.Rule(".kiln-panel-close").
		Set(
			"background", "transparent",
			"color", "{colors.kiln-fg-muted}",
			"border", "none",
			"font-size", "20px",
			"line-height", "1",
			"cursor", "pointer",
			"padding", "2px 6px",
		).
		Pseudo(":hover", "color", "{colors.kiln-fg}").
		End()

	// --- Log -----------------------------------------------------------
	ss.Rule(".kiln-log").
		Set(
			"flex", "1",
			"overflow-y", "auto",
			"padding", "{spacing.kiln-pad}",
			"list-style", "none",
			"display", "flex",
			"flex-direction", "column",
			"gap", "{spacing.kiln-pad-sm}",
		).
		End()

	ss.Rule(".kiln-msg").
		Set(
			"padding", "8px 10px",
			"border-radius", "{radii.kiln-md}",
			"white-space", "pre-wrap",
			"word-break", "break-word",
			"max-width", "85%",
		).
		End()

	ss.Rule(".kiln-msg-user").
		Set(
			"background", "{colors.kiln-user-bg}",
			"align-self", "flex-end",
		).
		End()

	ss.Rule(".kiln-msg-assistant").
		Set(
			"background", "{colors.kiln-assist-bg}",
			"align-self", "flex-start",
		).
		End()

	ss.Rule(".kiln-msg-tool").
		Set(
			"background", "transparent",
			"color", "{colors.kiln-tool-fg}",
			"font-family", "ui-monospace, monospace",
			"font-size", "11px",
			"align-self", "flex-start",
		).
		End()

	// --- Status / form ------------------------------------------------
	ss.Rule(".kiln-status").
		Set(
			"padding", "0 {spacing.kiln-pad}",
			"font-size", "11px",
			"color", "{colors.kiln-fg-muted}",
			"min-height", "16px",
		).
		End()

	ss.Rule(".kiln-form").
		Set(
			"display", "flex",
			"gap", "{spacing.kiln-pad-sm}",
			"padding", "{spacing.kiln-pad-sm} {spacing.kiln-pad}",
			"border-top", "1px solid {colors.kiln-glass-edge}",
		).
		End()

	ss.Rule(".kiln-input").
		Set(
			"flex", "1",
			"background", "rgba(255, 255, 255, 0.06)",
			"border", "1px solid {colors.kiln-glass-edge}",
			"color", "{colors.kiln-fg}",
			"padding", "8px 10px",
			"border-radius", "{radii.kiln-sm}",
			"font", "inherit",
			"outline", "none",
			"resize", "none",
			"min-height", "36px",
			"max-height", "160px",
			"line-height", "1.4",
		).
		Pseudo(":focus", "border-color", "{colors.kiln-accent}").
		End()

	ss.Rule(".kiln-send").
		Set(
			"background", "{colors.kiln-accent}",
			"color", "#fff",
			"border", "none",
			"padding", "0 14px",
			"border-radius", "{radii.kiln-sm}",
			"font", "inherit",
			"font-weight", "500",
			"cursor", "pointer",
			"align-self", "stretch",
		).
		Pseudo(":disabled", "opacity", "0.5", "cursor", "not-allowed").
		End()

	// --- empty state ---------------------------------------------------
	ss.Rule(".kiln-empty").
		Set(
			"flex", "1",
			"display", "flex",
			"flex-direction", "column",
			"align-items", "center",
			"justify-content", "center",
			"gap", "{spacing.kiln-pad-sm}",
			"padding", "{spacing.kiln-pad-lg}",
			"text-align", "center",
			"color", "{colors.kiln-fg-muted}",
		).
		Child("p", "margin", "0").
		End()

	ss.Rule(".kiln-empty-mark").
		Set(
			"width", "36px",
			"height", "36px",
			"border-radius", "10px",
			"background", "linear-gradient(135deg, #4f8cff 0%, #6f5cff 100%)",
			"display", "flex",
			"align-items", "center",
			"justify-content", "center",
			"font-size", "16px",
			"color", "white",
			"box-shadow", "0 8px 24px rgba(79, 140, 255, 0.35)",
			"margin-bottom", "{spacing.kiln-pad-sm}",
		).
		End()

	ss.Rule(".kiln-empty-sub").
		Set(
			"font-size", "12px",
			"line-height", "1.5",
			"max-width", "32ch",
			"color", "rgba(154, 160, 166, 0.85)",
		).
		End()

	// --- pending + failed message states -----------------------------
	ss.Rule(".kiln-msg-pending").
		Set("opacity", "0.6").
		End()

	ss.Rule(".kiln-msg-failed").
		Set(
			"opacity", "0.95",
			"box-shadow", "inset 0 0 0 1px rgba(255, 111, 111, 0.55)",
		).
		End()

	ss.Rule(".kiln-msg-tool-error").
		Set(
			"background", "transparent",
			"color", "{colors.kiln-bad}",
			"font-family", "ui-monospace, monospace",
			"font-size", "11px",
			"align-self", "flex-start",
		).
		End()

	// --- status feedback ---------------------------------------------
	ss.Rule(".kiln-status-ok").Set("color", "rgba(110, 220, 150, 0.85)").End()
	ss.Rule(".kiln-status-warn").Set("color", "rgba(255, 200, 110, 0.85)").End()
	ss.Rule(".kiln-status-error").Set("color", "{colors.kiln-bad}").End()

	// --- thinking indicator (typing dots) ----------------------------
	ss.Rule(".kiln-thinking").
		Set(
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.kiln-pad-sm}",
			"padding", "{spacing.kiln-pad-sm} {spacing.kiln-pad}",
			"color", "{colors.kiln-fg-muted}",
			"font-size", "12px",
		).
		End()

	ss.Rule(".kiln-thinking-dots").
		Set("display", "inline-flex", "gap", "4px").
		End()

	ss.Rule(".kiln-dot").
		Set(
			"width", "6px",
			"height", "6px",
			"border-radius", "50%",
			"background", "{colors.kiln-accent}",
			"display", "inline-block",
			"animation", "kilnPulse 1.2s ease-in-out infinite",
		).
		End()

	ss.Rule(".kiln-dot:nth-child(2)").Set("animation-delay", "0.15s").End()
	ss.Rule(".kiln-dot:nth-child(3)").Set("animation-delay", "0.3s").End()

	ss.Keyframes("kilnPulse",
		style.Step("0%, 80%, 100%", "opacity", "0.25", "transform", "scale(0.85)"),
		style.Step("40%", "opacity", "1", "transform", "scale(1)"),
	)

	// --- top-of-page build banner -----------------------------------
	// Appears across the very top of every Kiln-served page when the
	// world is being mutated. Independent of whether the chat panel
	// is open. Slides down on activation, stays for ~1.5s, slides up.
	ss.Rule(".kiln-build-banner").
		Set(
			"position", "fixed",
			"top", "0",
			"left", "0",
			"right", "0",
			"z-index", "2147483647",
			"display", "flex",
			"align-items", "center",
			"justify-content", "center",
			"gap", "{spacing.kiln-pad-sm}",
			"padding", "8px 14px",
			"background", "linear-gradient(90deg, rgba(79, 140, 255, 0.92), rgba(111, 92, 255, 0.92))",
			"backdrop-filter", "blur(16px) saturate(140%)",
			"-webkit-backdrop-filter", "blur(16px) saturate(140%)",
			"color", "#fff",
			"font-family", "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, sans-serif",
			"font-size", "12px",
			"font-weight", "500",
			"letter-spacing", "0.4px",
			"transform", "translateY(-100%)",
			"transition", "transform 0.25s ease, opacity 0.25s ease",
			"box-shadow", "0 6px 20px rgba(79, 140, 255, 0.4)",
			"opacity", "0",
		).
		End()

	ss.Rule(".kiln-build-banner.kiln-build-banner-on").
		Set("transform", "translateY(0)", "opacity", "1").
		End()

	ss.Rule(".kiln-build-spinner").
		Set(
			"width", "12px",
			"height", "12px",
			"border-radius", "50%",
			"border", "2px solid rgba(255, 255, 255, 0.35)",
			"border-top-color", "#fff",
			"animation", "kilnSpin 0.8s linear infinite",
			"display", "inline-block",
		).
		End()

	ss.Keyframes("kilnSpin",
		style.Step("0%", "transform", "rotate(0deg)"),
		style.Step("100%", "transform", "rotate(360deg)"),
	)

	// --- slide-in for system rows -----------------------------------
	ss.Rule(".kiln-msg.kiln-msg-tool").
		Set("animation", "kilnSlideIn 0.25s ease both").
		End()

	ss.Keyframes("kilnSlideIn",
		style.Step("0%", "opacity", "0", "transform", "translateY(-6px)"),
		style.Step("100%", "opacity", "1", "transform", "translateY(0)"),
	)

	return ss.CSS()
}

// baseCSS is loaded by every Kiln-rendered page so agent-built UIs are
// readable during build mode. The freeze step does NOT include this —
// users' real apps bring their own stylesheets.
func baseCSS() string {
	ss := style.NewStyleSheet(widgetTheme())

	ss.Rule("*, *::before, *::after").
		Set("box-sizing", "border-box").
		End()

	ss.Rule("html, body").
		Set("margin", "0", "padding", "0", "min-height", "100%").
		End()

	ss.Rule("body").
		Set(
			"background", "radial-gradient(ellipse at top, #1d2230 0%, #0e0f12 60%)",
			"background-attachment", "fixed",
			"color", "{colors.kiln-fg}",
			"font", "14px/1.55 -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, sans-serif",
		).
		End()

	ss.Rule(".kiln-page").
		Set(
			"max-width", "920px",
			"margin", "0 auto",
			"padding", "48px 24px 120px",
		).
		End()

	ss.Rule(".kiln-page h1").
		Set("font-size", "28px", "font-weight", "600", "letter-spacing", "0.2px", "margin", "0 0 16px").
		End()
	ss.Rule(".kiln-page h2").
		Set("font-size", "20px", "font-weight", "600", "margin", "32px 0 12px").
		End()
	ss.Rule(".kiln-page h3").
		Set("font-size", "16px", "font-weight", "600", "margin", "24px 0 8px").
		End()
	ss.Rule(".kiln-page p").
		Set("margin", "0 0 14px", "color", "rgba(230, 232, 235, 0.85)").
		End()
	ss.Rule(".kiln-page a").
		Set("color", "{colors.kiln-accent}", "text-decoration", "none").
		Pseudo(":hover", "text-decoration", "underline").
		End()
	ss.Rule(".kiln-page ul, .kiln-page ol").
		Set("margin", "0 0 14px", "padding-left", "22px").
		End()
	ss.Rule(".kiln-page li").
		Set("margin", "4px 0").
		End()
	ss.Rule(".kiln-page code").
		Set(
			"background", "rgba(255, 255, 255, 0.06)",
			"padding", "2px 6px",
			"border-radius", "4px",
			"font", "12px ui-monospace, monospace",
		).
		End()
	ss.Rule(".kiln-page pre").
		Set(
			"background", "rgba(0, 0, 0, 0.4)",
			"padding", "12px 14px",
			"border-radius", "8px",
			"overflow-x", "auto",
			"font", "12px/1.5 ui-monospace, monospace",
		).
		End()
	ss.Rule(".kiln-page table").
		Set("width", "100%", "border-collapse", "collapse", "margin", "12px 0 24px").
		End()
	ss.Rule(".kiln-page th, .kiln-page td").
		Set("padding", "8px 10px", "border-bottom", "1px solid rgba(255, 255, 255, 0.08)", "text-align", "left").
		End()
	ss.Rule(".kiln-page th").
		Set("color", "rgba(255, 255, 255, 0.6)", "font-weight", "500", "font-size", "12px", "text-transform", "uppercase", "letter-spacing", "0.4px").
		End()
	ss.Rule(".kiln-page button").
		Set(
			"background", "{colors.kiln-accent}",
			"color", "#fff",
			"border", "none",
			"padding", "8px 14px",
			"border-radius", "6px",
			"font", "inherit",
			"cursor", "pointer",
		).
		Pseudo(":hover", "filter", "brightness(1.05)").
		End()
	ss.Rule(".kiln-page input, .kiln-page textarea, .kiln-page select").
		Set(
			"background", "rgba(255, 255, 255, 0.06)",
			"border", "1px solid rgba(255, 255, 255, 0.12)",
			"color", "{colors.kiln-fg}",
			"padding", "8px 10px",
			"border-radius", "6px",
			"font", "inherit",
			"outline", "none",
			"width", "100%",
			"margin", "4px 0 14px",
		).
		Pseudo(":focus", "border-color", "{colors.kiln-accent}").
		End()
	ss.Rule(".kiln-page label").
		Set("display", "block", "margin", "12px 0 4px", "color", "rgba(230, 232, 235, 0.85)", "font-size", "13px").
		End()
	ss.Rule(".kiln-page form").
		Set("max-width", "520px", "margin", "16px 0").
		End()
	ss.Rule(".kiln-page hr").
		Set("border", "none", "border-top", "1px solid rgba(255, 255, 255, 0.08)", "margin", "32px 0").
		End()

	// Inline navs / link clusters: agent-built pages typically string
	// adjacent <a> elements together as nav items. Without spacing they
	// concatenate visually. Give any anchor an inline-block and a small
	// margin when followed by another anchor in the same parent.
	ss.Rule(".kiln-page nav").
		Set("display", "flex", "align-items", "center", "gap", "16px", "flex-wrap", "wrap", "margin-bottom", "24px").
		End()
	ss.Rule(".kiln-page nav a").
		Set("display", "inline-block").
		End()
	ss.Rule(".kiln-page nav a + a").
		Set("margin-left", "0").
		End()
	// Sections usually want vertical space between them.
	ss.Rule(".kiln-page section").
		Set("margin", "32px 0", "padding", "16px 0").
		End()

	return ss.CSS()
}
