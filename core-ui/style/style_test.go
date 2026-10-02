package style

import (
	"math"
	"strings"
	"testing"
)

// --- DefaultTheme ----------------------------------------------------------

func TestDefaultThemeHasAllTokens(t *testing.T) {
	th := DefaultTheme()
	checks := []struct {
		got  string
		want string
	}{
		{th.Colors.Primary.Name, "primary"},
		{th.Colors.Text.Name, "text"},
		{th.Colors.Background.Name, "background"},
		{th.Spacing.MD.Name, "md"},
		{th.Radii.MD.Name, "md"},
		{th.Fonts.Body.Name, "body"},
		{th.Breakpoints.MD.Name, "md"},
		{th.Shadows.MD.Name, "md"},
		{th.ZIndex.Modal.Name, "modal"},
		{th.Durations.Normal.Name, "normal"},
		{th.Typography.Base.Name, "base"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("DefaultTheme Name = %q, want %q", c.got, c.want)
		}
	}
}

// --- Resolve* (var-only contract) -----------------------------------------

func TestResolveAllEmitsVarRefs(t *testing.T) {
	th := DefaultTheme()
	cases := []struct {
		in, want string
	}{
		{"{colors.primary}", "var(--color-primary)"},
		{"{spacing.md}", "var(--spacing-md)"},
		{"{radii.lg}", "var(--radii-lg)"},
		{"{fonts.body}", "var(--font-body)"},
		{"padding: {spacing.sm} {spacing.lg}", "padding: var(--spacing-sm) var(--spacing-lg)"},
	}
	for _, c := range cases {
		got := th.ResolveAll(c.in)
		if got != c.want {
			t.Errorf("ResolveAll(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveAllPassesThroughUnknownCategory(t *testing.T) {
	th := DefaultTheme()
	got := th.ResolveAll("{unknown.token} + {colors.primary}")
	if !strings.Contains(got, "{unknown.token}") {
		t.Errorf("unknown category should pass through: %q", got)
	}
	if !strings.Contains(got, "var(--color-primary)") {
		t.Errorf("known token still resolved: %q", got)
	}
}

func TestResolveColorReturnsVarRef(t *testing.T) {
	if got := DefaultTheme().ResolveColor("primary"); got != "var(--color-primary)" {
		t.Errorf("ResolveColor: %q", got)
	}
}

func TestResolveSpacingReturnsVarRef(t *testing.T) {
	if got := DefaultTheme().ResolveSpacing("md"); got != "var(--spacing-md)" {
		t.Errorf("ResolveSpacing: %q", got)
	}
}

// --- CSSCustomProperties --------------------------------------------------

func TestCSSCustomPropertiesEmitsAllCategories(t *testing.T) {
	css := DefaultTheme().CSSCustomProperties()
	wants := []string{
		"--color-primary: #4F46E5;",
		"--color-text: #18181B;",
		"--spacing-md: 8px;",
		"--radii-md: 8px;",
		"--font-body:",
		"--breakpoint-md: 768px;",
		"--shadow-md:",
		"--z-modal: 300;",
		"--duration-fast: 150ms;",
		"--text-base: 1rem;",
	}
	for _, w := range wants {
		if !strings.Contains(css, w) {
			t.Errorf("CSSCustomProperties missing %q\n--- CSS ---\n%s", w, css)
		}
	}
}

func TestCSSCustomPropertiesDeterministic_AllCategories(t *testing.T) {
	first := DefaultTheme().CSSCustomProperties()
	for i := range 50 {
		got := DefaultTheme().CSSCustomProperties()
		if got != first {
			t.Fatalf("non-deterministic at iter %d", i)
		}
	}
}

func TestCSSCustomPropertiesOfEmbeddedTheme(t *testing.T) {
	type appTheme struct {
		Theme
		Brand struct {
			Logo Color
		}
	}
	at := appTheme{Theme: DefaultTheme()}
	at.Brand.Logo = Color{Name: "brand-logo", Value: "#FF00FF"}
	css := CSSCustomPropertiesOf(at)
	if !strings.Contains(css, "--color-primary:") {
		t.Errorf("missing canonical color in embedded-theme :root: %s", css)
	}
	if !strings.Contains(css, "--color-brand-logo: #FF00FF;") {
		t.Errorf("missing embedded brand color: %s", css)
	}
}

// --- Classes --------------------------------------------------------------

func TestClassesToAttr(t *testing.T) {
	c := Classes{"foo": true, "bar": false, "baz": true}
	if got := c.ToAttr()["class"]; got != "baz foo" {
		t.Errorf("Classes.ToAttr = %q, want sorted active set", got)
	}
}

func TestClassesEmpty(t *testing.T) {
	c := Classes{"bar": false}
	if c.ToAttr()["class"] != "" {
		t.Errorf("empty class attr should be empty")
	}
}

// --- Utility CSS ----------------------------------------------------------

func TestUtilityCSSEmitsVarRefs(t *testing.T) {
	cases := []struct{ class, want string }{
		{"p-md", "padding: var(--spacing-md);"},
		{"text-primary", "color: var(--color-primary);"},
		{"bg-success", "background-color: var(--color-success);"},
		{"rounded-lg", "border-radius: var(--radii-lg);"},
		{"gap-md", "gap: var(--spacing-md);"},
		{"text-base", "font-size: var(--text-base);"},
	}
	th := DefaultTheme()
	for _, c := range cases {
		got := GenerateUtilityCSS([]string{c.class}, th)
		if !strings.Contains(got, c.want) {
			t.Errorf("class %q → want %q in %q", c.class, c.want, got)
		}
	}
}

func TestUtilityCSSDisplay(t *testing.T) {
	cases := map[string]string{
		"flex":   "display: flex;",
		"grid":   "display: grid;",
		"block":  "display: block;",
		"hidden": "display: none;",
	}
	th := DefaultTheme()
	for class, want := range cases {
		got := GenerateUtilityCSS([]string{class}, th)
		if !strings.Contains(got, want) {
			t.Errorf("class %q → want %q, got %q", class, want, got)
		}
	}
}

// --- Extract ---------------------------------------------------------------

func TestExtractFromHTML(t *testing.T) {
	e := NewCSSExtractor(DefaultTheme())
	html := `<div class="foo bar"><span class="bar baz">x</span></div>`
	got := e.ExtractFromHTML(html)
	want := []string{"bar", "baz", "foo"}
	if len(got) != len(want) {
		t.Fatalf("Extract = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Extract[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// --- StyleSheet integration -----------------------------------------------

func TestStyleSheetSetEmitsVarRefs(t *testing.T) {
	ss := NewStyleSheet(DefaultTheme())
	ss.Rule(".x").Set("color", "{colors.primary}", "padding", "{spacing.md}").End()
	got := ss.CSS()
	if !strings.Contains(got, "color: var(--color-primary)") {
		t.Errorf("Set should emit var ref: %q", got)
	}
	if !strings.Contains(got, "padding: var(--spacing-md)") {
		t.Errorf("Set should emit var ref for spacing: %q", got)
	}
}

// --- Reflection-derived token names ---------------------------------------

func TestThemeAutoFillsTokenNames(t *testing.T) {
	// Authors should be able to declare typed tokens with just a
	// Value, the Name auto-derives from the struct-field path in
	// kebab-case. AutoFillNames walks the theme and assigns names
	// to any token with an empty Name. After autofill, the theme
	// passes Validate.
	th := Theme{
		Colors: ColorSet{
			Primary:   Color{Value: "#FF0000"}, // no Name; should fill "primary"
			Text:      Color{Value: "#000000"}, // → "text"
			PrimaryFg: Color{Value: "#FFFFFF"}, // → "primary-fg" (kebab from CamelCase)
		},
	}
	AutoFillNames(&th)
	if got := th.Colors.Primary.Name; got != "primary" {
		t.Errorf("Primary.Name = %q, want %q", got, "primary")
	}
	if got := th.Colors.PrimaryFg.Name; got != "primary-fg" {
		t.Errorf("PrimaryFg.Name = %q, want %q (kebab-case from CamelCase)", got, "primary-fg")
	}
}

func TestThemeAutoFillPreservesExplicitNames(t *testing.T) {
	// If the author specified Name explicitly (legacy or for an
	// override that needs a non-canonical CSS var name), autofill
	// must NOT clobber it.
	th := Theme{
		Colors: ColorSet{
			Primary: Color{Name: "brand-blue", Value: "#0000FF"},
		},
	}
	AutoFillNames(&th)
	if got := th.Colors.Primary.Name; got != "brand-blue" {
		t.Errorf("explicit Name overwritten: got %q", got)
	}
}

// --- WCAG contrast (DefaultTheme) ----------------------------------------

func TestDefaultTheme_WCAGContrast(t *testing.T) {
	// Token pairs that absolutely must hit WCAG AA (4.5:1 for body
	// text, 3:1 for large text / non-text). These are the live
	// failure modes from the UX review: small body text in
	// text-subtle was 2.56:1; status-colored backgrounds were 2.9:1.
	// The ratio comes from the shared contrast.go implementation —
	// the same one Theme.Validate's ink-pair guard calls — so this
	// table cannot pass while the guard computes something else.
	cases := []struct {
		name    string
		fg, bg  string
		minPair float64
		why     string
	}{
		{"text-on-surface", DefaultTheme().Colors.Text.Value, DefaultTheme().Colors.Surface.Value, 4.5, "body text"},
		{"text-muted-on-surface", DefaultTheme().Colors.TextMuted.Value, DefaultTheme().Colors.Surface.Value, 4.5, "secondary body text"},
		{"text-subtle-on-surface", DefaultTheme().Colors.TextSubtle.Value, DefaultTheme().Colors.Surface.Value, 4.5, "subtle text (was 2.56:1)"},
		{"primary-fg-on-primary", DefaultTheme().Colors.PrimaryFg.Value, DefaultTheme().Colors.Primary.Value, 4.5, "button label on primary bg"},
		{"danger-fg-on-danger", DefaultTheme().Colors.DangerFg.Value, DefaultTheme().Colors.Danger.Value, 4.5, "button label on danger bg"},
		{"white-on-warning", "#FFFFFF", DefaultTheme().Colors.Warning.Value, 4.5, "white label on warning solid (was 2.94:1)"},
		{"white-on-success", "#FFFFFF", DefaultTheme().Colors.Success.Value, 4.5, "white label on success solid (was 3.30:1)"},
		{"white-on-danger", "#FFFFFF", DefaultTheme().Colors.Danger.Value, 4.5, "white label on danger solid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := contrastRatio(c.fg, c.bg)
			if !ok {
				t.Fatalf("%s: contrastRatio(%q, %q) could not parse — the default theme is all hex, so this is a helper regression", c.name, c.fg, c.bg)
			}
			if got < c.minPair {
				t.Errorf("%s (%s on %s) contrast = %.2f:1, want >= %.1f:1 (%s)",
					c.name, c.fg, c.bg, got, c.minPair, c.why)
			}
		})
	}
}

// --- Theme.Validate ------------------------------------------------------

func TestThemeValidate_AcceptsDefault(t *testing.T) {
	if err := DefaultTheme().Validate(); err != nil {
		t.Errorf("DefaultTheme should pass validation: %v", err)
	}
}

func TestThemeValidate_RejectsZeroValue(t *testing.T) {
	err := Theme{}.Validate()
	if err == nil {
		t.Fatal("zero-valued Theme should fail validation")
	}
	if !strings.Contains(err.Error(), "Primary") {
		t.Errorf("error should name the missing field (e.g. Primary): %v", err)
	}
}

func TestThemeValidate_RejectsMissingName(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Value: "#FF0000"} // missing Name
	err := th.Validate()
	if err == nil {
		t.Fatal("Color with empty Name should fail validation")
	}
}

// The ink-pair guard: a filled button paints primary × primary-fg and
// danger × danger-fg, and the token system's guarantee is only about
// THOSE pairs. Borrowing the primary's ink for the danger fill was how
// light-primary hosts shipped unreadable danger buttons (axe failures
// out of the box), so a hex pair below 4.5:1 is a boot-time error.
func TestThemeValidate_RefusesLowContrastInkPairs(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*Theme)
		wantErr string
	}{
		{
			"danger pair 4.0:1",
			func(th *Theme) {
				th.Colors.Danger = Color{Name: "danger", Value: "#FF0000"}
				th.Colors.DangerFg = Color{Name: "danger-fg", Value: "#FFFFFF"}
			},
			"danger × danger-fg",
		},
		{
			"primary pair 4.0:1",
			func(th *Theme) {
				th.Colors.Primary = Color{Name: "primary", Value: "#FF0000"}
				th.Colors.PrimaryFg = Color{Name: "primary-fg", Value: "#FFFFFF"}
			},
			"primary × primary-fg",
		},
		{
			// #RGB parses the same as its long form: the guard must
			// not silently skip short hex.
			"danger pair short hex",
			func(th *Theme) {
				th.Colors.Danger = Color{Name: "danger", Value: "#F00"}
				th.Colors.DangerFg = Color{Name: "danger-fg", Value: "#FFF"}
			},
			"danger × danger-fg",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			th := DefaultTheme()
			c.setup(&th)
			err := th.Validate()
			if err == nil {
				t.Fatalf("%s should fail validation (contrast below 4.5:1)", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "4.5:1") {
				t.Errorf("error should name the pair (%s) and the 4.5:1 floor, got: %v", c.wantErr, err)
			}
		})
	}
}

// The guard skips what it cannot compute exactly: oklch(), var()
// references and named colours are legitimate token values a Go
// validator has no business approximating. Skipping is a refusal to
// guess, not an approval.
func TestThemeValidate_SkipsUnparseableInkPairs(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Theme)
	}{
		{
			"oklch danger (the amber-site shape)",
			func(th *Theme) {
				th.Colors.Danger = Color{Name: "danger", Value: "oklch(0.72 0.16 25)"}
			},
		},
		{
			"var() primary-fg",
			func(th *Theme) {
				th.Colors.PrimaryFg = Color{Name: "primary-fg", Value: "var(--color-text)"}
			},
		},
		{
			"named danger-fg",
			func(th *Theme) {
				th.Colors.DangerFg = Color{Name: "danger-fg", Value: "white"}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			th := DefaultTheme()
			c.setup(&th)
			if err := th.Validate(); err != nil {
				t.Errorf("%s: Validate refused an unparseable pair it must skip: %v", c.name, err)
			}
		})
	}
}

