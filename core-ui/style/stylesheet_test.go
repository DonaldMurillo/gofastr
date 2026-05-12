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
