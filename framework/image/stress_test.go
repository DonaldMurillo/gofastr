package image

import (
	"bytes"
	"fmt"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"math"
	"runtime"
	"runtime/debug"
	"testing"
)

// memDelta returns TotalAlloc bytes consumed running fn.
func memDelta(t *testing.T, fn func()) int64 {
	t.Helper()
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100)
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	fn()
	runtime.ReadMemStats(&m1)
	return int64(m1.TotalAlloc) - int64(m0.TotalAlloc)
}

// TestBombDoesNotAllocFullDecode verifies the MaxPixels guard
// short-circuits before image/png decodes the pixels.
func TestBombDoesNotAllocFullDecode(t *testing.T) {
	src := stdimage.NewNRGBA(stdimage.Rect(0, 0, 256, 256))
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	pngBytes := pngBuf.Bytes()

	var err error
	delta := memDelta(t, func() {
		_, err = DecodeBytesWithConfig(pngBytes, Config{MaxPixels: 100})
	})
	if err == nil || err != ErrDecompressionBomb {
		t.Fatalf("expected ErrDecompressionBomb, got %v", err)
	}
	// Decoded pixel buffer would be ≥256*256*4 = 262144 bytes. Anything
	// under ~50K means the buffer was never allocated.
	t.Logf("alloc delta on bomb path: %d bytes (image pixel buffer would be %d)", delta, 256*256*4)
	if delta > 100_000 {
		t.Errorf("bomb path appears to decode the pixel buffer (alloc=%d > 100K)", delta)
	}
}

// TestMalformedDecodes verifies each format's decoder errors gracefully.
func TestMalformedDecodes(t *testing.T) {
	samples := map[string][]byte{
		"png":  {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0xFF, 0xFF, 0xFF, 0xFF},
		"jpeg": {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0xFF, 0xFF, 0xFF, 0xFF},
		"gif":  []byte("GIF89a\x00\x00\xff\xff"),
		"bmp":  []byte("BM\xff\xff\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
		"tiff": {0x49, 0x49, 0x2A, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"webp": []byte("RIFF\xff\xff\x00\x00WEBPVP8L\xff\xff\x00\x00\xff\xff\xff\xff"),
	}
	for name, data := range samples {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("malformed %s panicked: %v", name, r)
				}
			}()
			_, err := DecodeBytes(data)
			if err == nil {
				t.Fatalf("malformed %s: expected error, got nil", name)
			}
		})
	}
}

// TestMultiFrameGIF: animated 2-frame GIF -> does Decode return first
// frame or error?
func TestMultiFrameGIF(t *testing.T) {
	pal := color.Palette{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 0, 255}}
	pal2 := color.Palette{color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 0, 255}}
	f1 := stdimage.NewPaletted(stdimage.Rect(0, 0, 4, 4), pal)
	f2 := stdimage.NewPaletted(stdimage.Rect(0, 0, 4, 4), pal2)
	for i := range f1.Pix {
		f1.Pix[i] = 0
	}
	for i := range f2.Pix {
		f2.Pix[i] = 0
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*stdimage.Paletted{f1, f2},
		Delay: []int{10, 10},
	}); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("animated gif decode: %v", err)
	}
	// What was returned?
	at := img.GoImage().At(0, 0)
	r, g, b, _ := at.RGBA()
	t.Logf("animated GIF first decoded pixel: R=%d G=%d B=%d (red would be ~65535,0,0; green ~0,65535,0)", r, g, b)
}

// TestResizeNegativeBehaviour documents what Resize does with negative
// dimensions. Behaviour observed via Bounds afterwards.
func TestResizeNegativeBehaviour(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 100, 80))
	img := FromImage(src, FormatPNG)
	cases := []struct{ w, h int }{
		{0, 0}, {-1, -1}, {-1, 0}, {0, -1}, {50, -1}, {-1, 50},
		{50, 0}, {0, 50},
	}
	for _, c := range cases {
		out := img.Resize(c.w, c.h)
		b := out.Bounds()
		t.Logf("Resize(%d,%d) -> %dx%d", c.w, c.h, b.Dx(), b.Dy())
	}
}

// TestChain100Allocations measures how much memory 100 chained
// Resize+Rotate(0)+Flip+Flop+Modulate{} calls allocate.
func TestChain100Allocations(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 16, 16))
	img := FromImage(src, FormatPNG)

	delta := memDelta(t, func() {
		for i := 0; i < 100; i++ {
			img = img.Resize(1, 1).Rotate(0).Flip().Flop().Modulate(Modulation{})
		}
	})
	t.Logf("100x chain alloc delta: %d bytes", delta)
}

// TestJPEGQualityExtremes verifies the clamps actually clamp.
func TestJPEGQualityExtremes(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 8, 8))
	img := FromImage(src, FormatJPEG)
	for _, q := range []int{-1, 0, 200, 1_000_000, math.MaxInt32, math.MinInt32} {
		t.Run(fmt.Sprintf("Q=%d", q), func(t *testing.T) {
			data, err := img.JPEG(JPEGOptions{Quality: q}).Bytes()
			if err != nil {
				t.Fatalf("JPEG q=%d: %v", q, err)
			}
			if len(data) == 0 {
				t.Fatalf("JPEG q=%d: zero bytes", q)
			}
		})
	}
}

// TestPNGCompressionInvalid: image/png treats unknown CompressionLevel
// values how? Does the framework guard?
func TestPNGCompressionInvalid(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 8, 8))
	img := FromImage(src, FormatPNG)
	_, err := img.PNG(PNGOptions{Compression: png.CompressionLevel(99)}).Bytes()
	t.Logf("PNG Compression=99 err=%v", err)
}

