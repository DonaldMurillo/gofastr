package style

import (
	"maps"
	"strings"
	"testing"
)

// The component-options layer: the Components grammar, the compiler
// hook (registration, freeze, emit-time validation), the token-map
// round trip, hash discrimination, copy isolation, and the scoped
// blocks' shape. One test compiler stands in for framework/ui's; the
// real one's emission has its own tests beside it.

// stageTestCompiler resets the hook and installs fn; the caller's
// deferred restore keeps the package's other tests free to emit theme
// CSS without a compiler installed (they never set Components, but the
// freeze must not leak between tests either).
func stageTestCompiler(t *testing.T, fn func(components map[string]string) []Declaration) {
	t.Helper()
	resetComponentOptionsForTest()
	t.Cleanup(resetComponentOptionsForTest)
	RegisterComponentOptionsCompiler(fn)
}

// echoCompiler emits one declaration per option, name derived from the
// key, value from the value: deterministic, and different options
// produce different bytes, which is what the hash tests need.
func echoCompiler(components map[string]string) []Declaration {
	decls := make([]Declaration, 0, len(components))
	for _, k := range sortedMapKeys(components) {
		decls = append(decls, Declaration{
			Name:  "--hui-test-" + strings.ReplaceAll(k, ".", "-"),
			Value: components[k],
		})
	}
	return decls
}

func themeWithComponents(opts map[string]string) Theme {
	th := DefaultTheme()
	th.Components = opts
	return th
}

func TestComponentsGrammarInValidate(t *testing.T) {
	for _, k := range []string{"Density", "button.Treatment", ".density", "density.", "button..treatment", "button treatment", "button.treatment.two "} {
		if err := themeWithComponents(map[string]string{k: "compact"}).Validate(); err == nil {
			t.Errorf("key %q passed Validate: the grammar must refuse it", k)
		}
	}
	for _, v := range []string{"Compact", "", "filled;}", "two words", "outline!"} {
		if err := themeWithComponents(map[string]string{"density": v}).Validate(); err == nil {
			t.Errorf("value %q passed Validate: the grammar must refuse it", v)
		}
	}
	good := map[string]string{
		"density":          "compact",
		"button.treatment": "outline",
		"button.radius":    "pill",
	}
	if err := themeWithComponents(good).Validate(); err != nil {
		t.Errorf("valid Components failed Validate: %v", err)
	}
}

func TestComponentOptionsRootEmission(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	css := themeWithComponents(map[string]string{"density": "compact"}).CSSCustomProperties()
	want := ":root {\n  --hui-test-density: compact;\n}"
	if !strings.Contains(css, want) {
		t.Errorf("root block missing compiled option\nwant substring:\n%s\ngot:\n%s", want, css)
	}
	// A theme with no options emits no compiled block.
	stageTestCompiler(t, echoCompiler)
	if css := DefaultTheme().CSSCustomProperties(); strings.Contains(css, "--hui-test") {
		t.Error("empty Components emitted compiled declarations")
	}
}

func TestComponentOptionsScopedEmission(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	th := themeWithComponents(map[string]string{"button.treatment": "outline"})
	th.DarkColors = map[string]string{"background": "#0a0a0a"}
	ref := RegisterThemeOverride(th)
	css := ThemeOverrideCSS(ref.Hash(), th)
	// The option and the alias lines must sit INSIDE every scope block,
	// after the tokens they rebind against.
	for _, probe := range []struct{ block, opener string }{
		{"light", ".fui-theme-" + ref.Hash() + " {\n"},
		{"explicit dark", "\n[data-color-scheme=\"dark\"] .fui-theme-" + ref.Hash() + " {\n"},
		{"media dark", "  :root:not([data-color-scheme=\"light\"]) .fui-theme-" + ref.Hash() + " {\n"},
	} {
		i := strings.Index(css, probe.opener)
		if i < 0 {
			t.Fatalf("%s block missing its opener %q in:\n%s", probe.block, probe.opener, css)
		}
		end := strings.Index(css[i:], "\n}")
		if end < 0 {
			t.Fatalf("%s block missing its closer in:\n%s", probe.block, css)
		}
		body := css[i : i+end]
		if !strings.Contains(body, "--hui-test-button-treatment: outline;") {
			t.Errorf("%s block lacks the compiled option:\n%s", probe.block, body)
		}
		if !strings.Contains(body, "--color-primary-foreground: var(--color-primary-fg);") {
			t.Errorf("%s block lacks the re-emitted alias tokens:\n%s", probe.block, body)
		}
	}
	if !strings.Contains(css, "--color-background: #0a0a0a;") {
		t.Error("dark token missing from dark scope blocks")
	}
}

