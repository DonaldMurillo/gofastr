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
	if a.Hash() != b.Hash() {
		t.Errorf("idempotent registration should return same hash; got %s vs %s", a.Hash(), b.Hash())
	}
	if a.Class() != b.Class() {
		t.Errorf("same content must name the same class: %q vs %q", a.Class(), b.Class())
	}
	if !strings.HasPrefix(a.Class(), "fui-theme-") {
		t.Errorf("Class should be fui-theme-<hash>: %q", a.Class())
	}
}

func TestThemeOverrideCSSWrapsInClass(t *testing.T) {
	th := DefaultTheme()
	th.Colors.Primary = Color{Name: "primary", Value: "#FF00FF"}
	ref := RegisterThemeOverride(th)
	css := ThemeOverrideCSS(ref.Hash(), th)
	if !strings.Contains(css, ".fui-theme-"+ref.Hash()+" {") {
		t.Errorf("CSS should open with class selector: %q", css)
	}
	if !strings.Contains(css, "--color-primary: #FF00FF;") {
		t.Errorf("override should include changed token: %q", css)
	}
}

// Registering must not hash: the documented package-level pattern
// (`var Dark = style.RegisterThemeOverride(...)` in a library package
// that does not import framework/ui) runs during init, before the
// component-options compiler is registered, and a hash computed there
// would freeze the hook with none registered — framework/ui's later
// init would panic. Hashing happens on first use instead, and only
// that latches the freeze.
func TestRegisterThemeOverrideDoesNotHash(t *testing.T) {
	resetComponentOptionsForTest()
	t.Cleanup(resetComponentOptionsForTest)

	th := themeWithComponents(map[string]string{"density": "compact"})
	th.DarkColors = map[string]string{"background": "#0a0a0a"}
	ref := RegisterThemeOverride(th)
	if componentOptionsFrozenForTest() {
		t.Fatal("registering an override froze the compiler hook: the package-level registration pattern must not hash during init")
	}

	// The late registration the pattern needs to survive: this is
	// framework/ui's init running after the library package's.
	RegisterComponentOptionsCompiler(echoCompiler)

	class := ref.Class()
	if !strings.HasPrefix(class, "fui-theme-") {
		t.Errorf("Class() = %q, want fui-theme-<hash>", class)
	}
	if !componentOptionsFrozenForTest() {
		t.Error("Class() hashes, and that first use is what must freeze the compiler hook")
	}
	// The handle's hash still addresses the stored theme: the scope CSS
	// is servable under it, options compiled.
	css := ThemeOverrideCSS(ref.Hash(), AllThemeOverrides()[ref.Hash()])
	if !strings.Contains(css, "--fui-test-density: compact;") {
		t.Errorf("compiled options missing from the scope block under the handle's hash:\n%s", css)
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
	for i := range 20 {
		if got := AllThemeOverridesCSS(); got != first {
			t.Fatalf("non-deterministic at iter %d", i)
		}
	}
}

// A zero ThemeRef is a mistake with a reason, not a nil dereference.
func TestZeroThemeRefPanicsWithAReason(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Class() on a zero ThemeRef returned instead of panicking")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "RegisterThemeOverride") {
			t.Fatalf("the panic does not say where a handle comes from: %v", r)
		}
	}()
	_ = ThemeRef{}.Class()
}
