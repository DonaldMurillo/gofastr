package image

import (
	"image/color"
	"testing"
)

// TestOrientationAfterFillBytes: JPEG allows any number of 0xFF fill
// bytes before a marker (ITU T.81 B.1.1.2), and image/jpeg skips them.
// The orientation walk read the second 0xFF as the marker code, took
// the next two bytes as a length, and lost the APP1 behind it.
func TestOrientationAfterFillBytes(t *testing.T) {
	t.Parallel()
	plain := encodeJPEG(t, solidRGBA(4, 4, color.RGBA{10, 20, 30, 255}), 85)
	for _, fill := range []int{1, 3} {
		withExif := insertExifAPP1(plain, 6)
		// Splice fill bytes between SOI and the APP1 marker.
		padded := append([]byte{}, withExif[:2]...)
		for range fill {
			padded = append(padded, 0xFF)
		}
		padded = append(padded, withExif[2:]...)
		if _, err := DecodeBytes(padded); err != nil {
			t.Fatalf("setup broken: padded JPEG does not decode: %v", err)
		}
		if got := readJPEGOrientation(padded); got != 6 {
			t.Errorf("%d fill byte(s): orientation = %d, want 6", fill, got)
		}
	}
}
