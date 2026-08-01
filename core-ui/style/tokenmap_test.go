package style_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// This file tests the ThemeToTokens / ApplyTokens contract as properties,
// not as a case-by-case matrix. The shape follows the brief and
// cmd/gofastr/blueprint_emitter_injection_test.go: a property is one
// sentence, a payload table drives it across every surface, and the
// assertion is "the payload never reaches the output it is emitted into".
//
// Properties under test:
//
//   - Round-trip: ApplyTokens(t, ThemeToTokens(t)) preserves ThemeHash(t)
//     (identical emitted CSS) — over style.DefaultTheme() AND the canonical
//     framework theme (which carries a populated DarkColors map).
//   - Determinism: ThemeToTokens is a pure function.
//   - Type validation: every token type accepts a valid value and rejects
//     a malformed one.
//   - Injection: no declaration-breaking payload may reach CSSCustomProperties.
//   - Unknown keys are rejected (fail closed, no probing).
//   - Applying a subset leaves every other token untouched.

// --- Round-trip: ThemeToTokens ∘ ApplyTokens preserves ThemeHash ---------

func TestApplyTokens_RoundTrip_DefaultTheme(t *testing.T) {
	base := style.DefaultTheme()
	assertRoundTripHash(t, base)
}

func TestApplyTokens_RoundTrip_FrameworkTheme(t *testing.T) {
	// The canonical framework theme carries a populated DarkColors map;
	// the round-trip must preserve it (the dark.* keys) exactly, or the
	// dark-scheme block of CSSCustomProperties drifts and the hash changes.
	base := uitheme.Default()
	assertRoundTripHash(t, base)
}

func assertRoundTripHash(t *testing.T, base style.Theme) {
	t.Helper()
	want := style.ThemeHash(base)
	tokens := style.ThemeToTokens(base)
	if len(tokens) == 0 {
		t.Fatalf("ThemeToTokens returned an empty map — the walk found nothing")
	}
	reapplied, err := style.ApplyTokens(base, tokens)
	if err != nil {
		t.Fatalf("round-trip ApplyTokens failed: %v", err)
	}
	if got := style.ThemeHash(reapplied); got != want {
		t.Errorf("round-trip changed the hash (want identical emitted CSS):\n  base  = %s\n  round = %s\n--- base CSS ---\n%s\n--- round CSS ---\n%s",
			want, got, base.CSSCustomProperties(), reapplied.CSSCustomProperties())
	}
}

func TestThemeToTokens_Deterministic(t *testing.T) {
	base := uitheme.Default()
	first := style.ThemeToTokens(base)
	for i := range 30 {
		if !tokenMapsEqual(first, style.ThemeToTokens(base)) {
			t.Fatalf("ThemeToTokens is non-deterministic at iteration %d", i)
		}
	}
}

// --- Key scheme: keys match the CSS emitter, dark.* is distinct -----------

func TestThemeToTokens_KeysMatchCSSVarNames(t *testing.T) {
	base := style.DefaultTheme()
	tokens := style.ThemeToTokens(base)
	// Spot-check one key per category — the key is the CSS custom-property
	// identifier without the leading "--", exactly what the :root block emits.
	for _, key := range []string{
		"color-primary", "color-code-surface",
		"spacing-md", "radii-lg", "font-body", "breakpoint-md",
		"shadow-md", "z-modal", "duration-fast", "easing-spring",
		"text-base", "tk-kw", "spacing-touch-target",
	} {
		if _, ok := tokens[key]; !ok {
			t.Errorf("expected key %q in token map", key)
		}
	}
	// The value must be byte-identical to what the :root block emits.
	if v := tokens["color-primary"]; v != base.Colors.Primary.Value {
		t.Errorf("color-primary value = %q, want CSS value %q", v, base.Colors.Primary.Value)
	}
	if v := tokens["spacing-md"]; v != "8px" {
		t.Errorf("spacing-md value = %q, want %q", v, "8px")
	}
	if v := tokens["z-modal"]; v != "300" {
		t.Errorf("z-modal value = %q, want %q (z-index is unitless)", v, "300")
	}
}

func TestThemeToTokens_DarkColorsKeyScheme(t *testing.T) {
	// DarkColors/DarkCode are flattened under "dark." so they stay distinct
	// from their light counterparts (which share the --color- name but live
	// in a different selector scope).
	base := uitheme.Default()
	tokens := style.ThemeToTokens(base)
	if v, ok := tokens["dark.color-primary"]; !ok || v != base.DarkColors["primary"] {
		t.Errorf("dark.color-primary = %q (%v), want %q",
			v, ok, base.DarkColors["primary"])
	}
	// The light value and the dark value for the SAME token name must NOT
	// collapse onto one key (that would lose information on round-trip).
	if tokens["color-primary"] == tokens["dark.color-primary"] {
		t.Errorf("light and dark primary collapsed to one value %q — dark. prefix not distinct",
			tokens["color-primary"])
	}
	// No typed-token key contains a "." — the dark. prefix is unambiguous.
	for k := range tokens {
		if strings.HasPrefix(k, "dark.") {
			continue
		}
		if strings.Contains(k, ".") {
			t.Errorf("non-dark key %q contains '.' — would collide with the dark. scheme", k)
		}
	}
}

