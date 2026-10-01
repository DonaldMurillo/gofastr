package style_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// An app's own tokens are typed like the built-ins: the value's type
// picks the prefix (style.Color → --color-*, style.Size → --size-*),
// and they reach every place a built-in does.

type brandTokens struct {
	BrandGlow style.Color
	HeroGap   style.Size
	Display   style.FontWeight
}

func brand() brandTokens {
	return brandTokens{
		BrandGlow: style.Color{Value: "#FF7A00"},
		HeroGap:   style.Size{Value: "clamp(2rem, 6vw, 5rem)"},
		Display:   style.FontWeight{Value: 800},
	}
}

func TestExtendEmitsAppTokens(t *testing.T) {
	css := style.DefaultTheme().Extend(brand()).CSSCustomProperties()
	for _, decl := range []string{
		"--color-brand-glow: #FF7A00;",
		"--size-hero-gap: clamp(2rem, 6vw, 5rem);",
		"--font-weight-display: 800;",
		"--color-primary: #4F46E5;",
	} {
		if !strings.Contains(css, decl) {
			t.Errorf(":root is missing %q", decl)
		}
	}
}

func TestExtendAcceptsPointer(t *testing.T) {
	b := brand()
	css := style.DefaultTheme().Extend(&b).CSSCustomProperties()
	if !strings.Contains(css, "--color-brand-glow: #FF7A00;") {
		t.Error("a pointer to the token struct should extend like the value")
	}
}

func TestExtendCopiesItsInputs(t *testing.T) {
	base := style.DefaultTheme()
	b := brand()
	ext := base.Extend(&b)
	b.BrandGlow.Value = "#000000"
	if !strings.Contains(ext.CSSCustomProperties(), "--color-brand-glow: #FF7A00;") {
		t.Error("changing the caller's struct after Extend changed the theme")
	}
	if strings.Contains(base.CSSCustomProperties(), "brand-glow") {
		t.Error("Extend changed its receiver")
	}
}

type glowTokens struct{ Glow style.Color }

type nestedBrandTokens struct {
	Accent *glowTokens
}

// A nested token struct held by pointer is copied too: an override on
// one theme must not write through a pointer another theme shares.
func TestApplyTokensNestedPointerStaysIsolated(t *testing.T) {
	base := style.DefaultTheme().Extend(nestedBrandTokens{Accent: &glowTokens{Glow: style.Color{Value: "#111111"}}})
	if _, err := style.ApplyTokens(base, map[string]string{"color-glow": "#222222"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base.CSSCustomProperties(), "--color-glow: #111111;") {
		t.Error("ApplyTokens on a copy changed the base theme's nested token")
	}
}

func TestExtendCopiesNestedPointers(t *testing.T) {
	in := nestedBrandTokens{Accent: &glowTokens{Glow: style.Color{Value: "#111111"}}}
	ext := style.DefaultTheme().Extend(in)
	in.Accent.Glow.Value = "#000000"
	if !strings.Contains(ext.CSSCustomProperties(), "--color-glow: #111111;") {
		t.Error("changing the caller's nested struct after Extend changed the theme")
	}
}

func TestExtendCollisionWithBuiltIn(t *testing.T) {
	msg := panicText(t, func() {
		style.DefaultTheme().Extend(struct{ Primary style.Color }{style.Color{Value: "#000"}})
	})
	for _, want := range []string{"--color-primary", "Theme.Colors.Primary", "Primary"} {
		if !strings.Contains(msg, want) {
			t.Errorf("panic %q should name %q", msg, want)
		}
	}
}

func TestExtendCollisionAcrossExtensions(t *testing.T) {
	msg := panicText(t, func() {
		style.DefaultTheme().Extend(brand()).Extend(struct{ HeroGap style.Size }{style.Size{Value: "1rem"}})
	})
	if !strings.Contains(msg, "--size-hero-gap") || !strings.Contains(msg, "brandTokens") {
		t.Errorf("panic %q should name the key and the first owner", msg)
	}
}

func TestExtendRefusesNonTokenInput(t *testing.T) {
	for name, in := range map[string]any{
		"string":    "brand",
		"no tokens": struct{ N int }{1},
		"nil":       nil,
	} {
		if msg := panicText(t, func() { style.DefaultTheme().Extend(in) }); msg == "" {
			t.Errorf("%s: Extend accepted it", name)
		}
	}
}

func TestExtendJoinsTheTokenMap(t *testing.T) {
	base := style.DefaultTheme().Extend(brand())
	tokens := style.ThemeToTokens(base)
	if tokens["size-hero-gap"] != "clamp(2rem, 6vw, 5rem)" || tokens["font-weight-display"] != "800" {
		t.Fatalf("ThemeToTokens is missing the app tokens: %v", tokens["size-hero-gap"])
	}
	got, err := style.ApplyTokens(base, map[string]string{
		"size-hero-gap":         "4rem",
		"dark.color-brand-glow": "#FFB066",
	})
	if err != nil {
		t.Fatal(err)
	}
	css := got.CSSCustomProperties()
	if !strings.Contains(css, "--size-hero-gap: 4rem;") || !strings.Contains(css, "--color-brand-glow: #FFB066;") {
		t.Errorf("ApplyTokens did not reach the app tokens:\n%s", css)
	}
	if !strings.Contains(base.CSSCustomProperties(), "--size-hero-gap: clamp(2rem, 6vw, 5rem);") {
		t.Error("ApplyTokens wrote through to the base theme's extension")
	}
	if _, err := style.ApplyTokens(base, map[string]string{"size-hero-gap": "huge"}); err == nil {
		t.Error("an app Size token accepted a bare word")
	}
}

func TestExtendChangesThemeHash(t *testing.T) {
	plain := style.DefaultTheme()
	a := plain.Extend(brand())
	b2 := brand()
	b2.HeroGap.Value = "3rem"
	b := plain.Extend(b2)
	if style.ThemeHash(plain) == style.ThemeHash(a) || style.ThemeHash(a) == style.ThemeHash(b) {
		t.Error("the hash should change with the app tokens and their values")
	}
}

func TestExtendValidates(t *testing.T) {
	if err := style.DefaultTheme().Extend(brand()).Validate(); err != nil {
		t.Fatalf("an extended default theme should validate: %v", err)
	}
	bad := brand()
	bad.HeroGap.Value = ""
	th := style.DefaultTheme().Extend(bad)
	if err := th.Validate(); err == nil || !strings.Contains(err.Error(), "HeroGap") {
		t.Errorf("Validate should refuse the empty app token by field, got %v", err)
	}
}

// A hand-assembled Extensions slice skips Extend's checks; Validate is
// the backstop that refuses a key two tokens would both emit.
func TestValidateRefusesDuplicateKeys(t *testing.T) {
	th := style.DefaultTheme()
	th.Extensions = []any{&struct{ Primary style.Color }{style.Color{Name: "primary", Value: "#000"}}}
	if err := th.Validate(); err == nil || !strings.Contains(err.Error(), "--color-primary") {
		t.Errorf("Validate should refuse the duplicate key, got %v", err)
	}
}

func TestExtendReachesScopedThemes(t *testing.T) {
	css := style.ThemeOverrideCSS("x", style.DefaultTheme().Extend(brand()))
	if !strings.Contains(css, "--color-brand-glow: #FF7A00;") {
		t.Error("a themed scope should re-declare the app tokens too")
	}
}

func panicText(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toString(r), "\n", " "), "  ", " "))
		}
	}()
	fn()
	return ""
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	}
	return "panic"
}
