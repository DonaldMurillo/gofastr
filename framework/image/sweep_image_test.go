package image

import (
	"errors"
	stdimage "image"
	"image/color"
	"io"
	"math"
	"testing"
)

func TestSweepImageHalfBlurHashRefusedBeforeAnyVariant(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	for _, s := range []VariantSet{
		{BlurHashY: 3, Variants: []Variant{{Width: 4, Format: FormatPNG}}},
		{BlurHashX: 4, Variants: []Variant{{Width: 4, Format: FormatPNG}}},
	} {
		calls := 0
		_, err := s.ProcessTo(src, func(VariantHeader, io.Reader) error { calls++; return nil })
		if err == nil || calls != 0 {
			t.Fatalf("BlurHash %dx%d: err=%v, sink calls=%d; want an error before any variant", s.BlurHashX, s.BlurHashY, err, calls)
		}
	}
}

func TestSweepImageUnknownFormatIsInvalidInput(t *testing.T) {
	if _, err := DecodeBytes([]byte("definitely not an image at all")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("garbage: %v, want ErrInvalidInput", err)
	}
}

func TestSweepImageJPEGScanBudgetSaturates(t *testing.T) {
	if got := jpegScanBudget(Config{MaxPixels: math.MaxInt64 / 2}); got != math.MaxInt64 {
		t.Fatalf("budget %d, want MaxInt64", got)
	}
}

func TestSweepImageScanCountSkipsFillBytes(t *testing.T) {
	d := []byte{0xFF, 0xD8, 0xFF, 0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9}
	if got := countJPEGScans(d); got != 1 {
		t.Fatalf("scans %d, want 1", got)
	}
}

func TestSweepImageChainKeepsOrientationAndConfig(t *testing.T) {
	img, err := DecodeBytesWithConfig(insertExifAPP1(encodeJPEG(t, solidRGBA(4, 4, color.RGBA{10, 20, 30, 255}), 85), 6), Config{MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	flipped := img.Flip()
	if got := flipped.Metadata().Orientation; got != 6 {
		t.Fatalf("orientation after Flip %d, want 6", got)
	}
	if err := flipped.checkResize(20, 20); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("400-pixel target after Flip under a 100-pixel cap: %v, want ErrTargetTooLarge", err)
	}
}

func TestSweepImageSniffNeedsAWholeRIFFHeader(t *testing.T) {
	for _, d := range [][]byte{[]byte("RIFF"), []byte("RIFF\x00\x00\x00\x00WAVEfmt ")} {
		if got := Sniff(d); got != FormatUnknown {
			t.Fatalf("Sniff(%q) = %v, want unknown", d, got)
		}
	}
}

func TestSweepImageFormatMIME(t *testing.T) {
	want := map[Format]string{
		FormatJPEG: "image/jpeg", FormatPNG: "image/png", FormatGIF: "image/gif", FormatBMP: "image/bmp",
		FormatTIFF: "image/tiff", FormatWebP: "image/webp", FormatUnknown: "application/octet-stream",
	}
	for f, m := range want {
		if got := f.MIME(); got != m {
			t.Errorf("%v.MIME() = %q, want %q", f, got, m)
		}
	}
}

func distinctPixels(w, h int) *stdimage.RGBA {
	m := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.RGBA{uint8(x*40 + 1), uint8(y*40 + 1), 7, 255})
		}
	}
	return m
}

func assertMapped(t *testing.T, name string, src *stdimage.RGBA, got *Image, to func(x, y int) (int, int)) {
	t.Helper()
	b := src.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dx, dy := to(x, y)
			if got.GoImage().At(dx, dy) != color.Color(src.RGBAAt(x, y)) {
				t.Fatalf("%s: source (%d,%d) not at (%d,%d)", name, x, y, dx, dy)
			}
		}
	}
}

func TestSweepImageOrientationsMapPixels(t *testing.T) {
	const w, h = 3, 2
	src := distinctPixels(w, h)
	want := map[int]func(x, y int) (int, int){
		1: func(x, y int) (int, int) { return x, y },
		2: func(x, y int) (int, int) { return w - 1 - x, y },
		3: func(x, y int) (int, int) { return w - 1 - x, h - 1 - y },
		4: func(x, y int) (int, int) { return x, h - 1 - y },
		5: func(x, y int) (int, int) { return y, x },
		6: func(x, y int) (int, int) { return h - 1 - y, x },
		7: func(x, y int) (int, int) { return h - 1 - y, w - 1 - x },
		8: func(x, y int) (int, int) { return y, w - 1 - x },
	}
	for k, to := range want {
		img := FromImage(src, FormatJPEG)
		img.orient = k
		out := img.AutoOrient()
		if out.Metadata().Orientation != 0 {
			t.Fatalf("orientation %d not cleared", k)
		}
		assertMapped(t, "orientation "+string(rune('0'+k)), src, out, to)
	}
	img := FromImage(src, FormatPNG)
	assertMapped(t, "Rotate(100)", src, img.Rotate(100), want[6])
	assertMapped(t, "Rotate(-90)", src, img.Rotate(-90), want[8])
}

