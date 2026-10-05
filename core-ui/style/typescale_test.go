package style

import (
	"strings"
	"testing"
)

// Line heights, letter spacing and opacities are theme tokens, so a
// theme alone can loosen body copy, tighten headings or change how far
// a disabled control fades.
func TestLeadingTrackingOpacityEmitted(t *testing.T) {
	css := DefaultTheme().CSSCustomProperties()
	for _, decl := range []string{
		"--leading-tight: 1.2;", "--leading-snug: 1.4;",
		"--leading-normal: 1.5;", "--leading-relaxed: 1.6;",
		"--tracking-tighter: -0.03em;", "--tracking-tight: -0.02em;",
		"--tracking-snug: -0.01em;", "--tracking-wide: 0.04em;",
		"--tracking-wider: 0.08em;",
		"--opacity-faint: 0.2;", "--opacity-disabled: 0.5;", "--opacity-muted: 0.6;",
	} {
		if !strings.Contains(css, decl) {
			t.Errorf("missing %q in :root", decl)
		}
	}
	th := DefaultTheme()
	if got := th.ResolveAll("{leading.normal} {tracking.wide} {opacity.disabled}"); got != "var(--leading-normal) var(--tracking-wide) var(--opacity-disabled)" {
		t.Errorf("ResolveAll = %q", got)
	}
}

func TestBadTypeScaleValuesRefused(t *testing.T) {
	cases := []struct {
		name string
		set  func(*Theme)
	}{
		{"leading %", func(th *Theme) { th.Leading.Normal.Value = "150%" }},
		{"leading negative", func(th *Theme) { th.Leading.Normal.Value = "-1" }},
		{"leading word", func(th *Theme) { th.Leading.Normal.Value = "loose" }},
		{"tracking unitless", func(th *Theme) { th.Tracking.Wide.Value = "0.04" }},
		{"tracking calc", func(th *Theme) { th.Tracking.Wide.Value = "calc(1px)" }},
		{"opacity > 1", func(th *Theme) { th.Opacities.Muted.Value = "1.5" }},
		{"opacity negative", func(th *Theme) { th.Opacities.Muted.Value = "-0.5" }},
		{"opacity trailing dot", func(th *Theme) { th.Opacities.Muted.Value = "0." }},
	}
	for _, tc := range cases {
		th := DefaultTheme()
		tc.set(&th)
		if err := th.Validate(); err == nil {
			t.Errorf("%s: must fail validation", tc.name)
		}
	}
	good := DefaultTheme()
	good.Leading.Normal.Value = "24px"
	good.Tracking.Tight.Value = "0"
	good.Opacities.Disabled.Value = "1"
	good.Opacities.Faint.Value = ".25"
	if err := good.Validate(); err != nil {
		t.Errorf("valid values refused: %v", err)
	}
}

// A theme.go written before these groups existed leaves them zero; they
// must stay unnamed and unemitted so the kit's fallbacks draw.
func TestUnsetTypeScaleFallsBack(t *testing.T) {
	th := DefaultTheme()
	th.Leading, th.Tracking, th.Opacities = LeadingSet{}, TrackingSet{}, OpacitySet{}
	AutoFillNames(&th)
	if err := th.Validate(); err != nil {
		t.Fatalf("a theme without the groups must validate: %v", err)
	}
	css := th.CSSCustomProperties()
	for _, prefix := range []string{"--leading-", "--tracking-", "--opacity-"} {
		if strings.Contains(css, prefix) {
			t.Errorf("unset %s tokens were emitted", prefix)
		}
	}
	th.Leading.Snug = LineHeight{Value: "1.3"}
	th.Tracking.Wide = LetterSpacing{Value: "0.1em"}
	th.Opacities.Muted = Opacity{Value: "0.4"}
	AutoFillNames(&th)
	if th.Leading.Snug.Name != "snug" || th.Leading.Tight.Name != "" {
		t.Errorf("autofill named %q / %q", th.Leading.Snug.Name, th.Leading.Tight.Name)
	}
	if th.Tracking.Wide.Name != "wide" || th.Opacities.Muted.Name != "muted" {
		t.Errorf("autofill missed a set token: %q / %q", th.Tracking.Wide.Name, th.Opacities.Muted.Name)
	}
	if err := th.Validate(); err != nil {
		t.Errorf("a partly set group must validate: %v", err)
	}
}

