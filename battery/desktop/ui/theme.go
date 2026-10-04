package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Theme returns the macOS desktop theme preset. It remains the
// compatibility shorthand for ThemeFor("darwin"). Use ThemeFor with
// runtime.GOOS when the app should use its host platform's preset.
//
//	site := app.NewApp("Focus")
//	site.WithTheme(desktopui.Theme())
func Theme() style.Theme {
	return ThemeFor("darwin")
}

// ThemeFor returns the canonical adaptive desktop theme with token
// values for goos, using the names returned by runtime.GOOS. The token
// fields stay the same across platforms. Windows and Linux presets use
// their system font stacks and light/dark surface colors; an unknown
// GOOS keeps the macOS reference preset.
//
//	site.WithTheme(desktopui.ThemeFor(runtime.GOOS))
func ThemeFor(goos string) style.Theme {
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

	// A desktop window is driven by a pointer: controls size to the
	// WCAG 2.5.8 minimum target (24 CSS px) instead of the 44 px
	// touch floor, near macOS's regular control heights. The desktop
	// layouts set the matching --ui-control-padding-y. Measured,
	// unverified.
	t.Layout.TouchTarget = style.Spacing{Name: "touch-target", Value: 24}

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
	// The macOS system blue is the accent (focus rings, selection,
	// links). Filled buttons cannot wear it: #FFFFFF on #007AFF is
	// 4.02:1, under the 4.5:1 AA floor style.Theme.Validate enforces
	// for primary × primary-fg. Primary is #0071E3, the darker blue
	// Apple's own web buttons use under white text (4.7:1), in both
	// schemes, so a filled button reads as the system blue and clears
	// AA.
	t.Colors.Accent = style.Color{Name: "accent", Value: "#007AFF"}
	t.Colors.Primary = style.Color{Name: "primary", Value: "#0071E3"}
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
		"primary":      "#0071E3", // AA under white; see Primary above
		"primary-fg":   "#FFFFFF",
	} {
		t.DarkColors[k] = v
	}

	applyPlatformTokens(&t, goos)

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

type platformThemeTokens struct {
	fonts [3]string
	light [7]string
	dark  [9]string
}

// Platform values live as data so the Windows and Linux presets share one
// application path instead of parallel assignment blocks.
var platformThemes = map[string]platformThemeTokens{
	// Fluent's system UI font and Windows 11 neutral surfaces. Accent is a
	// static blue fallback until native UISettings colors feed theme creation.
	"windows": {
		fonts: [3]string{"system-ui, 'Segoe UI', sans-serif", "system-ui, 'Segoe UI', sans-serif", "ui-monospace, 'Cascadia Mono', Consolas, monospace"},
		light: [7]string{"#F3F3F3", "#FFFFFF", "#F9F9F9", "#1C1C1C", "#0078D4", "#0067C0", "#FFFFFF"},
		dark:  [9]string{"#202020", "#1C1C1C", "#282828", "#FFFFFF", "#C5C5C5", "#878787", "#60CDFF", "#0067C0", "#FFFFFF"},
	},
	// Adwaita documents these fonts, surfaces, and blue accents. Blue-4 is
	// the filled-control primary because it clears the 4.5:1 ink-pair floor.
	"linux": {
		fonts: [3]string{"system-ui, 'Adwaita Sans', Cantarell, sans-serif", "system-ui, 'Adwaita Sans', Cantarell, sans-serif", "ui-monospace, 'Adwaita Mono', monospace"},
		light: [7]string{"#FAFAFB", "#FFFFFF", "#EBEBED", "#333334", "#0461BE", "#1C71D8", "#FFFFFF"},
		dark:  [9]string{"#222226", "#1D1D20", "#2E2E32", "#FFFFFF", "#C5C5C5", "#A0A0A0", "#81D0FF", "#1A5FB4", "#FFFFFF"},
	},
}

func applyPlatformTokens(t *style.Theme, goos string) {
	p, ok := platformThemes[goos]
	if !ok {
		return
	}
	t.Fonts.Body.Value, t.Fonts.Heading.Value, t.Fonts.Mono.Value = p.fonts[0], p.fonts[1], p.fonts[2]
	light := []*style.Color{
		&t.Colors.Background, &t.Colors.Surface, &t.Colors.SurfaceSoft,
		&t.Colors.Text, &t.Colors.Accent, &t.Colors.Primary, &t.Colors.PrimaryFg,
	}
	for i, color := range light {
		color.Value = p.light[i]
	}
	darkKeys := [...]string{"background", "surface", "surface-soft", "text", "text-muted", "text-subtle", "accent", "primary", "primary-fg"}
	for i, key := range darkKeys {
		t.DarkColors[key] = p.dark[i]
	}
}
