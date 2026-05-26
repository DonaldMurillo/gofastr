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

// TestEncodeLongDistanceBackrefDoesNotPanic plants a 4-pixel signature
// near the start of a >1MP image and again near the end, so the LZ77
// match-finder sees a pixel-distance > 1,048,456. Before the fix the
// distance prefix-code symbol overflowed the 40-symbol alphabet and
// the encoder panicked at dFreq[distSym]++ in lz77.go. The fix caps
// matchDist in findMatch so distSym stays in [0, 40).
// TestUniformImageShortCircuitsMultiPass asserts that a solid-color
// image is encoded in a single pass — the multi-pass strategy gains
// nothing on inputs where every predictor mode produces the same
// residual distribution (zero), so we save 4×CPU.
func TestUniformImageShortCircuitsMultiPass(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i+0] = 200
		src.Pix[i+1] = 50
		src.Pix[i+2] = 100
		src.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	lastEncodePasses.Store(0)
	if err := Encode(&buf, src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if p := lastEncodePasses.Load(); p != 1 {
		t.Errorf("uniform image used %d passes, want 1", p)
	}
	// Sanity: still decodes correctly.
	out, err := webp.Decode(&buf)
	if err != nil {
		t.Fatalf("webp.Decode: %v", err)
	}
	assertPixelEqual(t, src, out)
}

// TestDistanceBoundary pins the exact boundary of the distance
// prefix code: matchDist == (1<<20)-120 is the largest value that
// still produces a distSym < 40 (in-range). One more (i.e.
// (1<<20)-119) overflows. The findMatch cap must be set so distSym
// stays in range at any reachable matchDist.
func TestDistanceBoundary(t *testing.T) {
	// (matchDist + 120) → distSym must be < 40.
	for _, d := range []int{1, 100, 1000, (1 << 20) - 121, (1 << 20) - 120} {
		sym, _ := lz77Symbol(uint32(d) + 120)
		if sym >= 40 {
			t.Errorf("matchDist=%d → distSym=%d, want <40", d, sym)
		}
	}
	// One past the boundary must overflow — this confirms our cap is
	// EXACTLY right (not off by one in either direction).
	if sym, _ := lz77Symbol(uint32((1<<20)-119) + 120); sym < 40 {
		t.Errorf("matchDist=(1<<20)-119 should overflow distSym; got %d", sym)
	}
}

func TestEncodeLongDistanceBackrefDoesNotPanic(t *testing.T) {
	const w, h = 1100, 1100 // 1.21M pixels — past the 2^20 boundary
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Fill with a slow gradient so LZ77 doesn't trivially short-match;
	// it has to reach across the image for the planted signature.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x), G: uint8(y), B: 64, A: 255,
			})
		}
	}
	// Planted signature at offsets 0..3 and (w*h - 4)..(w*h - 1). The
	// distance between them is w*h - 4 = 1,209,996 — past the 1,048,576
	// limit of the 40-symbol distance alphabet.
	sig := []color.NRGBA{
		{R: 0xCA, G: 0xFE, B: 0xBA, A: 0xBE},
		{R: 0xDE, G: 0xAD, B: 0xBE, A: 0xEF},
		{R: 0xFA, G: 0xCE, B: 0xB0, A: 0x0C},
		{R: 0xC0, G: 0x1D, B: 0xBE, A: 0xEF},
	}
	for i, c := range sig {
		src.SetNRGBA(i, 0, c)
		src.SetNRGBA((w*h-4+i)%w, (w*h-4+i)/w, c)
	}

	var buf bytes.Buffer
	if err := Encode(&buf, src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := webp.Decode(&buf)
	if err != nil {
		t.Fatalf("webp.Decode: %v", err)
	}
	// Spot-check the signature pixels round-tripped byte-exact.
	for i, want := range sig {
		got := color.NRGBAModel.Convert(out.At(i, 0)).(color.NRGBA)
		if got != want {
			t.Errorf("front sig[%d] = %v, want %v", i, got, want)
		}
		bx, by := (w*h-4+i)%w, (w*h-4+i)/w
		got = color.NRGBAModel.Convert(out.At(bx, by)).(color.NRGBA)
		if got != want {
			t.Errorf("back sig[%d] = %v, want %v", i, got, want)
		}
	}
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

func TestCodeLengthsSatisfyKraft(t *testing.T) {
	// Skewed distribution that historically pushed lengths past 15.
	freq := make([]int, 256)
	for i := range freq {
		freq[i] = 1
	}
	freq[0] = 10000
	freq[1] = 5000
	freq[2] = 2500
	lens := codeLengths(freq)
	max := 0
	var kraft float64
	for _, l := range lens {
		if l > max {
			max = l
		}
		if l > 0 {
			kraft += 1.0 / float64(uint64(1)<<uint(l))
		}
	}
	if max > maxCodeLength {
		t.Errorf("max length %d exceeds %d", max, maxCodeLength)
	}
	if kraft > 1.0+1e-9 {
		t.Errorf("Kraft violation: sum 2^-len = %f > 1", kraft)
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
