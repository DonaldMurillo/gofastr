package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// ThemeFor returns the desktop theme with goos's token values; an unknown GOOS gets the macOS preset.
func ThemeFor(goos string) style.Theme {
	t := uitheme.Default()
	t.Name = "desktop"

	t.Fonts.Body = style.Font{Name: "body", Value: "-apple-system, system-ui, ui-sans-serif, sans-serif"}
	t.Fonts.Heading = style.Font{Name: "heading", Value: "-apple-system, system-ui, ui-sans-serif, sans-serif"}
	t.Fonts.Mono = style.Font{Name: "mono", Value: "ui-monospace, 'SF Mono', Menlo, monospace"}

	t.Typography = style.FontSizeSet{
		XS:   style.FontSize{Name: "xs", Value: "10px"},   // Footnote, Caption 1, Caption 2
		SM:   style.FontSize{Name: "sm", Value: "11px"},   // Subheadline
		Base: style.FontSize{Name: "base", Value: "13px"}, // Body, Headline
		LG:   style.FontSize{Name: "lg", Value: "15px"},   // Title 3
		XL:   style.FontSize{Name: "xl", Value: "17px"},   // Title 2
		XXL:  style.FontSize{Name: "2xl", Value: "22px"},  // Title 1
		XXXL: style.FontSize{Name: "3xl", Value: "26px"},  // Large Title
	}

	t.Radii = style.RadiusSet{
		None: style.Radius{Name: "none", Value: 0},
		SM:   style.Radius{Name: "sm", Value: 4},
		MD:   style.Radius{Name: "md", Value: 8},
		LG:   style.Radius{Name: "lg", Value: 12},
		XL:   style.Radius{Name: "xl", Value: 16},
		Full: style.Radius{Name: "full", Value: 9999},
	}

	t.Layout.TouchTarget = style.Spacing{Name: "touch-target", Value: 24}

	t.Colors.Background = style.Color{Name: "background", Value: "#FFFFFF"}
	t.Colors.Text = style.Color{Name: "text", Value: "#000000"}
	t.Colors.Accent = style.Color{Name: "accent", Value: "#007AFF"}
	t.Colors.Primary = style.Color{Name: "primary", Value: "#0071E3"}
	t.Colors.PrimaryFg = style.Color{Name: "primary-fg", Value: "#FFFFFF"}
	t.Colors.SurfaceSoft = style.Color{Name: "surface-soft", Value: "#F2F2F7"}

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

var platformThemes = map[string]platformThemeTokens{
	"windows": {
		fonts: [3]string{"system-ui, 'Segoe UI', sans-serif", "system-ui, 'Segoe UI', sans-serif", "ui-monospace, 'Cascadia Mono', Consolas, monospace"},
		light: [7]string{"#F3F3F3", "#FFFFFF", "#F9F9F9", "#1C1C1C", "#0078D4", "#0067C0", "#FFFFFF"},
		dark:  [9]string{"#202020", "#1C1C1C", "#282828", "#FFFFFF", "#C5C5C5", "#878787", "#60CDFF", "#0067C0", "#FFFFFF"},
	},
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
