package theme

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// Default returns the canonical adaptive framework theme, including complete
// light and dark semantic palettes.
//
// Pass an Overrides value to swap individual tokens. Overrides are
// applied on top of the typed style.DefaultTheme(); unset fields keep
// their defaults.
//
// Example — swap primary from indigo to teal in both palettes:
//
//	t := theme.Default(theme.Overrides{
//		Primary: "#0F766E",
//		DarkColors: map[string]string{"primary": "#5EEAD4"},
//	})
//
// Every component referencing --color-primary updates without any
// code change.
func Default(overrides ...Overrides) style.Theme {
	t := baseTheme()
	for _, o := range overrides {
		applyOverrides(&t, o)
	}
	return t
}

// Overrides is the set of tokens a host can swap to re-skin the
// framework theme. All fields are optional. Empty strings are
// ignored — zero-value RadiusXX ints likewise.
type Overrides struct {
	// Color tokens (CSS hex values).
	Background, Surface, SurfaceSoft string
	Border, BorderStrong             string
	Text, TextMuted, TextSubtle      string
	Primary, PrimaryFg               string
	Accent                           string
	Success, Warning, Danger, Info   string

	// Code-display surface tokens (ui.CodeBlock + demo source panels).
	// Intentionally a separate pair so dark mode reskins code blocks
	// independently of the page Text/Background pair.
	CodeSurface, CodeText, CodeBorder string

	// DarkColors explicitly overrides semantic dark-palette tokens by their
	// CSS name (for example "primary" or "surface-soft"). Light color fields
	// are not copied into dark mode because a contrast-safe dark value is
	// usually different.
	DarkColors map[string]string

	// Font families.
	FontBody, FontHeading, FontMono string

	// Reskin extras — only apply if you really need them.
	RadiusSm, RadiusMd, RadiusLg int // px
}

// baseTheme is the framework's opinionated default — a clean
// neutral palette with indigo primary. Identical shape to
// style.DefaultTheme(), but framework-ui has its own slight
// adjustments to colors and fonts.
func baseTheme() style.Theme {
	t := style.DefaultTheme()
	t.Name = "framework-ui"
	// Override a few values where framework-ui differs from the
	// generic style defaults.
	t.Colors.Background = style.Color{Name: "background", Value: "#FAFAF9"}
	t.Colors.Accent = style.Color{Name: "accent", Value: "#0891B2"}
	t.Fonts.Body = style.Font{Name: "body", Value: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif"}
	t.Fonts.Heading = style.Font{Name: "heading", Value: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif"}
	t.Fonts.Mono = style.Font{Name: "mono", Value: "ui-monospace, 'SF Mono', Menlo, Consolas, monospace"}
	// The framework theme is adaptive by default. core-ui's lower-level
	// style.DefaultTheme intentionally remains light-only for compatibility;
	// host applications and generated projects should use this theme so a
	// ThemeToggle and the synchronous OS-preference bootstrap always have a
	// complete, contrast-safe dark palette to switch to.
	t.DarkColors = map[string]string{
		"accent":        "#67E8F9",
		"background":    "#111113",
		"border":        "#3F3F46",
		"border-strong": "#71717A",
		"danger":        "#F87171",
		"info":          "#60A5FA",
		"primary":       "#A5B4FC",
		"primary-fg":    "#111827",
		"secondary":     "#D4D4D8",
		"secondary-fg":  "#18181B",
		"success":       "#4ADE80",
		"surface":       "#18181B",
		"surface-soft":  "#27272A",
		"text":          "#FAFAFA",
		"text-muted":    "#D4D4D8",
		"text-subtle":   "#A1A1AA",
		"warning":       "#FBBF24",
	}
	return t
}

// applyOverrides mutates t in place — only non-zero override fields
// touch the theme. Token Name preserved; only Value swaps.
func applyOverrides(t *style.Theme, o Overrides) {
	setColor := func(c *style.Color, v string) {
		if v == "" {
			return
		}
		c.Value = v
	}
	setColor(&t.Colors.Background, o.Background)
	setColor(&t.Colors.Surface, o.Surface)
	setColor(&t.Colors.SurfaceSoft, o.SurfaceSoft)
	setColor(&t.Colors.Border, o.Border)
	setColor(&t.Colors.BorderStrong, o.BorderStrong)
	setColor(&t.Colors.Text, o.Text)
	setColor(&t.Colors.TextMuted, o.TextMuted)
	setColor(&t.Colors.TextSubtle, o.TextSubtle)
	setColor(&t.Colors.Primary, o.Primary)
	setColor(&t.Colors.PrimaryFg, o.PrimaryFg)
	setColor(&t.Colors.Accent, o.Accent)
	setColor(&t.Colors.Success, o.Success)
	setColor(&t.Colors.Warning, o.Warning)
	setColor(&t.Colors.Danger, o.Danger)
	setColor(&t.Colors.Info, o.Info)
	setColor(&t.Colors.CodeSurface, o.CodeSurface)
	setColor(&t.Colors.CodeText, o.CodeText)
	setColor(&t.Colors.CodeBorder, o.CodeBorder)
	for name, value := range o.DarkColors {
		if value != "" {
			t.DarkColors[name] = value
		}
	}

	setFont := func(f *style.Font, v string) {
		if v == "" {
			return
		}
		f.Value = v
	}
	setFont(&t.Fonts.Body, o.FontBody)
	setFont(&t.Fonts.Heading, o.FontHeading)
	setFont(&t.Fonts.Mono, o.FontMono)

	if o.RadiusSm > 0 {
		t.Radii.SM.Value = o.RadiusSm
	}
	if o.RadiusMd > 0 {
		t.Radii.MD.Value = o.RadiusMd
	}
	if o.RadiusLg > 0 {
		t.Radii.LG.Value = o.RadiusLg
	}
}
