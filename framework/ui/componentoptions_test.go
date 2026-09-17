package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The real compiler, driven through the emitters the way a host drives
// them: a theme's options reach CSS through CSSCustomProperties
// (:root) and ThemeOverrideCSS (every scope block). Package init
// registered the compiler, so these tests exercise the wiring a real
// app gets.

func rootOptionCSS(o theme.ComponentOptions) string {
	return theme.Default(theme.Overrides{Components: o}).CSSCustomProperties()
}

func TestCompilerEmitsDensityVariables(t *testing.T) {
	for _, tc := range []struct {
		density  theme.Density
		controlH string
		gap      string
	}{
		{theme.Comfortable, "44px", "var(--spacing-md)"},
		{theme.Compact, "36px", "var(--spacing-sm)"},
	} {
		css := rootOptionCSS(theme.ComponentOptions{Density: tc.density})
		if !strings.Contains(css, "--fui-density-control-h: "+tc.controlH+";") {
			t.Errorf("density %v: control height missing (want %s)", tc.density, tc.controlH)
		}
		if !strings.Contains(css, "--fui-density-gap: "+tc.gap+";") {
			t.Errorf("density %v: gap missing (want %s)", tc.density, tc.gap)
		}
	}
}

func TestCompilerEmitsRadiusVariables(t *testing.T) {
	for _, tc := range []struct {
		radius theme.ButtonRadius
		value  string
	}{
		{theme.Round, "var(--radii-md)"},
		{theme.Square, "0"},
		{theme.Pill, "9999px"},
	} {
		css := rootOptionCSS(theme.ComponentOptions{Button: theme.ButtonOptions{Radius: tc.radius}})
		if !strings.Contains(css, "--fui-button-radius: "+tc.value+";") {
			t.Errorf("radius %v: variable missing (want %s)", tc.radius, tc.value)
		}
	}
}

func TestCompilerEmitsTreatmentVariables(t *testing.T) {
	for _, tc := range []struct {
		treatment theme.ButtonTreatment
		bg, fg    string
		border    string
	}{
		{theme.Filled, "var(--color-primary)", "var(--color-primary-fg)", "transparent"},
		{theme.Outline, "transparent", "var(--color-primary)", "var(--color-primary)"},
		{theme.Soft, "color-mix(in srgb, var(--color-primary) 15%, transparent)", "var(--color-primary)", "transparent"},
	} {
		css := rootOptionCSS(theme.ComponentOptions{Button: theme.ButtonOptions{Treatment: tc.treatment}})
		for _, variant := range []struct{ name, colour string }{
			{"primary", "var(--color-primary)"},
			{"danger", "var(--color-danger)"},
		} {
			bg := strings.ReplaceAll(tc.bg, "var(--color-primary)", variant.colour)
			fg := strings.ReplaceAll(tc.fg, "var(--color-primary)", variant.colour)
			border := strings.ReplaceAll(tc.border, "var(--color-primary)", variant.colour)
			for name, want := range map[string]string{
				"--fui-button-" + variant.name + "-bg":     bg,
				"--fui-button-" + variant.name + "-fg":     fg,
				"--fui-button-" + variant.name + "-border": border,
			} {
				if !strings.Contains(css, name+": "+want+";") {
					t.Errorf("treatment %v: %s missing (want %s)", tc.treatment, name, want)
				}
			}
		}
	}
	// The un-prefixed trio is gone: a rule reading --fui-button-bg
	// would fall back to nothing and draw an uncoloured button.
	def := rootOptionCSS(theme.DefaultOptions)
	for _, gone := range []string{"--fui-button-bg:", "--fui-button-fg:", "--fui-button-border:"} {
		if strings.Contains(def, gone) {
			t.Errorf("the un-prefixed trio %s still ships", gone)
		}
	}
}