func TestSweepImageWithoutEnlargementOnEitherAxis(t *testing.T) {
	img := FromImage(distinctPixels(10, 10), FormatPNG)
	if got := img.Resize(20, 5, WithoutEnlargement()); got != img {
		t.Fatalf("20x5 from 10x10 without enlargement resized to %v", got.Bounds())
	}
}

func TestSweepImageFitOutsideOverflowIsTooLarge(t *testing.T) {
	tall := FromImage(distinctPixels(1, 16), FormatPNG)
	if err := tall.checkResize(math.MaxInt/4, 1, WithFit(FitOutside)); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("FitOutside covering a MaxInt/4 width: %v, want ErrTargetTooLarge", err)
	}
}

func TestSweepImageResizeOfEmptyImageIsIdentity(t *testing.T) {
	img := FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, 0, 5)), FormatPNG)
	if got := img.Resize(10, 0); got != img {
		t.Fatalf("Resize of a 0x5 image returned %v, want the source", got.Bounds())
	}
}

type opaqueImage struct{ stdimage.Image }

func TestSweepImageModulateFastPathsMatchGeneric(t *testing.T) {
	nrgba := stdimage.NewNRGBA(stdimage.Rect(0, 0, 8, 4))
	rgba := stdimage.NewRGBA(stdimage.Rect(0, 0, 8, 4))
	ycc := stdimage.NewYCbCr(stdimage.Rect(0, 0, 8, 4), stdimage.YCbCrSubsampleRatio444)
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			c := color.NRGBA{uint8(x * 30), uint8(y * 60), uint8(200 - x*20), uint8(255 - y*70)}
			nrgba.SetNRGBA(x, y, c)
			rgba.Set(x, y, c)
			yi, ci := ycc.YOffset(x, y), ycc.COffset(x, y)
			ycc.Y[yi], ycc.Cb[ci], ycc.Cr[ci] = color.RGBToYCbCr(c.R, c.G, c.B)
		}
	}
	m := Modulation{Brightness: Float64(1.3), Saturation: Float64(0.4)}
	for name, src := range map[string]stdimage.Image{"nrgba": nrgba, "rgba": rgba, "ycbcr": ycc} {
		fast := FromImage(src, FormatPNG).Modulate(m).GoImage().(*stdimage.RGBA)
		slow := FromImage(opaqueImage{src}, FormatPNG).Modulate(m).GoImage().(*stdimage.RGBA)
		for i := range fast.Pix {
			if d := int(fast.Pix[i]) - int(slow.Pix[i]); d > 3 || d < -3 {
				t.Fatalf("%s: byte %d fast %d, generic %d", name, i, fast.Pix[i], slow.Pix[i])
			}
		}
	}
}

func TestSweepImageModulateIdentityAndNaN(t *testing.T) {
	img := FromImage(distinctPixels(4, 4), FormatPNG)
	if img.Modulate(Modulation{Brightness: Float64(1), Saturation: Float64(1)}) != img {
		t.Fatal("identity modulation built a new image")
	}
	gray := img.Modulate(Modulation{Saturation: Float64(0)}).GoImage().(*stdimage.RGBA)
	nan := img.Modulate(Modulation{Brightness: Float64(math.NaN()), Saturation: Float64(0)}).GoImage().(*stdimage.RGBA)
	if string(gray.Pix) != string(nan.Pix) {
		t.Fatal("a NaN brightness changed the result of a saturation-only modulation")
	}
}

func TestSweepImageGradientEdges(t *testing.T) {
	if _, err := NewGradient(2, 2, "x112233", "#445566"); err == nil {
		t.Fatal("a stop without # was accepted")
	}
	one, err := NewGradient(1, 1, "#102030", "#ffffff")
	if err != nil || one.GoImage().At(0, 0) != color.Color(color.NRGBA{0x10, 0x20, 0x30, 255}) {
		t.Fatalf("1x1 gradient: %v, %v; want the first stop", err, one)
	}
	mid, err := NewGradient(3, 1, "#000000", "#ff0000")
	if err != nil || mid.GoImage().At(1, 0).(color.NRGBA).R != 128 {
		t.Fatalf("midpoint of #000000 to #ff0000: %v, %v; want red 128", err, mid.GoImage().At(1, 0))
	}
}

func TestSweepImageEncoderOptionDefaultsAndClamps(t *testing.T) {
	img := FromImage(gradient(32, 32), FormatPNG)
	same := func(a, b *Encoder, what string) {
		t.Helper()
		x, err1 := a.Bytes()
		y, err2 := b.Bytes()
		if err1 != nil || err2 != nil || string(x) != string(y) {
			t.Fatalf("%s: errors %v %v, equal %v", what, err1, err2, string(x) == string(y))
		}
	}
	same(img.JPEG(), img.JPEG(JPEGOptions{Quality: 80}), "JPEG default is quality 80")
	same(img.JPEG(JPEGOptions{Quality: 500}), img.JPEG(JPEGOptions{Quality: 100}), "JPEG quality clamps to 100")
	same(img.GIF(GIFOptions{NumColors: 1000}), img.GIF(GIFOptions{NumColors: 256}), "GIF colors clamp to 256")
}

