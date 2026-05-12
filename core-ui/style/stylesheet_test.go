package style

import (
	"strings"
	"testing"
)

func TestStyleSheetBasic(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".test").
		Set("color", "{colors.primary}", "padding", "{spacing.md}").
		End()

	css := ss.CSS()
	if !strings.Contains(css, ".test {") {
		t.Errorf("expected .test rule, got: %s", css)
	}
	// Var-only contract: tokens resolve to CSS variable references,
	// never to literal values. The browser dereferences via the
	// :root block emitted by Theme.CSSCustomProperties().
	if !strings.Contains(css, "color: var(--color-primary)") {
		t.Errorf("expected var ref for color token, got: %s", css)
	}
	if !strings.Contains(css, "padding: var(--spacing-md)") {
		t.Errorf("expected var ref for spacing token, got: %s", css)
	}
}

func TestStyleSheetPseudo(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".btn").
		Set("background", "{colors.primary}").
		Pseudo(":hover", "background", "{colors.secondary}").
		End()

	css := ss.CSS()
	if !strings.Contains(css, ".btn:hover {") {
		t.Errorf("expected .btn:hover rule, got: %s", css)
	}
	if !strings.Contains(css, ".btn {") {
		t.Errorf("expected .btn rule, got: %s", css)
	}
}

func TestStyleSheetChild(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".card").
		Child("h3", "font-size", "1.125rem").
		Child("img", "width", "100%").
		End()

	css := ss.CSS()
	if !strings.Contains(css, ".card h3 {") {
		t.Errorf("expected .card h3 rule, got: %s", css)
	}
	if !strings.Contains(css, ".card img {") {
		t.Errorf("expected .card img rule, got: %s", css)
	}
}

func TestStyleSheetKeyframes(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Keyframes("fade",
		Step("0%", "opacity", "0"),
		Step("100%", "opacity", "1"),
	)

	css := ss.CSS()
	if !strings.Contains(css, "@keyframes fade {") {
		t.Errorf("expected @keyframes fade, got: %s", css)
	}
	if !strings.Contains(css, "0% {") {
		t.Errorf("expected 0%% step, got: %s", css)
	}
}

func TestStyleSheetMedia(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".grid").
		Set("grid-template-columns", "1fr 1fr").
		Media("max-width: 640px", func(ss *StyleSheet) {
			ss.Rule(".grid").
				Set("grid-template-columns", "1fr").
				End()
		}).
		End()

	css := ss.CSS()
	if !strings.Contains(css, "@media max-width: 640px") {
		t.Errorf("expected @media rule, got: %s", css)
	}
	if !strings.Contains(css, "grid-template-columns: 1fr 1fr") {
		t.Errorf("expected desktop grid rule, got: %s", css)
	}
}

// TestStyleSheetMediaDoesNotLeak guards against a regression where
// .Pseudo() called AFTER .Media() inside the same Rule chain ended up
// nested inside the @media block — silently changing the selector's
// meaning (hover only above 640px).
func TestStyleSheetMediaDoesNotLeak(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)
	ss.Rule(".btn").
		Set("color", "red").
		Media("(min-width: 640px)", func(ss *StyleSheet) {
			ss.Rule(".btn").Set("color", "blue").End()
		}).
		Pseudo(":hover", "color", "green").
		End()

	css := ss.CSS()
	// Count braces between the @media start and .btn:hover. If
	// open == close, the :hover sits OUTSIDE the @media block.
	mediaIdx := strings.Index(css, "@media (min-width: 640px)")
	hoverIdx := strings.Index(css, ".btn:hover")
	if mediaIdx < 0 || hoverIdx < 0 {
		t.Fatalf("expected both @media and :hover; got\n%s", css)
	}
	if hoverIdx > mediaIdx {
		between := css[mediaIdx:hoverIdx]
		open := strings.Count(between, "{")
		close := strings.Count(between, "}")
		if open > close {
			t.Errorf(":hover leaked INSIDE @media block; got:\n%s", css)
		}
	}
}

