package ui

import (
	"strings"
	"testing"
)

func TestColorPickerRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ColorPicker without Name should panic")
		}
	}()
	ColorPicker(ColorPickerConfig{Label: "x"})
}

func TestColorPickerRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ColorPicker without Label should panic")
		}
	}()
	ColorPicker(ColorPickerConfig{Name: "x"})
}

func TestColorPickerEmitsColorInput(t *testing.T) {
	h := string(ColorPicker(ColorPickerConfig{Name: "c", Label: "Color", Value: "#FF0000"}))
	if !strings.Contains(h, `type="color"`) {
		t.Errorf("expected type=color:\n%s", h)
	}
	if !strings.Contains(h, `value="#FF0000"`) {
		t.Errorf("expected initial value attr:\n%s", h)
	}
}

func TestColorPickerLabelAssociatedWithInput(t *testing.T) {
	h := string(ColorPicker(ColorPickerConfig{Name: "brand", Label: "Brand"}))
	if !strings.Contains(h, `for="brand"`) {
		t.Errorf("label[for] should default to Name:\n%s", h)
	}
	if !strings.Contains(h, `id="brand"`) {
		t.Errorf("input[id] should default to Name:\n%s", h)
	}
}

func TestColorPickerDisabledClassAndAttr(t *testing.T) {
	h := string(ColorPicker(ColorPickerConfig{Name: "c", Label: "x", Disabled: true}))
	if !strings.Contains(h, "is-disabled") {
		t.Errorf("Disabled should add .is-disabled wrapper class:\n%s", h)
	}
	if !strings.Contains(h, "disabled") {
		t.Errorf("Disabled should add disabled attr on input:\n%s", h)
	}
}