// TestBlurHashRanges covers the documented domain.
func TestBlurHashRanges(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 0, A: 255})
		}
	}
	img := FromImage(src, FormatPNG)
	if _, err := img.BlurHash(1, 1); err != nil {
		t.Errorf("BlurHash(1,1): %v", err)
	}
	if _, err := img.BlurHash(9, 9); err != nil {
		t.Errorf("BlurHash(9,9): %v", err)
	}
	if _, err := img.BlurHash(10, 9); err == nil {
		t.Errorf("BlurHash(10,9) should error")
	}
	if _, err := img.BlurHash(0, 0); err == nil {
		t.Errorf("BlurHash(0,0) should error")
	}
	if _, err := img.BlurHash(-1, 5); err == nil {
		t.Errorf("BlurHash(-1,5) should error")
	}

	// 1px image with (4,3) components is rejected (too few samples).
	one := FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, 1, 1)), FormatPNG)
	if _, err := one.BlurHash(4, 3); err == nil {
		t.Error("BlurHash(4,3) on 1×1 image should error (not enough samples)")
	}
	// 1px image with (1, 1) components is the minimum valid case.
	if _, err := one.BlurHash(1, 1); err != nil {
		t.Errorf("BlurHash(1,1) on 1×1: %v", err)
	}
}

// TestVariantSet100 sanity check.
func TestVariantSet100(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 2), B: 64, A: 255})
		}
	}
	img := FromImage(src, FormatPNG)
	variants := make([]Variant, 100)
	for i := range variants {
		variants[i] = Variant{Width: 16 + (i%50)*2, Format: FormatPNG}
	}
	set := VariantSet{Variants: variants}

	var res VariantResult
	var err error
	delta := memDelta(t, func() {
		res, err = set.Process(img)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Variants) != 100 {
		t.Errorf("got %d variants, want 100", len(res.Variants))
	}
	t.Logf("100-variant alloc delta: %d bytes (avg %d/variant)", delta, delta/100)
}

// TestVariantQualityExtremes verifies negative/huge/NaN-cast quality.
func TestVariantQualityExtremes(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	img := FromImage(src, FormatJPEG)
	naCast := int(math.NaN()) // platform-specific but a valid int
	set := VariantSet{Variants: []Variant{
		{Width: 16, Format: FormatJPEG, Quality: -1},
		{Width: 16, Format: FormatJPEG, Quality: 110},
		{Width: 16, Format: FormatJPEG, Quality: naCast},
		{Width: 16, Format: FormatJPEG, Quality: math.MinInt32},
	}}
	res, err := set.Process(img)
	if err != nil {
		t.Fatal(err)
	}
	for i, vo := range res.Variants {
		t.Logf("variant Q-case %d: %d bytes", i, len(vo.Bytes))
		if len(vo.Bytes) == 0 {
			t.Errorf("variant %d: zero bytes", i)
		}
	}
}

// TestSinkDoesNotRead — sink returns nil without reading. ProcessTo
// reuses one shared bytes.Buffer; second variant overwrites first's bytes.
func TestSinkDoesNotRead(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	img := FromImage(src, FormatPNG)
	set := VariantSet{Variants: []Variant{
		{Width: 16, Format: FormatPNG},
		{Width: 24, Format: FormatPNG},
	}}
	calls := 0
	_, err := set.ProcessTo(img, func(h VariantHeader, r io.Reader) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 sink calls, got %d", calls)
	}
}

// TestSinkReads1Byte — does the framework recover when sink reads
// only the first byte of variant 1 and then returns nil? Variant 2
// should still get a fully-written buffer.
func TestSinkReads1Byte(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	img := FromImage(src, FormatPNG)
	set := VariantSet{Variants: []Variant{
		{Width: 16, Format: FormatPNG},
		{Width: 24, Format: FormatPNG},
	}}
	var firstByteOf []byte
	_, err := set.ProcessTo(img, func(h VariantHeader, r io.Reader) error {
		var b [1]byte
		if _, err := r.Read(b[:]); err != nil && err != io.EOF {
			return err
		}
		firstByteOf = append(firstByteOf, b[0])
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	t.Logf("first byte per variant: %v (PNG magic byte is 0x89)", firstByteOf)
	for i, b := range firstByteOf {
		if b != 0x89 {
			t.Errorf("variant %d first byte = 0x%02x, want 0x89 (PNG magic) — buffer reuse leaked content?", i, b)
		}
	}
}

// TestSinkPanics — does ProcessTo propagate panics? (no recover in
// framework — so yes.) Just verify the test framework sees a panic.
func TestSinkPanics(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	img := FromImage(src, FormatPNG)
	set := VariantSet{Variants: []Variant{{Width: 16, Format: FormatPNG}}}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate")
		} else {
			t.Logf("panic propagated as expected: %v", r)
		}
	}()
	_, _ = set.ProcessTo(img, func(h VariantHeader, r io.Reader) error {
		panic("sink go boom")
	})
}

// TestVariantSetWidthLargerThanSourceUpscales documents the "no
// without-enlargement guard" behavior of VariantSet.
func TestVariantSetWidthLargerThanSourceUpscales(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 16, 16))
	img := FromImage(src, FormatPNG)
	set := VariantSet{Variants: []Variant{{Width: 2048, Format: FormatPNG}}}
	res, err := set.Process(img)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Variant width=2048 from src=16x16 produced %dx%d (%d bytes) — upscaled blindly", res.Variants[0].Width, res.Variants[0].Height, len(res.Variants[0].Bytes))
}