// --- Type validation: valid accepted, malformed rejected -------------------

func TestApplyTokens_TypeValidation(t *testing.T) {
	base := style.DefaultTheme()
	cases := []struct {
		key     string
		valid   string
		invalid string
		why     string
	}{
		{"color-primary", "#00FF00", "not-a-color", "bare word is not a color"},
		{"color-primary", "oklch(0.7 0.15 145)", "#12", "wrong hex length"},
		{"color-accent", "var(--color-primary)", "red green", "two tokens"},
		{"spacing-md", "10px", "abc", "non-numeric"},
		{"spacing-md", "0px", "8", "missing px suffix"},
		{"radii-lg", "8px", "8px;", "decl breaker"},
		{"breakpoint-md", "768px", "0px", "must be > 0"},
		{"breakpoint-md", "768px", "768", "missing px"},
		{"font-body", "'Inter', sans-serif", "font;--evil:1", "decl breaker"},
		{"shadow-md", "0 4px 6px rgba(0,0,0,0.1)", "0;}", "decl breaker"},
		{"z-modal", "100", "100px", "px not allowed on z-index"},
		{"z-modal", "0", "abc", "non-numeric"},
		{"duration-fast", "150ms", "fast", "not a duration"},
		{"duration-fast", "150ms", "0ms", "must be > 0"},
		{"easing-spring", "cubic-bezier(0.34, 1.56, 0.64, 1)", "spring;}", "decl breaker"},
		{"text-base", "1rem", "1rem;}", "decl breaker"},
		{"tk-kw", "#C792EA", "#C792EA;}", "decl breaker"},
		{"tk-pn", "var(--color-code-text)", "var(--x);--y:1", "decl breaker"},
	}
	for _, c := range cases {
		if _, err := style.ApplyTokens(base, map[string]string{c.key: c.valid}); err != nil {
			t.Errorf("ApplyTokens(%q=%q): unexpected error: %v", c.key, c.valid, err)
		}
		if _, err := style.ApplyTokens(base, map[string]string{c.key: c.invalid}); err == nil {
			t.Errorf("ApplyTokens(%q=%q): expected error (%s), got none", c.key, c.invalid, c.why)
		}
	}
}

// --- Color grammar: the bounded set accepted, the rest rejected -----------

func TestApplyTokens_ColorGrammar(t *testing.T) {
	base := style.DefaultTheme()
	valid := []string{
		"#FFF", "#FFFF", "#FF0000", "#FF0000FF", // hex 3/4/6/8
		"rgb(0, 0, 0)", "rgba(0, 0, 0, 0.5)",
		"hsl(120, 100%, 50%)", "hsla(120, 100%, 50%, 0.25)",
		"oklch(0.7 0.15 145)", "oklab(0.5 -0.1 0.05)",
		"color-mix(in srgb, var(--color-primary) 85%, white)", // nested var()
		"var(--color-primary)",
		"red", "transparent", "currentColor", "REBECCAPURPLE", // named, case-insensitive
	}
	invalid := []string{
		"", "not-a-color", "red more", "#GGG", "#12", "12345",
		"rgb()", "var()", // balanced but empty interior
		"expression(alert(1))", "rgb(0,0,0", "red)",
	}
	for _, v := range valid {
		if _, err := style.ApplyTokens(base, map[string]string{"color-primary": v}); err != nil {
			t.Errorf("color %q: unexpected rejection: %v", v, err)
		}
	}
	for _, v := range invalid {
		if _, err := style.ApplyTokens(base, map[string]string{"color-primary": v}); err == nil {
			t.Errorf("color %q: expected rejection, got none", v)
		}
	}
}

// --- Injection: no declaration-breaking payload reaches CSS ----------------
//
// Modeled on cmd/gofastr/blueprint_emitter_injection_test.go. The property
// is one sentence:
//
//	No theme-token value may escape the CSS declaration it is emitted into.
//
// Every value reaches CSS as `--<key>: <value>;` inside :root or the
// dark-scheme block. A payload carrying one of these sequences adds a new
// declaration (";"), closes the block ("}"), opens one ("{"), or otherwise
// toggles parsing. The sweep drives each payload through every free-form
// token surface and requires ApplyTokens to REJECT it; the payload must
// also never appear in any theme's CSSCustomProperties output.

var cssDeclBreakerPayloads = []string{
	"red; --evil: green",
	"red } body { background: red",
	"red { display: none }",
	"red/* c */",
	"red*/",
	"red<x>",
	`red\27`,
	"red\nbody{display:none}",
	"red\r\nbody{}",
	"url(javascript:alert(1))",
	"url(data:text/html,<script>)",
	"#4F46E5;--evil:1}",
	"red; }</style><script>alert(1)</script>",
}