// Short-form hex is a legitimate token spelling (#FFF, #F00): the
// contrast arithmetic must read it as CSS does — each digit doubled —
// not as a bare nibble divided by 255, which read #FFF as near-black
// and a valid #000 × #FFF pair as ~1.1:1.
func TestContrastRatio_ShortFormHex(t *testing.T) {
	r, ok := contrastRatio("#FFF", "#000")
	if !ok || math.Abs(r-21.0) > 0.01 {
		t.Errorf("#FFF on #000 = %.2f:1 (ok=%v), want 21:1 — the short pair must expand like CSS", r, ok)
	}
	short, okS := contrastRatio("#F00", "#FFF")
	long, okL := contrastRatio("#FF0000", "#FFFFFF")
	if !okS || !okL || math.Abs(short-long) > 1e-9 {
		t.Errorf("#F00 on #FFF (%.4f, ok=%v) must equal #FF0000 on #FFFFFF (%.4f, ok=%v)", short, okS, long, okL)
	}
}

// A short-hex pair that clears AA validates: the guard must judge the
// expanded value, not refuse (or wave through) the spelling.
func TestThemeValidate_ShortHexPairPassingAAClears(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Name: "primary", Value: "#888"}      // #888888, 5.9:1 under black ink
	th.Colors.PrimaryFg = Color{Name: "primary-fg", Value: "#000"} // #000000
	if err := th.Validate(); err != nil {
		t.Fatalf("short-hex primary pair clearing AA failed Validate: %v", err)
	}
}

