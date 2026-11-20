// Package pngtest builds small PNG fixtures for the tests of the
// framework's image-adjacent packages (framework/file and
// framework/uihost). Tests only: nothing outside a _test.go file imports
// it, so it never links into a binary.
package pngtest

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// Solid returns PNG bytes of a w×h image filled uniformly with c. Opaque
// colors encode the same bytes whatever premultiplied form the caller's
// color literal carries.
func Solid(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode solid png: %v", err)
	}
	return buf.Bytes()
}