func TestSweepImageBlurHashInvalidLastByte(t *testing.T) {
	if _, err := DecodeBlurHash(refHash[:len(refHash)-1]+"\\", BlurHashRenderConfig{}); err == nil {
		t.Fatal("a hash ending in a byte outside base83 decoded")
	}
}

func TestSweepImageBlurHashKeepsContrast(t *testing.T) {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if x >= 16 {
				src.Set(x, y, color.White)
			} else {
				src.Set(x, y, color.Black)
			}
		}
	}
	hash, err := FromImage(src, FormatPNG).BlurHash(4, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 32, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	l, _, _, _ := got.GoImage().At(2, 0).RGBA()
	r, _, _, _ := got.GoImage().At(29, 0).RGBA()
	if l>>8 > 125 || r>>8 < 222 {
		t.Fatalf("black|white decoded as left %d, right %d", l>>8, r>>8)
	}
}

func TestSweepImageBlurHashReencodesReference(t *testing.T) {
	img, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Width: 64, Height: 64})
	if err != nil {
		t.Fatal(err)
	}
	got, err := img.BlurHash(4, 3)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := decodeBlurHashComponents(got, 1)
	b, _ := decodeBlurHashComponents(refHash, 1)
	worst := 0.0
	for k := range b.factors {
		for c := 0; c < 3; c++ {
			worst = max(worst, math.Abs(a.factors[k][c]-b.factors[k][c]))
		}
	}
	if worst > 0.027 {
		t.Fatalf("re-encoding the decoded reference gave %s, %f from %s", got, worst, refHash)
	}
}

func TestSweepImageBlurHashDataURLDefaultsAndShrink(t *testing.T) {
	t.Cleanup(func() { SetBlurHashCacheSize(defaultBlurHashCacheSize); FlushBlurHashCache() })
	FlushBlurHashCache()
	def, err1 := BlurHashDataURL(refHash, BlurHashRenderConfig{})
	q50, err2 := BlurHashDataURL(refHash, BlurHashRenderConfig{Quality: 50})
	if err1 != nil || err2 != nil || def != q50 {
		t.Fatalf("default render differs from quality 50 (%v, %v)", err1, err2)
	}
	for w := 1; w <= 5; w++ {
		if _, err := BlurHashDataURL(refHash, BlurHashRenderConfig{Width: w}); err != nil {
			t.Fatal(err)
		}
	}
	SetBlurHashCacheSize(2)
	if n := BlurHashCacheLen(); n != 2 {
		t.Fatalf("after shrinking to 2 the cache holds %d", n)
	}
}

func TestSweepImagePlaceholderQuality(t *testing.T) {
	img := FromImage(gradient(64, 64), FormatPNG)
	def, err1 := img.Placeholder()
	q40, err2 := img.Resize(16, 0).JPEG(JPEGOptions{Quality: 40}).DataURL()
	q90, err3 := img.Placeholder(PlaceholderOptions{Quality: 90})
	if err1 != nil || err2 != nil || err3 != nil || def != q40 || q90 == def {
		t.Fatalf("default placeholder is not quality 40, or Quality 90 was ignored (%v %v %v)", err1, err2, err3)
	}
}

func TestSweepImageEmptyBaseNameIsImage(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 255}), FormatPNG)
	res, err := VariantSet{Variants: []Variant{{Width: 4, Format: FormatJPEG}}}.Process(src)
	if err != nil || len(res.Variants) != 1 || res.Variants[0].Name != "image-4.jpg" {
		t.Fatalf("got %+v, %v; want one variant named image-4.jpg", res.Variants, err)
	}
}

func TestSweepImageVariantQualityReachesJPEG(t *testing.T) {
	src := FromImage(gradient(64, 64), FormatPNG)
	res, err := VariantSet{Variants: []Variant{
		{Width: 64, Format: FormatJPEG, Quality: 5, Suffix: "lo"},
		{Width: 64, Format: FormatJPEG, Quality: 100, Suffix: "hi"},
	}}.Process(src)
	if err != nil {
		t.Fatal(err)
	}
	if lo, hi := len(res.Variants[0].Bytes), len(res.Variants[1].Bytes); lo >= hi {
		t.Fatalf("quality 5 gave %d bytes, quality 100 gave %d; want the low quality smaller", lo, hi)
	}
}

func TestSweepImageProcessToRefusesWholeSetUpFront(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 255}), FormatPNG)
	for name, vs := range map[string][]Variant{
		"later target too large": {{Width: 4, Format: FormatPNG, Suffix: "a"}, {Width: 1 << 40, Format: FormatPNG, Suffix: "b"}},
		"later name collides":    {{Width: 4, Format: FormatPNG, Suffix: "a"}, {Width: 6, Format: FormatPNG, Suffix: "a"}},
	} {
		calls := 0
		_, err := VariantSet{Variants: vs, AllowUpscale: true}.ProcessTo(src, func(VariantHeader, io.Reader) error { calls++; return nil })
		if err == nil || calls != 0 {
			t.Fatalf("%s: err=%v, sink calls=%d; want refusal before the first variant", name, err, calls)
		}
	}
}