func TestScopedThemeWithoutDarkPaletteStaysLight(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	th := themeWithComponents(nil)
	ref := RegisterThemeOverride(th)
	css := ThemeOverrideCSS(ref.Hash(), th)
	if strings.Contains(css, "data-color-scheme=\"dark\"") || strings.Contains(css, "prefers-color-scheme") {
		t.Errorf("a scope with no dark palette emitted dark blocks:\n%s", css)
	}
}

func TestComponentOptionsCompilerRegistrationRules(t *testing.T) {
	resetComponentOptionsForTest()
	t.Cleanup(resetComponentOptionsForTest)

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected a panic", name)
			}
		}()
		fn()
	}

	mustPanic("nil compiler", func() { RegisterComponentOptionsCompiler(nil) })

	RegisterComponentOptionsCompiler(echoCompiler)
	mustPanic("second registration", func() { RegisterComponentOptionsCompiler(echoCompiler) })

	// The freeze: the first emit latches the registration shut. A
	// compiler registered after the host composed app.css would be
	// missing from the frozen sheet, the catalog and the manifest —
	// three caches, one miss — so registration after that point is a
	// wiring bug and says so.
	_ = DefaultTheme().CSSCustomProperties()
	mustPanic("late registration", func() { RegisterComponentOptionsCompiler(echoCompiler) })

	// The freeze must hold on its own, not ride on "already
	// registered": emit FIRST, with no compiler at all, then try to
	// register. This is the production shape the guard exists for —
	// the host composed app.css before the styled layer's init ran.
	resetComponentOptionsForTest()
	_ = DefaultTheme().CSSCustomProperties()
	mustPanic("registration after an emit with no compiler", func() { RegisterComponentOptionsCompiler(echoCompiler) })

	// Emitting with no compiler registered at all stays silent: a
	// process that never imports the styled layer stores options it
	// cannot draw, and hashes them apart only once one is present.
	resetComponentOptionsForTest()
	if css := themeWithComponents(map[string]string{"density": "compact"}).CSSCustomProperties(); strings.Contains(css, "--hui") {
		t.Error("options emitted with no compiler registered")
	}
}

func TestComponentCompilerInvalidDeclarationPanicsAtEmit(t *testing.T) {
	for _, tc := range []struct {
		name string
		decl Declaration
	}{
		{"name without --", Declaration{Name: "hui-density", Value: "36px"}},
		{"name with a space", Declaration{Name: "--hui density", Value: "36px"}},
		{"name that selectors", Declaration{Name: "--x}body{", Value: "36px"}},
		{"empty value", Declaration{Name: "--hui-density", Value: ""}},
		{"value breaks the declaration", Declaration{Name: "--hui-density", Value: "36px; color: red"}},
		{"value closes the block", Declaration{Name: "--hui-density", Value: "36px}"}},
		{"value opens markup", Declaration{Name: "--hui-density", Value: "36px<img>"}},
		{"value loads a url", Declaration{Name: "--hui-density", Value: "url(https://attacker/x)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stageTestCompiler(t, func(map[string]string) []Declaration {
				return []Declaration{tc.decl}
			})
			defer func() {
				if recover() == nil {
					t.Errorf("compiler declaration %+v emitted without a panic", tc.decl)
				}
			}()
			_ = themeWithComponents(map[string]string{"density": "compact"}).CSSCustomProperties()
		})
	}
	// The grammar the compiler's output MAY use: token references,
	// parentheses, commas, spaces, numbers.
	stageTestCompiler(t, func(map[string]string) []Declaration {
		return []Declaration{{Name: "--hui-ok", Value: "color-mix(in srgb, var(--color-primary) 85%, transparent)"}}
	})
	if css := themeWithComponents(map[string]string{"density": "compact"}).CSSCustomProperties(); !strings.Contains(css, "--hui-ok: color-mix(in srgb, var(--color-primary) 85%, transparent);") {
		t.Error("a legitimate value with parens, commas and spaces was refused")
	}
}

