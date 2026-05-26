package vp8l

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/webp"
)

// FuzzEncode generates random NRGBA images and asserts encode -> decode
// equality. Width/height are clamped to [1,64] so each iteration is fast.
func FuzzEncode(f *testing.F) {
	// Seed corpus.
	f.Add(uint8(1), uint8(1), []byte{0, 0, 0, 255})
	f.Add(uint8(2), uint8(2), []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255})
	f.Add(uint8(8), uint8(8), bytes.Repeat([]byte{128}, 8*8*4))
	f.Add(uint8(3), uint8(1), []byte{0, 0, 0, 255, 128, 128, 128, 255, 255, 255, 255, 255})

	f.Fuzz(func(t *testing.T, wb, hb uint8, data []byte) {
		w := int(wb)%64 + 1
		h := int(hb)%64 + 1
		need := w * h * 4
		if len(data) < need {
			// Pad with zeros so we always have enough bytes.
			pad := make([]byte, need-len(data))
			data = append(append([]byte{}, data...), pad...)
		}
		src := image.NewNRGBA(image.Rect(0, 0, w, h))
		copy(src.Pix, data[:need])

		var buf bytes.Buffer
		if err := Encode(&buf, src); err != nil {
			t.Fatalf("Encode error: %v (w=%d h=%d)", err, w, h)
		}
		out, err := webp.Decode(&buf)
		if err != nil {
			t.Fatalf("webp.Decode failed for valid encode (w=%d h=%d): %v", w, h, err)
		}
		ob := out.Bounds()
		if ob.Dx() != w || ob.Dy() != h {
			t.Fatalf("bounds: got %dx%d want %dx%d", ob.Dx(), ob.Dy(), w, h)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				got := color.NRGBAModel.Convert(out.At(x, y)).(color.NRGBA)
				if want != got {
					t.Fatalf("pixel (%d,%d) mismatch want=%v got=%v (w=%d h=%d)", x, y, want, got, w, h)
				}
			}
		}
	})
}
