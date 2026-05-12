package style

import (
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
	for i := 0; i < 50; i++ {
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
	// silently emits `--spacing-md: 0px` etc. and breaks layout —
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
// is "none" — the special sentinel that documents intent.
func TestThemeValidate_AllowsNoneRadius(t *testing.T) {
	th := DefaultTheme()
	// none already exists; just confirm the default theme passes.
	if err := th.Validate(); err != nil {
		t.Errorf("default theme should pass: %v", err)
	}
}

// --- RouteGraph preload (deprecated but still works) ----------------------

func TestRouteGraphPreloadManifest(t *testing.T) {
	g := NewRouteGraph()
	g.AddRoute("/", "home.css", []string{"/about"})
	g.AddRoute("/about", "about.css", nil)
	m := g.PreloadManifest()
	if m["/"].CSS != "home.css" {
		t.Errorf("home CSS chunk wrong: %v", m["/"])
	}
	if len(m["/"].Preload) != 1 || m["/"].Preload[0] != "about.css" {
		t.Errorf("home preloads wrong: %v", m["/"].Preload)
	}
}