// The ink-pair guard's dark half: a non-empty DarkColors map
// re-declares the same variables, so the dark pairs are judged by the
// same 4.5:1 floor — resolved key by key, an absent key falling back to
// the light token that actually paints when the map does not override it.
func TestThemeValidate_RefusesLowContrastDarkPairs(t *testing.T) {
	cases := []struct {
		name    string
		dark    map[string]string
		wantErr string
	}{
		{
			// White ink on the light-red dark danger: 2.77:1.
			"dark danger with light ink",
			map[string]string{"danger": "#F87171", "danger-fg": "#FFFFFF"},
			"Theme.DarkColors: danger × danger-fg",
		},
		{
			// The map omits danger-fg, so the LIGHT ink (#FFFFFF)
			// is what paints on the dark danger — judged, not waved
			// through as "absent".
			"dark danger, danger-fg falls back to the light ink",
			map[string]string{"danger": "#F87171"},
			"Theme.DarkColors: danger × danger-fg",
		},
		{
			"dark primary with light ink",
			map[string]string{"primary": "#5EEAD4", "primary-fg": "#FFFFFF"},
			"Theme.DarkColors: primary × primary-fg",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			th := DefaultTheme()
			th.DarkColors = c.dark
			err := th.Validate()
			if err == nil {
				t.Fatalf("%s should fail validation (dark contrast below 4.5:1)", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "4.5:1") {
				t.Errorf("error should name the dark pair (%s) and the 4.5:1 floor, got: %v", c.wantErr, err)
			}
		})
	}
}

