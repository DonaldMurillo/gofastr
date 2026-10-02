package ui

import (
	"strings"
	"testing"
)

func TestSpinnerDefaultsRingMd(t *testing.T) {
	h := Spinner(SpinnerConfig{})
	mustContain(t, h, `data-fui-comp="ui-spinner"`)
	mustContain(t, h, `role="status"`)
	mustContain(t, h, "fui-spinner__ring")
	mustContain(t, h, "Loading…")
	if strings.Contains(string(h), "fui-spinner--md") {
		t.Fatalf("default md size should not emit modifier:\n%s", h)
	}
}

func TestSpinnerCustomLabel(t *testing.T) {
	h := Spinner(SpinnerConfig{Label: "Saving"})
	mustContain(t, h, "Saving")
}

func TestSpinnerSizeAndInlineVariants(t *testing.T) {
	h := Spinner(SpinnerConfig{Size: SpinnerLg, Inline: true})
	mustContain(t, h, "fui-spinner--lg")
	mustContain(t, h, "fui-spinner--inline")
}

func TestSpinnerDotsVariant(t *testing.T) {
	h := Spinner(SpinnerConfig{Variant: SpinnerDots})
	mustContain(t, h, "fui-spinner__dots")
	if !strings.Contains(string(h), "fui-spinner__dot") {
		t.Fatalf("dots variant should render dot children:\n%s", h)
	}
	if strings.Contains(string(h), "fui-spinner__ring") {
		t.Fatalf("dots variant should not render the ring element:\n%s", h)
	}
}

func TestSpinnerGridVariantRendersNineCells(t *testing.T) {
	h := Spinner(SpinnerConfig{Variant: SpinnerGrid})
	mustContain(t, h, "fui-spinner__grid")
	// Each cell renders as a child span; exactly 9 cells in the 3x3
	// grid. We count occurrences of the class name.
	count := strings.Count(string(h), `class="fui-spinner__cell"`)
	if count != 9 {
		t.Fatalf("SpinnerGrid should emit exactly 9 cells, got %d:\n%s", count, h)
	}
	if strings.Contains(string(h), "fui-spinner__ring") || strings.Contains(string(h), "fui-spinner__dots") {
		t.Fatalf("grid variant should not render ring or dots elements:\n%s", h)
	}
}

func TestSpinnerExtraAttrsOnRoot(t *testing.T) {
	h := Spinner(SpinnerConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Spinner root missing data-test:\n%s", root)
	}
}

// role=status already implies a polite live region, and busy belongs
// to the region an RPC marks: the spinner writes neither attribute.
func TestSpinnerWritesNoLiveOrBusy(t *testing.T) {
	h := string(Spinner(SpinnerConfig{}))
	for _, bad := range []string{"aria-live", "aria-busy"} {
		if strings.Contains(h, bad) {
			t.Errorf("the spinner wrote %s:\n%s", bad, h)
		}
	}
}