func TestTypeScaleApplyAndParse(t *testing.T) {
	th, err := ApplyTokens(DefaultTheme(), map[string]string{
		"leading-normal": "1.7", "tracking-wide": "0.1em", "opacity-disabled": "0.4",
	})
	if err != nil {
		t.Fatalf("ApplyTokens: %v", err)
	}
	if th.Leading.Normal.Value != "1.7" || th.Tracking.Wide.Value != "0.1em" || th.Opacities.Disabled.Value != "0.4" {
		t.Errorf("not applied: %+v %+v %+v", th.Leading.Normal, th.Tracking.Wide, th.Opacities.Disabled)
	}
	if _, err := ApplyTokens(DefaultTheme(), map[string]string{"opacity-disabled": "2"}); err == nil {
		t.Error("ApplyTokens accepted opacity 2")
	}
	for key, want := range map[string]string{"leading-x": "leading", "tracking-x": "tracking", "opacity-x": "opacity"} {
		if got := TokenCategory(key); got != want {
			t.Errorf("TokenCategory(%q) = %q", key, got)
		}
	}
	tok, err := ParseToken("leading-display", "1.05")
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if l, ok := tok.(LineHeight); !ok || l.Value != "1.05" || l.Name != "display" {
		t.Errorf("ParseToken = %#v", tok)
	}
}

// Knobs carry the per-component --ui-* variables on the theme itself,
// so one theme reaches every value the scale tokens do not.
func TestKnobsEmittedAtRootAndScope(t *testing.T) {
	th := DefaultTheme()
	th.Knobs = map[string]string{"ui-sidebar-width": "16rem", "ui-button-edge": "var(--color-border-strong)"}
	if err := th.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	css := th.CSSCustomProperties()
	want := ":root {\n  --ui-button-edge: var(--color-border-strong);\n  --ui-sidebar-width: 16rem;\n}"
	if !strings.Contains(css, want) {
		t.Errorf(":root knob block missing:\n%s", css)
	}
	scoped := ThemeOverrideCSS("abc", th)
	if !strings.Contains(scoped, "--ui-sidebar-width: 16rem;") {
		t.Errorf("scope block missing the knob:\n%s", scoped)
	}
	if ThemeHash(th) == ThemeHash(DefaultTheme()) {
		t.Error("a knob did not change the theme's hash")
	}
}

func TestBadKnobsRefused(t *testing.T) {
	for key, val := range map[string]string{
		"sidebar-width":    "16rem",
		"ui-Sidebar":       "16rem",
		"ui--x":            "1px",
		"ui-x":             "1px; } body { color: red",
		"ui-y":             "url(https://evil.example/x)",
		"ui-z":             "",
		"--ui-dashes-kept": "1px",
	} {
		th := DefaultTheme()
		th.Knobs = map[string]string{key: val}
		if err := th.Validate(); err == nil {
			t.Errorf("knob %q=%q must fail validation", key, val)
		}
	}
}

func TestKnobsRoundTripTokens(t *testing.T) {
	th, err := ApplyTokens(DefaultTheme(), map[string]string{"knob.ui-menu-width": "18rem"})
	if err != nil {
		t.Fatalf("ApplyTokens: %v", err)
	}
	if th.Knobs["ui-menu-width"] != "18rem" {
		t.Errorf("knob not applied: %v", th.Knobs)
	}
	if got := ThemeToTokens(th)["knob.ui-menu-width"]; got != "18rem" {
		t.Errorf("ThemeToTokens = %q", got)
	}
	if _, err := ApplyTokens(DefaultTheme(), map[string]string{"knob.menu-width": "18rem"}); err == nil {
		t.Error("ApplyTokens accepted a knob without the ui- prefix")
	}
	base := DefaultTheme()
	base.Knobs = map[string]string{"ui-a": "1px"}
	if _, err := ApplyTokens(base, map[string]string{"knob.ui-a": "2px"}); err != nil {
		t.Fatal(err)
	}
	if base.Knobs["ui-a"] != "1px" {
		t.Error("ApplyTokens wrote through to the base theme's knobs")
	}
}