func TestApplyTokens_RejectsDeclarationBreakers(t *testing.T) {
	base := style.DefaultTheme()
	// Every token surface whose value is a free-form string reaching CSS,
	// plus the dark-scheme overrides (which re-emit the same vars in a
	// different scope). Color carries the bounded grammar as a second gate;
	// the others carry only the declaration-breaker rejection.
	surfaces := []string{
		"color-primary",
		"font-body",
		"shadow-md",
		"easing-spring",
		"text-base",
		"tk-kw",
		"dark.color-primary",
		"dark.tk-kw",
	}
	for _, payload := range cssDeclBreakerPayloads {
		label := strings.NewReplacer("\n", "N", "\r", "R", " ", "_").Replace(payload)
		for _, key := range surfaces {
			t.Run(key+"/"+label, func(t *testing.T) {
				before := base.CSSCustomProperties()
				_, err := style.ApplyTokens(base, map[string]string{key: payload})
				if err == nil {
					t.Fatalf("SECURITY: [injection] ApplyTokens accepted %q=%q — expected rejection", key, payload)
				}
				// ApplyTokens must not mutate base (it deep-copies the dark
				// maps), and the payload must never appear in any theme's CSS.
				after := base.CSSCustomProperties()
				if after != before {
					t.Fatalf("SECURITY: [injection] ApplyTokens mutated the base theme for %q", key)
				}
				if strings.Contains(before, payload) {
					t.Fatalf("SECURITY: [injection] payload %q is present in the base theme CSS", payload)
				}
			})
		}
	}
}

// --- Unknown keys: fail closed, no probing --------------------------------

func TestApplyTokens_UnknownKeyErrors(t *testing.T) {
	base := style.DefaultTheme()
	for _, key := range []string{
		"color-nonexistent", // plausible typo
		"spacing-huge",
		"random-key",
		"dark.color-imaginary", // no light token, no existing dark entry
		"dark.something",       // bad dark.* suffix
		"primary",              // missing category prefix
		"--color-primary",      // caller passed the full CSS var, not the key
		"color-primary: red",   // embedded declaration
	} {
		if _, err := style.ApplyTokens(base, map[string]string{key: "#000000"}); err == nil {
			t.Errorf("ApplyTokens(%q): expected unknown-key error, got none", key)
		}
	}
}

// --- Subset application leaves every other token untouched -----------------

func TestApplyTokens_SubsetLeavesOthersUntouched(t *testing.T) {
	base := style.DefaultTheme()
	primaryWas := base.Colors.Primary.Value
	secondaryWas := base.Colors.Secondary.Value
	spacingWas := base.Spacing.MD.Value

	out, err := style.ApplyTokens(base, map[string]string{"color-primary": "#FF0000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Colors.Primary.Value != "#FF0000" {
		t.Errorf("primary not applied: got %q", out.Colors.Primary.Value)
	}
	if out.Colors.Secondary.Value != secondaryWas {
		t.Errorf("untouched token changed: secondary = %q, want %q", out.Colors.Secondary.Value, secondaryWas)
	}
	if out.Spacing.MD.Value != spacingWas {
		t.Errorf("untouched token changed: spacing-md = %d, want %d", out.Spacing.MD.Value, spacingWas)
	}
	// Base must be unmodified.
	if base.Colors.Primary.Value != primaryWas {
		t.Errorf("ApplyTokens mutated the base theme's primary (value copy leaked)")
	}
}

func TestApplyTokens_DoesNotMutateBaseDarkMaps(t *testing.T) {
	// The theme-variant host holds one base theme process-wide and serves
	// per-request variants off it. If ApplyTokens wrote through base's dark
	// map, one visitor's brand color would leak into every other visitor.
	base := uitheme.Default()
	before := base.DarkColors["primary"]

	_, err := style.ApplyTokens(base, map[string]string{"dark.color-primary": "#112233"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.DarkColors["primary"] != before {
		t.Errorf("ApplyTokens mutated base.DarkColors (map aliasing bug): %q -> %q", before, base.DarkColors["primary"])
	}
}

func TestApplyTokens_DarkOverrideAcceptsValidRejectsInjection(t *testing.T) {
	base := uitheme.Default()
	// A legitimate dark override round-trips.
	out, err := style.ApplyTokens(base, map[string]string{"dark.color-primary": "#112233"})
	if err != nil {
		t.Fatalf("valid dark override rejected: %v", err)
	}
	if out.DarkColors["primary"] != "#112233" {
		t.Errorf("dark override not applied: %q", out.DarkColors["primary"])
	}
	// An injection payload is rejected even on a valid dark key.
	for _, payload := range []string{"red; }", "url(x)", "red{}"} {
		if _, err := style.ApplyTokens(base, map[string]string{"dark.color-primary": payload}); err == nil {
			t.Errorf("dark.color-primary accepted injection %q", payload)
		}
	}
}

// --- tokenMapsEqual helper ------------------------------------------------

func tokenMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
