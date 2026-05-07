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
	if !strings.Contains(css, "color: #4F46E5") {
		t.Errorf("expected resolved color token, got: %s", css)
	}
	if !strings.Contains(css, "padding: 8px") {
		t.Errorf("expected resolved spacing token, got: %s", css)
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
