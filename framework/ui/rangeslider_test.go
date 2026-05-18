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

func TestRangeSliderSwapsCrossedValues(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{
		Name: "x", Label: "x", Min: 0, Max: 100,
		ValueLow: 80, ValueHigh: 20, // crossed — should auto-swap
	}))
	// After swap: lo=20, hi=80.
	if !strings.Contains(h, `value="20"`) || !strings.Contains(h, `value="80"`) {
		t.Errorf("crossed Low/High should swap into ordered pair:\n%s", h)
	}
}

func TestRangeSliderShowValueAddsMirror(t *testing.T) {
	on := string(RangeSlider(RangeSliderConfig{
		Name: "x", Label: "x", ShowValue: true,
	}))
	if !strings.Contains(on, "data-fui-range-slider-value") {
		t.Errorf("ShowValue should emit data-fui-range-slider-value:\n%s", on)
	}
	if !strings.Contains(on, " – ") {
		t.Errorf("ShowValue mirror should render the initial lo – hi text:\n%s", on)
	}
	off := string(RangeSlider(RangeSliderConfig{Name: "x", Label: "x"}))
	if strings.Contains(off, "data-fui-range-slider-value") {
		t.Errorf("default ShowValue=false should NOT emit mirror:\n%s", off)
	}
}

func TestRangeSliderModuleMarkersPaired(t *testing.T) {
	h := string(RangeSlider(RangeSliderConfig{Name: "x", Label: "x", ID: "rs1"}))
	if c := strings.Count(h, `data-fui-range-slider="rs1"`); c != 2 {
		t.Errorf("both inputs should share data-fui-range-slider=<id>, got %d:\n%s", c, h)
	}
}
