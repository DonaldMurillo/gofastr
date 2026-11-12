package uihost

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// orientedIconJPEG is a 64x64 JPEG whose stored top-left quadrant is red
// (the rest blue), tagged EXIF orientation 6: "rotate 90 CW to display",
// a phone photo. Displayed upright, the red quadrant is top-right.
func orientedIconJPEG(t *testing.T) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			c := color.RGBA{0, 0, 255, 255}
			if x < 32 && y < 32 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, // Orientation = 6
		0x00, 0x00, 0x00, 0x00,
	}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(exif) + 2
	out := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}, exif...)
	return append(out, buf.Bytes()[2:]...)
}

// TestAppIconFollowsOrientation: the icons are PNGs, which carry no
// orientation tag, so the rotation a viewer applies to the source must be
// baked in or every icon ships sideways.
func TestAppIconFollowsOrientation(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(orientedIconJPEG(t)))
	raw := ds.appIcons[appIconPath(32)]
	if raw == nil {
		t.Fatal("no 32px icon generated")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("icon does not decode: %v", err)
	}
	red := func(x, y int) bool {
		r, _, b, _ := img.At(x, y).RGBA()
		return r > b
	}
	if !red(24, 8) || red(8, 8) {
		t.Errorf("icon is not upright: red at top-right=%v top-left=%v, want true/false", red(24, 8), red(8, 8))
	}
}
