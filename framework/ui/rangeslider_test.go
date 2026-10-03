package ui

import (
	"strings"
	"testing"
)

func TestRangeSliderRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RangeSlider without Name should panic")
		}
	}()
	RangeSlider(RangeSliderConfig{Label: "x"})
}

func TestRangeSliderRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RangeSlider without Label should panic")
		}
	}()
	RangeSlider(RangeSliderConfig{Name: "x"})
}

func TestRangeSliderEmitsTwoInputs(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{
		Name: "price", Label: "Price", Min: 0, Max: 1000,
		ValueLow: 100, ValueHigh: 800,
	}))
	if c := strings.Count(h, `type="range"`); c != 2 {
		t.Errorf("expected 2 range inputs, got %d:\n%s", c, h)
	}
	if !strings.Contains(h, `name="price-min"`) {
		t.Errorf("expected name=price-min:\n%s", h)
	}
	if !strings.Contains(h, `name="price-max"`) {
		t.Errorf("expected name=price-max:\n%s", h)
	}
	if !strings.Contains(h, `value="100"`) {
		t.Errorf("expected ValueLow=100:\n%s", h)
	}
	if !strings.Contains(h, `value="800"`) {
		t.Errorf("expected ValueHigh=800:\n%s", h)
	}
}

// Posted data is ordered, not refused: a crossed pair renders as the
// ordered low/high pair, the same repair the module makes of a drag.
func TestRangeSliderOrdersCrossedPostedValues(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{
		Name: "x", Label: "x", Min: 0, Max: 100,
		ValueLow: 80, ValueHigh: 20,
	}))
	if !strings.Contains(h, `name="x-min" step="1" type="range" value="20"`) {
		t.Errorf("crossed Low/High should order to the smaller value first:\n%s", h)
	}
	if !strings.Contains(h, `name="x-max" step="1" type="range" value="80"`) {
		t.Errorf("crossed Low/High should order to the larger value second:\n%s", h)
	}
}

func TestRangeSliderShowValueAddsMirror(t *testing.T) {
	on := string(RangeSlider(RangeSliderConfig{
		Name: "x", Label: "x", ShowValue: true,
	}))
	if !strings.Contains(on, `data-hui-range-slider-output="%s to %s"`) {
		t.Errorf("ShowValue should emit the output hook carrying the sentence's shape:\n%s", on)
	}
	if !strings.Contains(on, ">0 to 100</output>") {
		t.Errorf("ShowValue mirror should render the initial pair sentence:\n%s", on)
	}
	off := string(RangeSlider(RangeSliderConfig{Name: "x", Label: "x"}))
	if strings.Contains(off, "data-hui-range-slider-output") {
		t.Errorf("default ShowValue=false should NOT emit the output:\n%s", off)
	}
}

func TestRangeSliderModuleMarkersPaired(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{Name: "x", Label: "x", ID: "rs1"}))
	if !strings.Contains(h, `data-hui-range-slider-low=""`) || !strings.Contains(h, `data-hui-range-slider-high=""`) {
		t.Errorf("each thumb should carry its own hook:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-range-slider=""`) {
		t.Errorf("the pair should carry its root hook the module scopes by:\n%s", h)
	}
}

// ExtraAttrs land on the root element but never override what the
// component owns (#262): role and aria-label keep framework values, and
// a spoofed range hook is dropped by the data-hui refusal.
func TestRangeSliderExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{
		Name: "price", Label: "Price", Class: "mine",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "role": "evil", "aria-label": "evil", "Class": "evil",
			"data-hui-range-slider": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `role="group"`, `aria-label="Price"`, `class="fui-range-slider mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root missing %q:\n%s", want, root)
		}
	}
}
