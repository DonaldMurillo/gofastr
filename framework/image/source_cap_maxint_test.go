package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

// MaxSourceBytes: math.MaxInt64 reads as "no practical cap", but
// DecodeWithConfig read through io.LimitReader(r, limit+1), and the +1
// wrapped to MinInt64: LimitReader read nothing, so every image failed to
// decode as an empty source. The cap now stays put at MaxInt64 (no
// source can be longer anyway).
func TestDecodeMaxIntSourceCapStillReads(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeWithConfig(bytes.NewReader(buf.Bytes()), Config{MaxSourceBytes: math.MaxInt64})
	if err != nil {
		t.Fatalf("decode with MaxSourceBytes = MaxInt64: %v", err)
	}
	if b := img.img.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("decoded bounds %v, want 2x2", b)
	}
}