// TestStyleSheetNestedMediaPreservesInner asserts that Media() inside
// a Media() callback produces a nested @media block — the outer
// Media() must not overwrite the inner rule's parent.
func TestStyleSheetNestedMediaPreservesInner(t *testing.T) {
	ss := NewStyleSheet(DefaultTheme())
	ss.Media("(min-width: 640px)", func(outer *StyleSheet) {
		outer.Rule(".x").Set("color", "red").End()
		outer.Media("(prefers-color-scheme: dark)", func(inner *StyleSheet) {
			inner.Rule(".x").Set("color", "blue").End()
		})
	})
	css := ss.CSS()
	outerIdx := strings.Index(css, "@media (min-width: 640px)")
	innerIdx := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if outerIdx < 0 {
		t.Fatalf("expected outer @media; got\n%s", css)
	}
	if innerIdx < 0 {
		t.Fatalf("nested @media silently collapsed — inner rule lost its parent; got\n%s", css)
	}
	if outerIdx >= innerIdx {
		t.Errorf("outer @media must appear BEFORE inner @media in serialized CSS; got outer=%d inner=%d:\n%s", outerIdx, innerIdx, css)
	}
}

// TestStyleSheetMediaMixedOrder covers the case where a Media block
// wraps three siblings: a plain rule, an inner Media, and another
// plain rule. The output must preserve source order — the trailing
// rule must come AFTER the inner @media closes, not get sucked into
// the inner block.
func TestStyleSheetMediaMixedOrder(t *testing.T) {
	ss := NewStyleSheet(DefaultTheme())
	ss.Media("(min-width: 640px)", func(outer *StyleSheet) {
		outer.Rule(".a").Set("color", "red").End()
		outer.Media("(prefers-color-scheme: dark)", func(inner *StyleSheet) {
			outer.Rule(".b").Set("color", "blue").End()
			_ = inner // satisfy linter — we intentionally use outer here
		})
		outer.Rule(".c").Set("color", "green").End()
	})
	css := ss.CSS()
	aIdx := strings.Index(css, ".a {")
	bIdx := strings.Index(css, ".b {")
	cIdx := strings.Index(css, ".c {")
	if aIdx < 0 || bIdx < 0 || cIdx < 0 {
		t.Fatalf("missing rule in output:\n%s", css)
	}
	if !(aIdx < bIdx && bIdx < cIdx) {
		t.Errorf("rules must serialize in source order .a < .b < .c; got a=%d b=%d c=%d:\n%s", aIdx, bIdx, cIdx, css)
	}
}

// TestStyleSheetMediaTopLevel covers Media() called at the top level
// (no enclosing Rule). Previously a silent no-op.
func TestStyleSheetMediaTopLevel(t *testing.T) {
	ss := NewStyleSheet(DefaultTheme())
	ss.Rule(".x").Set("color", "red").End()
	ss.Media("(min-width: 640px)", func(ss *StyleSheet) {
		ss.Rule(".x").Set("color", "blue").End()
	})
	css := ss.CSS()
	if !strings.Contains(css, "@media (min-width: 640px)") {
		t.Errorf("expected top-level @media, got:\n%s", css)
	}
	if !strings.Contains(css, "color: blue") {
		t.Errorf("expected nested rule body inside @media, got:\n%s", css)
	}
}

// TestStyleSheetContainerTopLevel mirrors Media — Container() called
// at the top level must produce real @container output.
func TestStyleSheetContainerTopLevel(t *testing.T) {
	ss := NewStyleSheet(DefaultTheme())
	ss.Container("layout", "(min-width: 400px)", func(ss *StyleSheet) {
		ss.Rule(".card").Set("padding", "16px").End()
	})
	css := ss.CSS()
	if !strings.Contains(css, "@container layout (min-width: 400px)") {
		t.Errorf("expected top-level @container, got:\n%s", css)
	}
	if !strings.Contains(css, "padding: 16px") {
		t.Errorf("expected nested rule body, got:\n%s", css)
	}
}

