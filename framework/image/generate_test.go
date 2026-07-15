package image

import "testing"

func TestNewGradientCornersMatchStops(t *testing.T) {
	img, err := NewGradient(64, 64, "#FF0000", "#0000FF")
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("bounds %v", b)
	}
	r, _, _, _ := img.GoImage().At(0, 0).RGBA()
	if r>>8 < 240 {
		t.Errorf("top-left should be ~from stop (red), got r=%d", r>>8)
	}
	_, _, bl, _ := img.GoImage().At(63, 63).RGBA()
	if bl>>8 < 240 {
		t.Errorf("bottom-right should be ~to stop (blue), got b=%d", bl>>8)
	}
}

func TestNewGradientRejectsBadInput(t *testing.T) {
	if _, err := NewGradient(0, 64, "#FF0000", "#0000FF"); err == nil {
		t.Errorf("zero width must error")
	}
	if _, err := NewGradient(64, 64, "red", "#0000FF"); err == nil {
		t.Errorf("non-hex stop must error")
	}
	if _, err := NewGradient(64, 64, "#F00", "#0000FF"); err == nil {
		t.Errorf("short hex must error")
	}
}
