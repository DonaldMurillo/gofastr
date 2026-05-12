package style

import (
	"strings"
	"testing"
)

func TestRegisterThemeOverrideIdempotent(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Name: "primary", Value: "#FF0000"}
	a := RegisterThemeOverride(th)
	b := RegisterThemeOverride(th)
	if a.Hash != b.Hash {
		t.Errorf("idempotent registration should return same hash; got %s vs %s", a.Hash, b.Hash)
	}
	if !strings.HasPrefix(a.Class(), "fui-theme-") {
		t.Errorf("Class should be fui-theme-<hash>: %q", a.Class())
	}
}

func TestThemeOverrideCSSWrapsInClass(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Name: "primary", Value: "#FF00FF"}
	ref := RegisterThemeOverride(th)
	css := ThemeOverrideCSS(ref.Hash, th)
	if !strings.Contains(css, ".fui-theme-"+ref.Hash+" {") {
		t.Errorf("CSS should open with class selector: %q", css)
	}
	if !strings.Contains(css, "--color-primary: #FF00FF;") {
		t.Errorf("override should include changed token: %q", css)
	}
}

func TestAllThemeOverridesCSSDeterministic(t *testing.T) {
	// Register two overrides; output must be byte-stable across calls.
	th1 := DefaultTheme()
	th1.Colors.Primary = Color{Name: "primary", Value: "#AAAAAA"}
	_ = RegisterThemeOverride(th1)
	th2 := DefaultTheme()
	th2.Colors.Primary = Color{Name: "primary", Value: "#BBBBBB"}
	_ = RegisterThemeOverride(th2)
	first := AllThemeOverridesCSS()
	for i := 0; i < 20; i++ {
		if got := AllThemeOverridesCSS(); got != first {
			t.Fatalf("non-deterministic at iter %d", i)
		}
	}
}