// The dark half skips what it cannot compute exactly, exactly like the
// light half — and a dark palette whose pairs clear AA (the framework
// base dark values) validates.
func TestThemeValidate_DarkPairsSkippedAndPassing(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = map[string]string{"primary": "oklch(0.82 0.155 78)"}
	if err := th.Validate(); err != nil {
		t.Errorf("oklch dark primary must be skipped, not approximated: %v", err)
	}
	// The framework dark palette's own pairs: 8.90:1 and 6.41:1.
	th.DarkColors = map[string]string{
		"primary":    "#A5B4FC",
		"primary-fg": "#111827",
		"danger":     "#F87171",
		"danger-fg":  "#111827",
	}
	if err := th.Validate(); err != nil {
		t.Errorf("contrast-safe dark palette failed Validate: %v", err)
	}
	// An empty dark map is light-only: nothing to judge, and the light
	// pairs already ran.
	th.DarkColors = nil
	if err := th.Validate(); err != nil {
		t.Errorf("light-only theme failed Validate: %v", err)
	}
}

func TestThemeValidate_RejectsMissingValue(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Name: "primary"} // missing Value
	err := th.Validate()
	if err == nil {
		t.Fatal("Color with empty Value should fail validation")
	}
}

func TestThemeValidate_RejectsZeroNumericValues(t *testing.T) {
	// A populated Name but a zero Value on numeric token types
	// silently emits `--spacing-md: 0px` etc. and breaks layout,
	// exactly what Validate was added to prevent.
	cases := []struct {
		name  string
		setup func(*Theme)
	}{
		{"Spacing.MD=0", func(t *Theme) { t.Spacing.MD = Spacing{Name: "md", Value: 0} }},
		{"Radii.MD=0", func(t *Theme) { t.Radii.MD = Radius{Name: "md", Value: 0} }},
		{"Breakpoints.MD=0", func(t *Theme) { t.Breakpoints.MD = Breakpoint{Name: "md", Value: 0} }},
		{"ZIndex.Modal=0", func(t *Theme) { t.ZIndex.Modal = ZIndexValue{Name: "modal", Value: 0} }},
		{"Durations.Normal=0", func(t *Theme) { t.Durations.Normal = Duration{Name: "normal", Value: 0} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			th := DefaultTheme()
			c.setup(&th)
			if err := th.Validate(); err == nil {
				t.Errorf("%s should fail validation (zero Value)", c.name)
			}
		})
	}
}

// "none" / "0" radii are a real use case (sharp corners).
// Validate must accept Radius{Name: "none", Value: 0} when the Name
// is "none", the special sentinel that documents intent.
func TestThemeValidate_AllowsNoneRadius(t *testing.T) {
	th := DefaultTheme()
	// none already exists; just confirm the default theme passes.
	if err := th.Validate(); err != nil {
		t.Errorf("default theme should pass: %v", err)
	}
}
