package theme

import "github.com/gofastr/gofastr/core-ui/style"

// Default returns the canonical framework theme.
//
// Pass an Overrides value to swap individual tokens. Overrides are
// merged on top of the defaults; unset fields keep their defaults.
//
// Example — swap primary from indigo to teal:
//
//	t := theme.Default(theme.Overrides{Primary: "#14B8A6"})
//
// Every component that references --color-primary updates without
// any code change.
func Default(overrides ...Overrides) style.Theme {
	t := baseTheme()
	for _, o := range overrides {
		t = applyOverrides(t, o)
	}
	return t
}

// Overrides is the set of tokens a host can swap to re-skin the
// framework theme. All fields are optional. Empty strings are ignored.
type Overrides struct {
	// Color tokens (CSS hex values).
	Background    string // page backdrop
	Surface       string // card / widget background
	SurfaceSoft   string // muted surface (callouts, hover states)
	Border        string // hairline borders
	BorderStrong  string // emphasized borders (active states)
	Text          string // body text
	TextMuted     string // secondary text
	TextSubtle    string // tertiary text (timestamps, hints)
	Primary       string // brand color
	PrimaryFg     string // text/icon color on primary
	Accent        string // secondary accent
	Success       string // positive state
	Warning       string // caution state
	Danger        string // destructive state
	Info          string // informational state

	// Font families.
	FontBody    string
	FontHeading string
	FontMono    string

	// Reskin extras — only apply if you really need them.
	RadiusSm  int // px
	RadiusMd  int // px
	RadiusLg  int // px
}

func baseTheme() style.Theme {
	return style.Theme{
		Name: "framework-ui",
		Colors: style.Colors{
			"background":    "#FAFAF9",
			"surface":       "#FFFFFF",
			"surface-soft":  "#F4F4F5",
			"border":        "#E4E4E7",
			"border-strong": "#A1A1AA",
			"text":          "#18181B",
			"text-muted":    "#52525B",
			"text-subtle":   "#A1A1AA",
			"primary":       "#4F46E5",
			"primary-fg":    "#FFFFFF",
			"accent":        "#0891B2",
			"success":       "#16A34A",
			"warning":       "#CA8A04",
			"danger":        "#DC2626",
			"info":          "#2563EB",
		},
		Spacing: style.Spacing{
			"xs":  2,
			"sm":  4,
			"md":  8,
			"lg":  16,
			"xl":  24,
			"2xl": 32,
			"3xl": 48,
		},
		Radii: style.Radii{
			"none": 0,
			"sm":   4,
			"md":   8,
			"lg":   12,
			"xl":   16,
			"full": 9999,
		},
		Fonts: style.Fonts{
			"body":    "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif",
			"heading": "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif",
			"mono":    "ui-monospace, 'SF Mono', Menlo, Consolas, monospace",
		},
		Breakpoints: style.Breakpoints{
			"sm": 640,
			"md": 768,
			"lg": 1024,
			"xl": 1280,
		},
	}
}

func applyOverrides(t style.Theme, o Overrides) style.Theme {
	custom := style.Theme{Colors: style.Colors{}, Fonts: style.Fonts{}, Radii: style.Radii{}}
	if o.Background != "" {
		custom.Colors["background"] = o.Background
	}
	if o.Surface != "" {
		custom.Colors["surface"] = o.Surface
	}
	if o.SurfaceSoft != "" {
		custom.Colors["surface-soft"] = o.SurfaceSoft
	}
	if o.Border != "" {
		custom.Colors["border"] = o.Border
	}
	if o.BorderStrong != "" {
		custom.Colors["border-strong"] = o.BorderStrong
	}
	if o.Text != "" {
		custom.Colors["text"] = o.Text
	}
	if o.TextMuted != "" {
		custom.Colors["text-muted"] = o.TextMuted
	}
	if o.TextSubtle != "" {
		custom.Colors["text-subtle"] = o.TextSubtle
	}
	if o.Primary != "" {
		custom.Colors["primary"] = o.Primary
	}
	if o.PrimaryFg != "" {
		custom.Colors["primary-fg"] = o.PrimaryFg
	}
	if o.Accent != "" {
		custom.Colors["accent"] = o.Accent
	}
	if o.Success != "" {
		custom.Colors["success"] = o.Success
	}
	if o.Warning != "" {
		custom.Colors["warning"] = o.Warning
	}
	if o.Danger != "" {
		custom.Colors["danger"] = o.Danger
	}
	if o.Info != "" {
		custom.Colors["info"] = o.Info
	}
	if o.FontBody != "" {
		custom.Fonts["body"] = o.FontBody
	}
	if o.FontHeading != "" {
		custom.Fonts["heading"] = o.FontHeading
	}
	if o.FontMono != "" {
		custom.Fonts["mono"] = o.FontMono
	}
	if o.RadiusSm > 0 {
		custom.Radii["sm"] = o.RadiusSm
	}
	if o.RadiusMd > 0 {
		custom.Radii["md"] = o.RadiusMd
	}
	if o.RadiusLg > 0 {
		custom.Radii["lg"] = o.RadiusLg
	}
	return style.MergeThemes(t, custom)
}
