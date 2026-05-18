package ui

import (
	"strings"
	"testing"
)

func TestSliderRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Slider without Name should panic")
		}
	}()
	Slider(SliderConfig{Label: "x"})
}

func TestSliderRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Slider without Label should panic")
		}
	}()
	Slider(SliderConfig{Name: "x"})
}

func TestSliderRejectsMinGTEMax(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Slider with Min >= Max should panic")
		}
	}()
	Slider(SliderConfig{Name: "x", Label: "x", Min: 10, Max: 5})
}

func TestSliderEmitsRangeInputWithBounds(t *testing.T) {
	h := string(Slider(SliderConfig{
		Name: "vol", Label: "Volume", Min: 0, Max: 100, Step: 5, Value: 25,
	}))
	if !strings.Contains(h, `type="range"`) {
		t.Errorf("expected type=range:\n%s", h)
	}
	if !strings.Contains(h, `min="0"`) || !strings.Contains(h, `max="100"`) {
		t.Errorf("expected min/max attrs:\n%s", h)
	}
	if !strings.Contains(h, `step="5"`) {
		t.Errorf("expected step=5:\n%s", h)
	}
	if !strings.Contains(h, `value="25"`) {
		t.Errorf("expected initial value=25:\n%s", h)
	}
}

func TestSliderClampsValueOutOfRange(t *testing.T) {
	low := string(Slider(SliderConfig{Name: "x", Label: "x", Min: 10, Max: 90, Value: 5}))
	if !strings.Contains(low, `value="10"`) {
		t.Errorf("Value below Min should clamp to Min:\n%s", low)
	}
	high := string(Slider(SliderConfig{Name: "x", Label: "x", Min: 10, Max: 90, Value: 200}))
	if !strings.Contains(high, `value="90"`) {
		t.Errorf("Value above Max should clamp to Max:\n%s", high)
	}
}

func TestSliderShowValueAddsMirrorMarker(t *testing.T) {
	on := string(Slider(SliderConfig{Name: "x", Label: "x", ShowValue: true}))
	if !strings.Contains(on, "data-fui-slider-mirror") {
		t.Errorf("ShowValue should emit data-fui-slider-mirror:\n%s", on)
	}
	if !strings.Contains(on, `<output `) {
		t.Errorf("ShowValue should emit an <output> element:\n%s", on)
	}
	off := string(Slider(SliderConfig{Name: "x", Label: "x"}))
	if strings.Contains(off, "data-fui-slider-mirror") {
		t.Errorf("default ShowValue=false should NOT emit mirror marker:\n%s", off)
	}
}

func TestSliderHasAriaLabel(t *testing.T) {
	h := string(Slider(SliderConfig{Name: "vol", Label: "Volume"}))
	if !strings.Contains(h, `aria-label="Volume"`) {
		t.Errorf("Slider input should carry aria-label=Label:\n%s", h)
	}
}
