package theme

import (
	"fmt"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Default returns the canonical adaptive framework theme, including complete
// light and dark semantic palettes.
//
// Pass an Overrides value to swap individual tokens. Overrides are
// applied on top of the typed style.DefaultTheme(); unset fields keep
// their defaults.
//
// Example. Swap primary from indigo to teal in both palettes:
//
//	t := theme.Default(theme.Overrides{
//		Primary: "#0F766E",
//		Dark:    &theme.Overrides{Primary: "#5EEAD4"},
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
// ignored, zero-value RadiusXX ints likewise.
type Overrides struct {
	// Color tokens (CSS hex values).
	Background, Surface, SurfaceSoft         string
	Border, BorderStrong                     string
	Text, TextMuted, TextSubtle              string
	Primary, PrimaryFg                       string
	Accent                                   string
	Success, Warning, Danger, DangerFg, Info string

	// Code-display surface tokens (ui.CodeBlock + demo source panels).
	// Intentionally a separate pair so dark mode reskins code blocks
	// independently of the page Text/Background pair.
	CodeSurface, CodeText, CodeBorder string

	// Dark is the dark-mode twin of the colour fields above: the same
	// typed fields, compiled into the theme's dark palette
	// (style.Theme.DarkColors) keyed by the CSS names the dark-scheme
	// blocks read. Light colour fields are not copied into dark mode
	// automatically because a contrast-safe dark value is usually
	// different; a light override with no dark twin logs a warning
	// naming the field.
	//
	// Dark changes colours only: setting Components, a font, a radius
	// or a nested Dark inside it panics when the theme is built. A
	// non-nil Dark with no colour set (Dark: &theme.Overrides{}) means
	// "the framework's dark palette is deliberate" and silences the
	// light-only warning.
	Dark *Overrides

	// Components are the typed component options (Density, the button
	// family's Treatment and Radius). Zero values mean "leave the
	// construction base unchanged" while overrides merge, exactly like
	// the string fields above; an explicit Comfortable, Filled or Round
	// resets an earlier override. The merged result is flattened into
	// style.Theme.Components complete, so every theme this package
	// produces declares a full option set.
	Components ComponentOptions

	// Font families.
	FontBody, FontHeading, FontMono string

	// Reskin extras: only apply if you really need them.
	RadiusSm, RadiusMd, RadiusLg int // px
}

// baseTheme is the framework's opinionated default: style.DefaultTheme's
// neutral zinc palette (near-black primary, white page, hairline
// borders) plus a complete dark palette.
func baseTheme() style.Theme {
	t := style.DefaultTheme()
	t.Name = "framework-ui"
	// The framework theme is adaptive by default. core-ui's lower-level
	// style.DefaultTheme intentionally remains light-only for compatibility;
	// host applications and generated projects should use this theme so a
	// ThemeToggle and the synchronous OS-preference bootstrap always have a
	// complete, contrast-safe dark palette to switch to.
	t.DarkColors = map[string]string{
		"accent":        "#60A5FA",
		"background":    "#09090B",
		"border":        "#27272A",
		"border-strong": "#3F3F46",
		// Code-surface tokens: the dark values match the light ones on
		// purpose — the code surface is already dark in both schemes
		// (see style.ColorSet.CodeSurface), so dark mode reuses the
		// inkwell. Declared explicitly so the dark-gap boot check sees
		// the theme as complete rather than warning about an omission.
		"code-border":  "#27272A",
		"code-surface": "#18181B",
		"code-text":    "#E4E4E7",
		"danger":       "#F87171",
		"danger-fg":    "#111827", // 6.41:1 on the dark danger fill; the dark ink mirrors primary-fg's
		"info":         "#60A5FA",
		"primary":      "#FAFAFA",
		"primary-fg":   "#18181B",
		"secondary":    "#27272A",
		"secondary-fg": "#FAFAFA",
		"success":      "#4ADE80",
		"surface":      "#18181B",
		"surface-soft": "#27272A",
		"text":         "#FAFAFA",
		"text-muted":   "#D4D4D8",
		"text-subtle":  "#A1A1AA",
		"warning":      "#FBBF24",
	}
	// The complete default option set, flattened. Every theme built
	// here carries a full option set, which is what makes option
	// variables nest: each boundary redeclares the whole set.
	t.Components = DefaultOptions.Flattened()
	return t
}

// colorFields pairs every colour-bearing Overrides field with the CSS
// token name it compiles to. One table drives the three walks that
// must agree — the light setters, the Dark compile into
// style.Theme.DarkColors, and the light-only warning — so a colour
// field can never exist in one walk and not the others. That drift is
// exactly what made the old string-keyed DarkColors map silently drop
// a typo'd token name ("surfce-soft" compiled and did nothing).
var colorFields = []struct {
	field string // Go field name, for panic and warning messages
	css   string // key in style.Theme.DarkColors
	get   func(*Overrides) string
	set   func(*style.Theme, string)
}{
	{"Background", "background", func(o *Overrides) string { return o.Background }, func(t *style.Theme, v string) { t.Colors.Background.Value = v }},
	{"Surface", "surface", func(o *Overrides) string { return o.Surface }, func(t *style.Theme, v string) { t.Colors.Surface.Value = v }},
	{"SurfaceSoft", "surface-soft", func(o *Overrides) string { return o.SurfaceSoft }, func(t *style.Theme, v string) { t.Colors.SurfaceSoft.Value = v }},
	{"Border", "border", func(o *Overrides) string { return o.Border }, func(t *style.Theme, v string) { t.Colors.Border.Value = v }},
	{"BorderStrong", "border-strong", func(o *Overrides) string { return o.BorderStrong }, func(t *style.Theme, v string) { t.Colors.BorderStrong.Value = v }},
	{"Text", "text", func(o *Overrides) string { return o.Text }, func(t *style.Theme, v string) { t.Colors.Text.Value = v }},
	{"TextMuted", "text-muted", func(o *Overrides) string { return o.TextMuted }, func(t *style.Theme, v string) { t.Colors.TextMuted.Value = v }},
	{"TextSubtle", "text-subtle", func(o *Overrides) string { return o.TextSubtle }, func(t *style.Theme, v string) { t.Colors.TextSubtle.Value = v }},
	{"Primary", "primary", func(o *Overrides) string { return o.Primary }, func(t *style.Theme, v string) { t.Colors.Primary.Value = v }},
	{"PrimaryFg", "primary-fg", func(o *Overrides) string { return o.PrimaryFg }, func(t *style.Theme, v string) { t.Colors.PrimaryFg.Value = v }},
	{"Accent", "accent", func(o *Overrides) string { return o.Accent }, func(t *style.Theme, v string) { t.Colors.Accent.Value = v }},
	{"Success", "success", func(o *Overrides) string { return o.Success }, func(t *style.Theme, v string) { t.Colors.Success.Value = v }},
	{"Warning", "warning", func(o *Overrides) string { return o.Warning }, func(t *style.Theme, v string) { t.Colors.Warning.Value = v }},
	{"Danger", "danger", func(o *Overrides) string { return o.Danger }, func(t *style.Theme, v string) { t.Colors.Danger.Value = v }},
	{"DangerFg", "danger-fg", func(o *Overrides) string { return o.DangerFg }, func(t *style.Theme, v string) { t.Colors.DangerFg.Value = v }},
	{"Info", "info", func(o *Overrides) string { return o.Info }, func(t *style.Theme, v string) { t.Colors.Info.Value = v }},
	{"CodeSurface", "code-surface", func(o *Overrides) string { return o.CodeSurface }, func(t *style.Theme, v string) { t.Colors.CodeSurface.Value = v }},
	{"CodeText", "code-text", func(o *Overrides) string { return o.CodeText }, func(t *style.Theme, v string) { t.Colors.CodeText.Value = v }},
	{"CodeBorder", "code-border", func(o *Overrides) string { return o.CodeBorder }, func(t *style.Theme, v string) { t.Colors.CodeBorder.Value = v }},
}

// applyOverrides mutates t in place: only non-zero override fields
// touch the theme. Token Name preserved; only Value swaps. A Dark
// override is validated first (dark mode changes colours only) and
// then compiled into t.DarkColors, the map the dark-scheme CSS
// blocks read.
func applyOverrides(t *style.Theme, o Overrides) {
	if o.Dark != nil {
		o.Dark.mustBeColourOnly()
	}
	for _, f := range colorFields {
		if v := f.get(&o); v != "" {
			f.set(t, v)
		}
	}
	applyDark(t, o.Dark)
	if o.Dark == nil {
		warnLightOnly(t, o)
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
	applyComponentOptions(t, o.Components)
}

// applyDark compiles a Dark override's colour fields into the theme's
// dark palette (style.Theme.DarkColors), keyed by the CSS names the
// dark-scheme blocks read. Empty fields are ignored, exactly like the
// light setters.
func applyDark(t *style.Theme, dark *Overrides) {
	if dark == nil {
		return
	}
	for _, f := range colorFields {
		v := f.get(dark)
		if v == "" {
			continue
		}
		if t.DarkColors == nil {
			t.DarkColors = map[string]string{}
		}
		t.DarkColors[f.css] = v
	}
}

// mustBeColourOnly panics when a Dark override carries anything but
// colour fields. Dark mode changes colours only: component options,
// fonts and radii are scheme-independent and belong on the top level,
// and a nested Dark has no meaning. Failing at theme build turns a
// configuration mistake the compiler accepted into an error at boot.
func (d *Overrides) mustBeColourOnly() {
	if d.Components != (ComponentOptions{}) {
		panic("theme: Dark.Components is set; dark mode changes colours only. Put Components on the top level.")
	}
	for _, f := range []struct{ name, value string }{
		{"FontBody", d.FontBody},
		{"FontHeading", d.FontHeading},
		{"FontMono", d.FontMono},
	} {
		if f.value != "" {
			panic("theme: Dark." + f.name + " is set; dark mode changes colours only. Put " + f.name + " on the top level.")
		}
	}
	for _, r := range []struct {
		name  string
		value int
	}{
		{"RadiusSm", d.RadiusSm},
		{"RadiusMd", d.RadiusMd},
		{"RadiusLg", d.RadiusLg},
	} {
		if r.value != 0 {
			panic("theme: Dark." + r.name + " is set; dark mode changes colours only. Put " + r.name + " on the top level.")
		}
	}
	if d.Dark != nil {
		panic("theme: Dark.Dark is set; dark mode changes colours only. There is no dark-of-dark; set colours here and everything else on the top level.")
	}
}

// warnLightOnly names every colour field the override sets for light
// mode while the theme carries a dark value for that token: dark mode
// keeps painting that dark value, which is rarely what a re-skin
// wants. One slog.Warn per field per theme build. A non-nil Dark —
// even with no colour set, which says the framework's dark palette is
// deliberate — silences the warning.
func warnLightOnly(t *style.Theme, o Overrides) {
	for _, f := range colorFields {
		light := f.get(&o)
		if light == "" {
			continue
		}
		dark := t.DarkColors[f.css]
		if dark == "" {
			continue
		}
		slog.Warn(fmt.Sprintf(
			"theme: %s is set for light (%s) but not in Dark; dark mode keeps the framework's %s. Set Dark.%s, or Dark: &theme.Overrides{} to silence this.",
			f.field, light, dark, f.field))
	}
}

// applyComponentOptions merges the typed options into the theme's
// flattened map: only non-unset fields write their key, so an
// explicit Comfortable, Filled or Round resets an earlier override
// while an unset one leaves the base value standing. The base map is
// complete, so the result is too.
func applyComponentOptions(t *style.Theme, o ComponentOptions) {
	flat := o.Flattened()
	if len(flat) == 0 {
		return
	}
	if t.Components == nil {
		t.Components = map[string]string{}
	}
	for k, v := range flat {
		t.Components[k] = v
	}
}
