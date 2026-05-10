package skeleton

import (
	"strings"
	"testing"
)

func TestDefaultsToLine(t *testing.T) {
	h := string(New(Config{}))
	if !strings.Contains(h, "skeleton-line") {
		t.Errorf("expected skeleton-line, got: %s", h)
	}
}

func TestVariantBlock(t *testing.T) {
	h := string(New(Config{Variant: Block}))
	if !strings.Contains(h, "skeleton-block") {
		t.Errorf("expected skeleton-block, got: %s", h)
	}
}

func TestVariantCircle(t *testing.T) {
	h := string(New(Config{Variant: Circle}))
	if !strings.Contains(h, "skeleton-circle") {
		t.Errorf("expected skeleton-circle, got: %s", h)
	}
}

func TestAriaHiddenAlwaysTrue(t *testing.T) {
	h := string(New(Config{}))
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("expected aria-hidden=true, got: %s", h)
	}
	stack := string(New(Config{Count: 3}))
	if !strings.Contains(stack, `aria-hidden="true"`) {
		t.Errorf("expected stack to be aria-hidden, got: %s", stack)
	}
}

func TestCountRendersStack(t *testing.T) {
	h := string(New(Config{Count: 3}))
	if !strings.Contains(h, "skeleton-stack") {
		t.Errorf("expected skeleton-stack, got: %s", h)
	}
	if strings.Count(h, "skeleton-line") != 3 {
		t.Errorf("expected 3 skeleton-line, got %d in: %s",
			strings.Count(h, "skeleton-line"), h)
	}
}

func TestStackLastLineShortened(t *testing.T) {
	h := string(New(Config{Count: 3}))
	if !strings.Contains(h, "inline-size:65%") {
		t.Errorf("expected last line to default to 65%% width, got: %s", h)
	}
}

func TestExplicitWidthHeight(t *testing.T) {
	h := string(New(Config{Variant: Block, Width: "12rem", Height: "4rem"}))
	if !strings.Contains(h, "inline-size:12rem") || !strings.Contains(h, "block-size:4rem") {
		t.Errorf("expected width/height in style, got: %s", h)
	}
}

func TestCircleUsesEqualSides(t *testing.T) {
	h := string(New(Config{Variant: Circle, Width: "3rem"}))
	if !strings.Contains(h, "inline-size:3rem") || !strings.Contains(h, "block-size:3rem") {
		t.Errorf("expected circle to use equal sides, got: %s", h)
	}
}

func TestBaseCSSHasKeyframes(t *testing.T) {
	css := BaseCSS()
	for _, want := range []string{
		"@keyframes skeleton-shimmer", ".skeleton-line", ".skeleton-circle",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("BaseCSS missing %q", want)
		}
	}
}