// A complete option set declares every variable at :root: this is the
// vocabulary the component stylesheets will consume, all of it, in one
// block.
func TestCompilerEmitsTheCompleteSetAtRoot(t *testing.T) {
	css := rootOptionCSS(theme.DefaultOptions)
	for _, name := range []string{
		"--fui-density-control-h", "--fui-density-gap",
		"--fui-button-radius",
		"--fui-button-primary-bg", "--fui-button-primary-fg", "--fui-button-primary-border",
		"--fui-button-danger-bg", "--fui-button-danger-fg", "--fui-button-danger-border",
	} {
		if !strings.Contains(css, name+":") {
			t.Errorf("complete option set missing %s", name)
		}
	}
}

func TestCompilerEmitsOptionsInsideScopeBlocks(t *testing.T) {
	th := theme.Default(theme.Overrides{Components: theme.ComponentOptions{
		Density: theme.Compact,
		Button:  theme.ButtonOptions{Treatment: theme.Outline, Radius: theme.Square},
	}})
	ref := style.RegisterThemeOverride(th)
	css := style.ThemeOverrideCSS(ref.Hash(), th)
	// Light and both dark scope blocks each re-declare the options:
	// every boundary is where the var() references must compute.
	for _, probe := range []struct{ block, opener string }{
		{"light", ".fui-theme-" + ref.Hash() + " {\n"},
		{"explicit dark", "\n[data-color-scheme=\"dark\"] .fui-theme-" + ref.Hash() + " {\n"},
		{"media dark", "  :root:not([data-color-scheme=\"light\"]) .fui-theme-" + ref.Hash() + " {\n"},
	} {
		i := strings.Index(css, probe.opener)
		if i < 0 {
			t.Fatalf("%s scope block missing its opener %q in:\n%s", probe.block, probe.opener, css)
		}
		end := strings.Index(css[i:], "\n}")
		if end < 0 {
			t.Fatalf("%s scope block missing its closer in:\n%s", probe.block, css)
		}
		body := css[i : i+end]
		for _, want := range []string{
			"--fui-density-control-h: 36px;",
			"--fui-button-radius: 0;",
			"--fui-button-primary-bg: transparent;",
			"--fui-button-primary-border: var(--color-primary);",
			"--fui-button-danger-bg: transparent;",
			"--fui-button-danger-border: var(--color-danger);",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s scope block missing %s:\n%s", probe.block, want, body)
			}
		}
	}
}

// Unknown option keys and values panic at emit, not silently drop:
// the map is data that crossed a boundary.
func TestCompilerPanicsOnNonMemberValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a non-member option value emitted without a panic")
		}
	}()
	th := theme.Default()
	th.Components["density"] = "cozy"
	_ = th.CSSCustomProperties()
}

// Unknown option KEYS panic at emit too, not silently drop: the map
// is data that crossed a boundary.
func TestCompilerPanicsOnUnknownOption(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unknown option key emitted without a panic")
		}
	}()
	th := theme.Default()
	th.Components["banana.split"] = "ripe"
	_ = th.CSSCustomProperties()
}

// With the real compiler registered (this package's init), Validate
// runs it, so an unknown vocabulary fails at boot instead of
// detonating as a panic inside the first request. The value here is
// grammar-clean — "cozy" is one lowercase word — so only the
// vocabulary check can catch it.
func TestValidateCatchesUnknownVocabularyAtBoot(t *testing.T) {
	th := theme.Default()
	th.Components["density"] = "cozy"
	err := th.Validate()
	if err == nil || !strings.Contains(err.Error(), "density") {
		t.Fatalf("Validate must name the unknown option at boot; got %v", err)
	}
}

// The theme's own hash separates option sets under the REAL compiler,
// so a per-theme cache or URL keyed on ThemeHash never serves one
// option set's CSS under another's key.
func TestThemeHashSeparatesOptionsUnderRealCompiler(t *testing.T) {
	filled := theme.Default()
	outline := theme.Default(theme.Overrides{Components: theme.ComponentOptions{
		Button: theme.ButtonOptions{Treatment: theme.Outline},
	}})
	if style.ThemeHash(filled) == style.ThemeHash(outline) {
		t.Error("equal tokens with different treatments hash identically")
	}
}
