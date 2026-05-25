package vp8l

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/webp"
)

// roundTrip encodes the image, decodes it via x/image/webp, and
// returns the decoded image. Failures along the way abort the test.
func roundTrip(t *testing.T, src image.Image) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := webp.Decode(&buf)
	if err != nil {
		t.Fatalf("webp.Decode: %v", err)
	}
	return out
}

func assertPixelEqual(t *testing.T, want, got image.Image) {
	t.Helper()
	wb, gb := want.Bounds(), got.Bounds()
	if wb.Dx() != gb.Dx() || wb.Dy() != gb.Dy() {
		t.Fatalf("bounds mismatch: want %v, got %v", wb, gb)
	}
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			// Compare via the NRGBA model so RGBA (premultiplied) and
			// NRGBA (straight) sources are unambiguous on both sides.
			w := color.NRGBAModel.Convert(want.At(wb.Min.X+x, wb.Min.Y+y)).(color.NRGBA)
			g := color.NRGBAModel.Convert(got.At(gb.Min.X+x, gb.Min.Y+y)).(color.NRGBA)
			if w != g {
				t.Fatalf("pixel (%d,%d) differs: want %v, got %v", x, y, w, g)
			}
		}
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetRGBA(x, y, c)
		}
	}
	return m
}

func gradient(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / max(1, w-1)),
				G: uint8(y * 255 / max(1, h-1)),
				B: uint8((x + y) * 255 / max(1, w+h-2)),
				A: 255,
			})
		}
	}
	return m
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestEncodeSolidRed(t *testing.T) {
	src := solid(8, 8, color.RGBA{255, 0, 0, 255})
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeSolidWhite(t *testing.T) {
	src := solid(4, 4, color.RGBA{255, 255, 255, 255})
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeSolidTransparent(t *testing.T) {
	src := solid(2, 2, color.RGBA{0, 0, 0, 0})
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeGradientSmall(t *testing.T) {
	src := gradient(16, 12)
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeGradientLarger(t *testing.T) {
	src := gradient(128, 96)
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeWithAlpha(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: 128,
				A: uint8(((x + y) * 32) % 256),
			})
		}
	}
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeTwoColors(t *testing.T) {
	// Two-pixel image: forces every channel to have exactly two used
	// symbols, exercising the simple-code path with nSymbols=2.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{255, 255, 255, 255})
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeThreeColors(t *testing.T) {
	// Three-pixel image: forces every channel to have three used
	// symbols, exercising the normal-code path with the smallest
	// possible code-length code.
	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	src.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{128, 128, 128, 255})
	src.SetRGBA(2, 0, color.RGBA{255, 255, 255, 255})
	out := roundTrip(t, src)
	assertPixelEqual(t, src, out)
}

func TestEncodeEmptyErrors(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 0, 0))
	var buf bytes.Buffer
	if err := Encode(&buf, src); err == nil {
		t.Fatal("expected error for empty image")
	}
}

func TestBitWriterRoundsBytes(t *testing.T) {
	bw := &bitWriter{}
	bw.writeBits(0b1, 1)
	bw.writeBits(0b10, 2)
	bw.writeBits(0b101, 3)
	bw.writeBits(0b11, 2)
	// LSB-first per byte: bit 0 = '1' (first writeBits)
	//                     bit 1..2 = '10' LSB-first (value 0b10 → low bit 0, then high bit 1)
	//                     bit 3..5 = '101' LSB-first (value 0b101 → low bit 1, then 0, then 1)
	//                     bit 6..7 = '11' LSB-first (value 0b11 → low bit 1, then high bit 1)
	// Byte (MSB→LSB) = 1 1 1 0 1 1 0 1 = 0xED.
	got := bw.Bytes()
	if len(got) != 1 || got[0] != 0xED {
		t.Fatalf("got %x, want [0xED]", got)
	}
}

func TestBitWriterRevHuffmanCode(t *testing.T) {
	// MSB-first 4-bit code 0b1010 should be written as 0b0101.
	bw := &bitWriter{}
	bw.writeBitsRev(0b1010, 4)
	bw.writeBits(0, 4) // pad to byte boundary
	got := bw.Bytes()
	if len(got) != 1 || got[0] != 0b00000101 {
		t.Fatalf("got %x, want 0x05", got)
	}
}

func TestCanonicalCodesMatchKraft(t *testing.T) {
	// 4-symbol alphabet with lengths (1, 2, 3, 3) — classic Kraft-equality.
	codes := canonicalCodes([]int{1, 2, 3, 3})
	// Expected canonical (MSB-first):
	//   symbol 0: 0
	//   symbol 1: 10
	//   symbol 2: 110
	//   symbol 3: 111
	want := []uint32{0b0, 0b10, 0b110, 0b111}
	for i, w := range want {
		if codes[i] != w {
			t.Errorf("symbol %d code = %b, want %b", i, codes[i], w)
		}
	}
}
