package theme

import (
	"fmt"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Default returns the adaptive framework theme with every override applied
// in order; an unset field keeps its default.
func Default(overrides ...Overrides) style.Theme {
	t := baseTheme()
	for _, o := range overrides {
		applyOverrides(&t, o)
	}
	return t
}

// Overrides is the set of tokens a host can swap; zero fields are ignored.
type Overrides struct {
	Background, Surface, SurfaceSoft         string
	Border, BorderStrong                     string
	Text, TextMuted, TextSubtle              string
	Primary, PrimaryFg                       string
	Accent                                   string
	Success, Warning, Danger, DangerFg, Info string

	CodeSurface, CodeText, CodeBorder string

	// Dark holds the dark palette's colours; any other field set in it panics.
	Dark *Overrides

	Components ComponentOptions

	FontBody, FontHeading, FontMono string

	RadiusSm, RadiusMd, RadiusLg int // px
}

func baseTheme() style.Theme {
	t := style.DefaultTheme()
	t.Name = "framework-ui"
	t.DarkColors = style.DefaultDarkColors()
	t.Components = DefaultOptions.Flattened()
	return t
}

var colorFields = []struct {
	field string
	css   string
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
