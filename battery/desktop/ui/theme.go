package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Theme returns the desktop theme: the canonical adaptive framework
// theme with macOS token values. Apply it the way any host applies a
// theme, before mounting the UI host:
//
//	site := app.NewApp("Focus")
//	site.WithTheme(desktopui.Theme())
//
// The token VALUES are per platform (macOS today; Windows and Linux
// swap values later, never fields). Everything the plan sources is
// cited in the comments; everything else is marked measured,
// unverified, and the proof plan validates it against native captures.
func Theme() style.Theme {
	t := uitheme.Default()
	t.Name = "desktop"

	// Plan, "SF Pro facts the theme needs": -apple-system and
	// system-ui both resolve to SF Pro in WebKit; the stack carries
	// the generic fallbacks for a plain browser (--serve mode).
	t.Fonts.Body = style.Font{Name: "body", Value: "-apple-system, system-ui, ui-sans-serif, sans-serif"}
	t.Fonts.Heading = style.Font{Name: "heading", Value: "-apple-system, system-ui, ui-sans-serif, sans-serif"}
	t.Fonts.Mono = style.Font{Name: "mono", Value: "ui-monospace, 'SF Mono', Menlo, monospace"}

	// Plan, HIG Typography table (macOS built-in text styles). One CSS
	// px in a WKWebView equals one point, so the point sizes are the px
	// values. The canonical scale has seven slots and the table ten
	// rows; the mapping below keeps every distinct size the table gives
	// except Callout 12 (nearest slots: SM 11, Base 13). Headline is
	// Body 13 bold; weight is a component concern, not a size token.
	t.Typography = style.FontSizeSet{
		XS:   style.FontSize{Name: "xs", Value: "10px"},   // Footnote, Caption 1, Caption 2
		SM:   style.FontSize{Name: "sm", Value: "11px"},   // Subheadline
		Base: style.FontSize{Name: "base", Value: "13px"}, // Body, Headline
		LG:   style.FontSize{Name: "lg", Value: "15px"},   // Title 3
		XL:   style.FontSize{Name: "xl", Value: "17px"},   // Title 2
		XXL:  style.FontSize{Name: "2xl", Value: "22px"},  // Title 1
		XXXL: style.FontSize{Name: "3xl", Value: "26px"},  // Large Title
	}

	// Concentric-corner rule, WWDC25 session 356: "concentric shapes
	// calculate their radius by subtracting padding from the parent's",
	// capsules use half the container height, fixed shapes keep a
	// constant radius. Apple publishes no radius numbers (plan,
	// "unverified"), so the two fixed anchors are measured:
	//
	//	XL 16  thick-glass sheet/popover outer corner, measured, unverified
	//	LG 12  menu and popover corner, measured, unverified
	//	MD 8 = XL 16 - 8   (a group inside a sheet, 8px padding between)
	//	SM 4 = MD 8 - 4    (a control inside a group, 4px padding between)
	//	Full    capsule rule: half the container height
	t.Radii = style.RadiusSet{
		None: style.Radius{Name: "none", Value: 0},
		SM:   style.Radius{Name: "sm", Value: 4},
		MD:   style.Radius{Name: "md", Value: 8},
		LG:   style.Radius{Name: "lg", Value: 12},
		XL:   style.Radius{Name: "xl", Value: 16},
		Full: style.Radius{Name: "full", Value: 9999},
	}

	// Plan, "The web side in WebKit": Canvas/CanvasText and AccentColor
	// ARE in WebKit (Safari 16.5+), but the design system's color-token
	// grammar refuses them (canvas/canvastext/accentcolor are not CSS
	// named colors and a keyword fallback chain is not one function
	// call; pinned by TestColorGrammarRefusesSystemKeywords). Until the
	// grammar learns system keywords, the nearest sourced values stand
	// in: WebKit resolves canvas and canvastext to white and black in
	// light mode (measured, unverified), and the accent is the macOS
	// system blue. -webkit-focus-ring-color cannot ride along as a
	// fallback for the same grammar reason.
	t.Colors.Background = style.Color{Name: "background", Value: "#FFFFFF"}
	t.Colors.Text = style.Color{Name: "text", Value: "#000000"}
	// The macOS system blue. Measured contrast on the light value:
	// #007AFF with #FFFFFF text is 4.0:1 (Apple's own selection rows
	// and buttons ship this pair); black text on it is 5.2:1 and passes
	// AA, a host that needs strict AA can override primary-fg.
	t.Colors.Accent = style.Color{Name: "accent", Value: "#007AFF"}
	t.Colors.Primary = style.Color{Name: "primary", Value: "#007AFF"}
	t.Colors.PrimaryFg = style.Color{Name: "primary-fg", Value: "#FFFFFF"}
	// Apple systemGray6, the light control/material tint. measured, unverified
	t.Colors.SurfaceSoft = style.Color{Name: "surface-soft", Value: "#F2F2F7"}

	// Dark palette matched to macOS dark surfaces (measured,
	// unverified). Only the tokens that change are re-declared; the
	// framework's contrast-checked status colors and code surface
	// carry over from the canonical dark palette unchanged.
	for k, v := range map[string]string{
		"background":   "#1E1E1E", // dark window chrome
		"surface":      "#2A2A2A", // cards and sheets on the dark window
		"surface-soft": "#333336", // dark control tint
		"text":         "#F5F5F7", // Apple primary label on dark
		"text-muted":   "#98989D", // Apple secondary label on dark
		"text-subtle":  "#8E8E93", // Apple tertiary label on dark
		"accent":       "#0A84FF", // system blue, dark variant
		"primary":      "#0A84FF",
		"primary-fg":   "#FFFFFF",
	} {
		t.DarkColors[k] = v
	}

	// The producer assigns color values directly, so it runs the
	// grammar itself (the struct-assignment path validates nothing).
	for _, c := range []style.Color{
		t.Colors.Background, t.Colors.Text, t.Colors.Accent,
		t.Colors.Primary, t.Colors.PrimaryFg, t.Colors.SurfaceSoft,
	} {
		if err := style.ValidateColorValue(c.Value); err != nil {
			panic("desktopui: theme color " + c.Name + ": " + err.Error())
		}
	}

	return t
}