// TestStyleSheetSetOddCountPanics catches the silent-drop footgun
// where ss.Set("padding") (no value) was ignored without warning.
// TestStyleSheetSetBeforeRulePanics guards against the silent no-op
// where ss.Set(...) called BEFORE any ss.Rule(...) just drops the
// properties on the floor. A typo'd builder chain should fail loud.
func TestStyleSheetSetBeforeRulePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on Set before Rule, got none")
		}
	}()
	ss := NewStyleSheet(DefaultTheme())
	ss.Set("color", "red")
}

func TestStyleSheetPseudoBeforeRulePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on Pseudo before Rule, got none")
		}
	}()
	ss := NewStyleSheet(DefaultTheme())
	ss.Pseudo(":hover", "color", "red")
}

func TestStyleSheetChildBeforeRulePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on Child before Rule, got none")
		}
	}()
	ss := NewStyleSheet(DefaultTheme())
	ss.Child(".x", "color", "red")
}

func TestStyleSheetSetOddCountPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on odd-count Set, got none")
		}
	}()
	ss := NewStyleSheet(DefaultTheme())
	ss.Rule(".x").Set("color", "red", "padding").End()
}

func TestStyleSheetPseudoOddCountPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on odd-count Pseudo, got none")
		}
	}()
	ss := NewStyleSheet(DefaultTheme())
	ss.Rule(".x").Pseudo(":hover", "color").End()
}

// TestStyleSheetDeterministic asserts CSS() is byte-stable across
// repeated calls. Load-bearing for content-addressed cache keys.
func TestStyleSheetDeterministic(t *testing.T) {
	build := func() string {
		ss := NewStyleSheet(DefaultTheme())
		ss.Rule(".a").Set("color", "red").End()
		ss.Rule(".b").Set("color", "blue").End()
		ss.Media("(min-width: 640px)", func(ss *StyleSheet) {
			ss.Rule(".a").Set("color", "green").End()
		})
		return ss.CSS()
	}
	first := build()
	for i := 0; i < 20; i++ {
		if build() != first {
			t.Fatalf("non-deterministic CSS() on iteration %d", i)
		}
	}
}

func TestStyleSheetEmptyRule(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".empty").End()

	css := ss.CSS()
	// Empty rules should not output
	if strings.Contains(css, ".empty") {
		t.Errorf("empty rule should not generate output, got: %s", css)
	}
}

func TestStyleSheetComplexSelector(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule("[aria-label=\"Hero\"]").
		Set("text-align", "center").
		Child("h1", "font-size", "2.5rem").
		End()

	css := ss.CSS()
	if !strings.Contains(css, `[aria-label="Hero"] {`) {
		t.Errorf("expected aria selector, got: %s", css)
	}
	if !strings.Contains(css, `[aria-label="Hero"] h1 {`) {
		t.Errorf("expected child selector, got: %s", css)
	}
}

func TestStyleSheetContainer(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".sidebar").
		Set("container-type", "inline-size", "container-name", "sidebar").
		Container("sidebar", "(min-width: 400px)", func(ss *StyleSheet) {
			ss.Rule(".sidebar .widget").
				Set("font-size", "1.125rem").
				End()
		}).
		End()

	css := ss.CSS()
	if !strings.Contains(css, "container-type: inline-size") {
		t.Errorf("expected container-type, got: %s", css)
	}
	if !strings.Contains(css, "container-name: sidebar") {
		t.Errorf("expected container-name, got: %s", css)
	}
	if !strings.Contains(css, "@container sidebar (min-width: 400px)") {
		t.Errorf("expected @container rule, got: %s", css)
	}
	if !strings.Contains(css, ".sidebar .widget {") {
		t.Errorf("expected .sidebar .widget rule, got: %s", css)
	}
}

func TestStyleSheetContainerUnnamed(t *testing.T) {
	theme := DefaultTheme()
	ss := NewStyleSheet(theme)

	ss.Rule(".card-grid").
		Set("container-type", "inline-size").
		Container("", "(min-width: 300px)", func(ss *StyleSheet) {
			ss.Rule(".card").
				Set("flex-direction", "row").
				End()
		}).
		End()

	css := ss.CSS()
	if !strings.Contains(css, "@container (min-width: 300px)") {
		t.Errorf("expected unnamed @container, got: %s", css)
	}
}