func TestThemeHashSeparatesComponentOptions(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	base := themeWithComponents(map[string]string{"button.treatment": "filled"})
	outline := themeWithComponents(map[string]string{"button.treatment": "outline"})
	same := themeWithComponents(map[string]string{"button.treatment": "filled"})
	if ThemeHash(base) == ThemeHash(outline) {
		t.Error("equal tokens with different options hash identically: an option-dependent stylesheet would be served under the other's cache key")
	}
	if ThemeHash(base) != ThemeHash(same) {
		t.Error("identical themes hash apart")
	}
}

// A binary that links no component-options compiler (framework/uihost
// alone imports neither framework/ui nor registers one here) must still
// not hash two option-different themes together: ThemeHash fingerprints
// the flattened options directly, so identity holds whatever layers the
// binary linked. Without it, the second RegisterThemeOverride of an
// option-different theme would be dropped as a duplicate.
func TestThemeHashSeparatesOptionsWithoutCompiler(t *testing.T) {
	resetComponentOptionsForTest()
	t.Cleanup(resetComponentOptionsForTest)

	base := themeWithComponents(map[string]string{"button.treatment": "filled"})
	outline := themeWithComponents(map[string]string{"button.treatment": "outline"})
	same := themeWithComponents(map[string]string{"button.treatment": "filled"})
	if ThemeHash(base) == ThemeHash(outline) {
		t.Error("option-different themes hashed identically with no compiler registered: identity would depend on which layers the binary linked")
	}
	if ThemeHash(base) != ThemeHash(same) {
		t.Error("identical themes hashed apart with no compiler registered")
	}
}

func TestComponentsTokenMapRoundTrip(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	th := themeWithComponents(map[string]string{
		"density":          "compact",
		"button.treatment": "outline",
		"button.radius":    "square",
	})
	th.DarkColors = map[string]string{"background": "#0a0a0a"}
	tokens := ThemeToTokens(th)
	if tokens["component.button.treatment"] != "outline" {
		t.Errorf("ThemeToTokens lost the option: %#v", tokens["component.button.treatment"])
	}
	reApplied, err := ApplyTokens(DefaultTheme(), tokens)
	if err != nil {
		t.Fatalf("ApplyTokens refused its own round trip: %v", err)
	}
	if !maps.Equal(reApplied.Components, th.Components) {
		t.Errorf("options did not survive the round trip: %#v", reApplied.Components)
	}
	if ThemeHash(reApplied) != ThemeHash(th) {
		t.Error("round trip changed the theme's content address")
	}

	// The grammar is the boundary on the way back in, too.
	if _, err := ApplyTokens(DefaultTheme(), map[string]string{"component.density": "Compact"}); err == nil {
		t.Error("an uppercase option value passed ApplyTokens")
	}
	if _, err := ApplyTokens(DefaultTheme(), map[string]string{"component.Density": "compact"}); err == nil {
		t.Error("an uppercase option key passed ApplyTokens")
	}
}

func TestRegisterThemeOverrideClonesComponents(t *testing.T) {
	stageTestCompiler(t, echoCompiler)
	shared := map[string]string{"density": "compact"}
	sharedDark := map[string]string{"background": "#0a0a0a"}
	th := themeWithComponents(shared)
	th.DarkColors = sharedDark
	ref := RegisterThemeOverride(th)
	before := ThemeOverrideCSS(ref.Hash(), AllThemeOverrides()[ref.Hash()])

	// A caller-side write after registration changes nothing served…
	shared["density"] = "comfortable"
	if after := ThemeOverrideCSS(ref.Hash(), AllThemeOverrides()[ref.Hash()]); after != before {
		t.Error("mutating the caller's map after registration changed the emitted CSS")
	}
	// …and neither does mutating a theme handed back by the registry.
	returned := AllThemeOverrides()[ref.Hash()]
	returned.Components["density"] = "comfortable"
	returned.DarkColors["background"] = "#ffffff"
	if after := ThemeOverrideCSS(ref.Hash(), AllThemeOverrides()[ref.Hash()]); after != before {
		t.Error("mutating a returned copy changed the stored theme")
	}
}

func TestApplyTokensDoesNotShareComponentsMap(t *testing.T) {
	base := themeWithComponents(map[string]string{"density": "compact"})
	applied, err := ApplyTokens(base, map[string]string{"component.density": "comfortable"})
	if err != nil {
		t.Fatalf("ApplyTokens: %v", err)
	}
	if applied.Components["density"] != "comfortable" {
		t.Fatal("option was not applied")
	}
	if base.Components["density"] != "compact" {
		t.Error("ApplyTokens mutated the base theme's Components map")
	}
}
