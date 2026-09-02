package ui_test

import (
	"regexp"
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: a numeric input's declared bounds must describe the range
// the caller intended. NumberInput uses the int zero value as "unset"
// (its doc: "When both 0, no client-side bound is applied"), but the
// emitter gates on `Min != 0 || Max != 0` and then writes BOTH
// attributes — so a one-sided bound silently fabricates the other side
// at zero. Min-only produces min="1" max="0": an EMPTY valid range,
// every value invalid. Max-only produces min="0": negative values the
// caller never forbade are rejected. NumberField (pointer bounds) is
// the correct contrast and is pinned green below.
func TestNumberInputOneSidedBoundBadRange(t *testing.T) {
	t.Run("min-only-fabricates-empty-range", func(t *testing.T) {
		h := string(ui.NumberInput(ui.NumberInputConfig{
			Name: "n", Label: "N", Min: 1, Value: 5,
		}))
		if strings.Contains(h, `max="0"`) {
			t.Errorf("RED: NumberInput Min-only emitted max=\"0\" alongside min=\"1\" — the intersection is empty, every value is invalid:\n%s", h)
		}
		if !strings.Contains(h, `min="1"`) {
			t.Errorf("NumberInput dropped the declared lower bound:\n%s", h)
		}
	})
	t.Run("max-only-fabricates-zero-floor", func(t *testing.T) {
		h := string(ui.NumberInput(ui.NumberInputConfig{
			Name: "n", Label: "N", Max: 10, Value: -2,
		}))
		if strings.Contains(h, `min="0"`) {
			t.Errorf("RED: NumberInput Max-only emitted min=\"0\", silently forbidding the negative values the caller allowed:\n%s", h)
		}
		if !strings.Contains(h, `max="10"`) {
			t.Errorf("NumberInput dropped the declared upper bound:\n%s", h)
		}
	})
}

// TestNumberFieldPointerBoundsOneSided is the green contrast: NumberField
// distinguishes an explicit zero from an omitted bound with *float64
// pointers, so a one-sided bound renders exactly one attribute.
func TestNumberFieldPointerBoundsOneSided(t *testing.T) {
	min := 1.5
	hMin := string(ui.NumberField(ui.NumberFieldConfig{
		Name: "n", Label: "N", Min: &min,
	}))
	if !strings.Contains(hMin, `min="1.5"`) || strings.Contains(hMin, `max="`) {
		t.Errorf("NumberField Min-only must emit min and no max:\n%s", hMin)
	}

	max := 10.0
	hMax := string(ui.NumberField(ui.NumberFieldConfig{
		Name: "n", Label: "N", Max: &max,
	}))
	if !strings.Contains(hMax, `max="10"`) || strings.Contains(hMax, `min="`) {
		t.Errorf("NumberField Max-only must emit max and no min:\n%s", hMax)
	}
}

// Property: a component that documents a per-item length cap must apply
// it to every surface the item can enter. TagInput.MaxLength is
// documented as "caps individual tag length (chars)" and is enforced on
// the visible input (maxlength) and at JS commit time — but the
// SSR-rendered Values bypass it entirely: a server that round-trips
// r.Form["tags"] re-renders attacker-supplied oversized tags as hidden
// inputs that resubmit verbatim. This is the red for that gap.
func TestTagInputMaxLengthSkipsSSRValues(t *testing.T) {
	h := string(ui.TagInput(ui.TagInputConfig{
		Name: "tags", Label: "Tags", MaxLength: 5,
		Values: []string{"aaaaaaaaaa", "ok"},
	}))
	re := regexp.MustCompile(`class="ui-tag-input__hidden"[^>]*value="([^"]*)"`)
	for _, m := range re.FindAllStringSubmatch(h, -1) {
		if got := len(strings.ReplaceAll(strings.ReplaceAll(m[1], "&amp;", "&"), "&quot;", `"`)); got > 5 {
			t.Errorf("RED: hidden tag value %q is %d chars; MaxLength=5 must cap every rendered tag (doc: \"caps individual tag length\"):\n%s", m[1], got, h)
		}
	}
	if !strings.Contains(h, `value="ok"`) {
		t.Fatalf("expected the in-cap tag to render:\n%s", h)
	}
}
