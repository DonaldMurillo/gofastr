package ui

import (
	"strings"
	"testing"
)

func TestSpinnerDefaultsRingMd(t *testing.T) {
	h := Spinner(SpinnerConfig{})
	mustContain(t, h, `data-fui-comp="ui-spinner"`)
	mustContain(t, h, `role="status"`)
	mustContain(t, h, `aria-busy="true"`)
	mustContain(t, h, "ui-spinner__ring")
	mustContain(t, h, "Loading…")
	if strings.Contains(string(h), "ui-spinner--md") {
		t.Fatalf("default md size should not emit modifier:\n%s", h)
	}
}

func TestSpinnerCustomLabel(t *testing.T) {
	h := Spinner(SpinnerConfig{Label: "Saving"})
	mustContain(t, h, "Saving")
}

func TestSpinnerSizeAndInlineVariants(t *testing.T) {
	h := Spinner(SpinnerConfig{Size: SpinnerLg, Inline: true})
	mustContain(t, h, "ui-spinner--lg")
	mustContain(t, h, "ui-spinner--inline")
}

func TestSpinnerDotsVariant(t *testing.T) {
	h := Spinner(SpinnerConfig{Variant: SpinnerDots})
	mustContain(t, h, "ui-spinner--dots")
	mustContain(t, h, "ui-spinner__dots")
	if !strings.Contains(string(h), "ui-spinner__dot") {
		t.Fatalf("dots variant should render dot children:\n%s", h)
	}
	if strings.Contains(string(h), "ui-spinner__ring") {
		t.Fatalf("dots variant should not render the ring element:\n%s", h)
	}
}

func TestSpinnerGridVariantRendersNineCells(t *testing.T) {
	h := Spinner(SpinnerConfig{Variant: SpinnerGrid})
	mustContain(t, h, "ui-spinner--grid")
	mustContain(t, h, "ui-spinner__grid")
	// Each ::after / cell renders as a child span; exactly 9 cells in
	// the 3x3 grid. We count occurrences of the class name.
	count := strings.Count(string(h), `class="ui-spinner__cell"`)
	if count != 9 {
		t.Fatalf("SpinnerGrid should emit exactly 9 cells, got %d:\n%s", count, h)
	}
	if strings.Contains(string(h), "ui-spinner__ring") || strings.Contains(string(h), "ui-spinner__dots") {
		t.Fatalf("grid variant should not render ring or dots elements:\n%s", h)
	}
}
