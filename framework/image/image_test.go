package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// solidRGBA returns an RGBA image of size w×h filled with c.
func solidRGBA(w, h int, c color.RGBA) *stdimage.RGBA {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// gradient returns an RGB gradient image (red varies on x, green on y).
func gradient(w, h int) *stdimage.RGBA {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / max1(w-1)),
				G: uint8(y * 255 / max1(h-1)),
				B: 64,
				A: 255,
			})
		}
	}
	return img
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// encodePNG returns a PNG-encoded buffer for use as a Decode input.
func encodePNG(t *testing.T, img stdimage.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img stdimage.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func TestSniffDetectsEachFormat(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want Format
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, FormatJPEG},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00"), FormatPNG},
		{"gif87", []byte("GIF87a\x00\x00\x00\x00\x00\x00"), FormatGIF},
		{"gif89", []byte("GIF89a\x00\x00\x00\x00\x00\x00"), FormatGIF},
		{"bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), FormatBMP},
		{"tiff-le", []byte{0x49, 0x49, 0x2A, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}, FormatTIFF},
		{"tiff-be", []byte{0x4D, 0x4D, 0x00, 0x2A, 0, 0, 0, 0, 0, 0, 0, 0}, FormatTIFF},
		{"webp", append(append([]byte("RIFF"), 0, 0, 0, 0), []byte("WEBP")...), FormatWebP},
		{"unknown", []byte("not an image"), FormatUnknown},
		{"short", []byte{0xFF}, FormatUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sniff(c.data); got != c.want {
				t.Fatalf("Sniff = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDecodeBytesRoundTripPNG(t *testing.T) {
	data := encodePNG(t, gradient(32, 24))
	img, err := DecodeBytes(data)
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}
	if img.Format() != FormatPNG {
		t.Fatalf("Format = %v, want PNG", img.Format())
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 24 {
		t.Fatalf("Bounds = %v, want 32×24", b)
	}
}

func TestDecodeBombGuardTrips(t *testing.T) {
	data := encodePNG(t, solidRGBA(10, 10, color.RGBA{255, 0, 0, 255}))
	_, err := DecodeBytesWithConfig(data, Config{MaxPixels: 50})
	if err == nil || !strings.Contains(err.Error(), "decompression bomb") {
		t.Fatalf("err = %v, want decompression bomb", err)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := DecodeBytes([]byte("not an image at all")); err == nil {
		t.Fatal("expected error")
	}
}

func TestResizePreservesAspectWithHeightZero(t *testing.T) {
	src := FromImage(gradient(100, 50), FormatPNG)
	out := src.Resize(40, 0)
	if b := out.Bounds(); b.Dx() != 40 || b.Dy() != 20 {
		t.Fatalf("Bounds = %v, want 40×20", b)
	}
}

func TestResizeFitInside(t *testing.T) {
	// 100×50 fitted inside 60×60 → uniform scale by min(0.6, 1.2) = 0.6 → 60×30.
	src := FromImage(gradient(100, 50), FormatPNG)
	out := src.Resize(60, 60, WithFit(FitInside))
	if b := out.Bounds(); b.Dx() != 60 || b.Dy() != 30 {
		t.Fatalf("Bounds = %v, want 60×30", b)
	}
}

func TestResizeWithoutEnlargementNoOp(t *testing.T) {
	src := FromImage(gradient(50, 50), FormatPNG)
	out := src.Resize(200, 200, WithoutEnlargement())
	if b := out.Bounds(); b.Dx() != 50 || b.Dy() != 50 {
		t.Fatalf("Bounds = %v, want 50×50 unchanged", b)
	}
}

func TestRotate90Dimensions(t *testing.T) {
	src := FromImage(gradient(20, 10), FormatPNG)
	out := src.Rotate(90)
	if b := out.Bounds(); b.Dx() != 10 || b.Dy() != 20 {
		t.Fatalf("Bounds = %v, want 10×20", b)
	}
}

func TestRotate180SamePixels(t *testing.T) {
	src := solidRGBA(4, 4, color.RGBA{128, 64, 32, 255})
	out := FromImage(src, FormatPNG).Rotate(180)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if got := out.GoImage().At(x, y); !sameColor(got, src.At(3-x, 3-y)) {
				t.Fatalf("pixel (%d,%d) mismatch", x, y)
			}
		}
	}
}

func TestFlipFlopReturnsToSource(t *testing.T) {
	src := gradient(8, 6)
	wrapped := FromImage(src, FormatPNG)
	round := wrapped.Flip().Flip().Flop().Flop()
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			if !sameColor(round.GoImage().At(x, y), src.At(x, y)) {
				t.Fatalf("pixel (%d,%d) mismatch after flip/flop roundtrip", x, y)
			}
		}
	}
}

func TestModulateZeroIsIdentity(t *testing.T) {
	src := gradient(8, 8)
	out := FromImage(src, FormatPNG).Modulate(Modulation{})
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if !sameColor(out.GoImage().At(x, y), src.At(x, y)) {
				t.Fatalf("Modulate zero changed pixel (%d,%d)", x, y)
			}
		}
	}
}

func TestModulateGrayscaleLiteralZero(t *testing.T) {
	// Saturation=0 should produce literal grayscale per the doc.
	// Before the fix the encoder coerced 0 → 1 and silently returned
	// identity. The test asserts R=G=B exactly for every pixel.
	src := FromImage(gradient(4, 4), FormatPNG).
		Modulate(Modulation{Saturation: Float64(0)})
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			r, g, b, _ := src.GoImage().At(x, y).RGBA()
			if r != g || g != b {
				t.Fatalf("Saturation:0 should be grayscale at (%d,%d), got R=%d G=%d B=%d", x, y, r>>8, g>>8, b>>8)
			}
		}
	}
}

// TestModulateNaNNoOps pins the behaviour for NaN inputs: NaN is
// programmer error (typically int(NaN)=0 or float-from-broken-config)
// and historically slipped through clamp8 to produce silent black.
// The fix treats NaN as nil — return source unchanged.
func TestModulateNaNNoOps(t *testing.T) {
	src := gradient(8, 8)
	out := FromImage(src, FormatPNG).Modulate(Modulation{
		Brightness: Float64(math.NaN()),
		Saturation: Float64(math.NaN()),
	})
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if !sameColor(out.GoImage().At(x, y), src.At(x, y)) {
				t.Fatalf("NaN modulation changed pixel (%d,%d)", x, y)
			}
		}
	}
}

func TestModulateGrayscale(t *testing.T) {
	// Saturation=ε makes R=G=B for every pixel.
	src := FromImage(gradient(4, 4), FormatPNG).
		Modulate(Modulation{Saturation: Float64(0.0001)})
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			r, g, b, _ := src.GoImage().At(x, y).RGBA()
			diff := int(r) - int(g)
			if diff < -256 || diff > 256 || int(g)-int(b) < -256 || int(g)-int(b) > 256 {
				t.Fatalf("expected near-grayscale at (%d,%d), got %d/%d/%d", x, y, r>>8, g>>8, b>>8)
			}
		}
	}
}

func TestPNGRejectsInvalidCompressionLevel(t *testing.T) {
	src := FromImage(gradient(8, 8), FormatPNG)
	if _, err := src.PNG(PNGOptions{Compression: 99}).Bytes(); err == nil {
		t.Error("expected error for invalid CompressionLevel 99")
	}
	if _, err := src.PNG(PNGOptions{Compression: -4}).Bytes(); err == nil {
		t.Error("expected error for invalid CompressionLevel -4")
	}
	// Valid range is [-3, 0]; each must succeed.
	for _, lvl := range []png.CompressionLevel{png.DefaultCompression, png.NoCompression, png.BestSpeed, png.BestCompression} {
		if _, err := src.PNG(PNGOptions{Compression: lvl}).Bytes(); err != nil {
			t.Errorf("level %d should succeed; got %v", lvl, err)
		}
	}
}

func TestEncodersAllRoundTrip(t *testing.T) {
	src := FromImage(gradient(16, 12), FormatPNG)
	encs := []*Encoder{
		src.JPEG(JPEGOptions{Quality: 90}),
		src.PNG(),
		src.GIF(),
		src.BMP(),
		src.TIFF(),
	}
	for _, e := range encs {
		t.Run(e.MIME(), func(t *testing.T) {
			data, err := e.Bytes()
			if err != nil {
				t.Fatalf("encode %s: %v", e.MIME(), err)
			}
			img, err := DecodeBytes(data)
			if err != nil {
				t.Fatalf("decode %s: %v", e.MIME(), err)
			}
			if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 12 {
				t.Fatalf("decoded bounds %v, want 16×12", img.Bounds())
			}
		})
	}
}

func TestWebPLossyReturnsErrFormatUnsupported(t *testing.T) {
	src := FromImage(gradient(8, 8), FormatPNG)
	_, err := src.WebP(WebPOptions{Lossy: true}).Bytes()
	if err == nil {
		t.Fatal("expected ErrFormatUnsupported")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want format-unsupported message", err)
	}
}

// TestWebPZeroValueOptionsIsLossless asserts that the idiomatic Go
// pattern `Image.WebP(WebPOptions{})` produces a valid lossless WebP.
// Before the fix the zero-value Lossless field was false, which
// errored as "lossy not implemented" — a foot-gun for callers who
// reach for the zero-value struct.
func TestWebPZeroValueOptionsIsLossless(t *testing.T) {
	src := FromImage(gradient(16, 12), FormatPNG)
	data, err := src.WebP(WebPOptions{}).Bytes()
	if err != nil {
		t.Fatalf("WebP(WebPOptions{}): %v", err)
	}
	if Sniff(data) != FormatWebP {
		t.Fatalf("zero-value WebPOptions produced non-WebP bytes")
	}
}

func TestWebPLosslessRoundTrips(t *testing.T) {
	src := FromImage(gradient(24, 16), FormatPNG)
	data, err := src.WebP(WebPOptions{}).Bytes()
	if err != nil {
		t.Fatalf("WebP lossless encode: %v", err)
	}
	if Sniff(data) != FormatWebP {
		t.Fatalf("output is not a WebP: %v", Sniff(data))
	}
	decoded, err := DecodeBytes(data)
	if err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 24 || b.Dy() != 16 {
		t.Errorf("re-decoded bounds = %v, want 24×16", b)
	}
}

// TestEncoderMemoizesTerminalCalls asserts that calling Bytes /
// Base64 / DataURL multiple times on the same Encoder doesn't
// re-encode. Counts invocations via an instrumented encode func.
func TestEncoderMemoizesTerminalCalls(t *testing.T) {
	calls := 0
	src := FromImage(gradient(32, 16), FormatPNG)
	// Build an Encoder using PNG, then wrap encode with a counter.
	pngEnc := src.PNG()
	inner := pngEnc.encode
	pngEnc.encode = func(w io.Writer, img stdimage.Image) error {
		calls++
		return inner(w, img)
	}

	if _, err := pngEnc.Bytes(); err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if _, err := pngEnc.Bytes(); err != nil {
		t.Fatalf("Bytes 2nd: %v", err)
	}
	if _, err := pngEnc.Base64(); err != nil {
		t.Fatalf("Base64: %v", err)
	}
	if _, err := pngEnc.DataURL(); err != nil {
		t.Fatalf("DataURL: %v", err)
	}
	if calls != 1 {
		t.Errorf("encode invoked %d times across 4 terminal calls; want 1", calls)
	}
}

// TestEncoderBytesConcurrentSafe must pass under `go test -race`.
// Before the sync.Once fix the cached/cachedErr/cachedSet fields
// raced; under -race the test would fail. Calling Bytes() from N
// goroutines must return byte-identical slices and invoke the codec
// exactly once.
func TestEncoderBytesConcurrentSafe(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	var calls int32
	enc := src.PNG()
	inner := enc.encode
	enc.encode = func(w io.Writer, img stdimage.Image) error {
		atomic.AddInt32(&calls, 1)
		return inner(w, img)
	}

	const N = 32
	results := make([][]byte, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = enc.Bytes()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < N; i++ {
		if !bytes.Equal(results[0], results[i]) {
			t.Fatalf("goroutine %d produced different bytes", i)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Errorf("encode invoked %d times under concurrent Bytes(); want 1", c)
	}
}

func TestEncoderDataURL(t *testing.T) {
	src := FromImage(gradient(4, 4), FormatPNG)
	durl, err := src.PNG().DataURL()
	if err != nil {
		t.Fatalf("DataURL: %v", err)
	}
	if !strings.HasPrefix(durl, "data:image/png;base64,") {
		t.Fatalf("data URL prefix wrong: %q", durl[:40])
	}
}

func TestPlaceholderReturnsDataURL(t *testing.T) {
	src := FromImage(gradient(200, 150), FormatPNG)
	durl, err := src.Placeholder()
	if err != nil {
		t.Fatalf("Placeholder: %v", err)
	}
	if !strings.HasPrefix(durl, "data:image/jpeg;base64,") {
		t.Fatalf("placeholder prefix wrong: %q", durl[:40])
	}
	if len(durl) > 4000 {
		t.Fatalf("placeholder unexpectedly large: %d bytes", len(durl))
	}
}

func TestBlurHashSolidRed1x1(t *testing.T) {
	src := FromImage(solidRGBA(4, 4, color.RGBA{255, 0, 0, 255}), FormatPNG)
	hash, err := src.BlurHash(1, 1)
	if err != nil {
		t.Fatalf("BlurHash: %v", err)
	}
	// Expected derivation in image_test.go alongside the test rather than
	// the source, so the reasoning lives next to the assertion.
	const want = "00TI:j"
	if hash != want {
		t.Fatalf("BlurHash = %q, want %q", hash, want)
	}
}

func TestBlurHashSolidRed4x3(t *testing.T) {
	// Note: BlurHash's cosine basis is not orthonormal at low component
	// counts (no +0.5 sample offset), so uniform images produce small
	// nonzero AC factors. We assert structure rather than a specific value
	// here; an end-to-end reference vector is verified separately.
	src := FromImage(solidRGBA(8, 6, color.RGBA{255, 0, 0, 255}), FormatPNG)
	hash, err := src.BlurHash(4, 3)
	if err != nil {
		t.Fatalf("BlurHash: %v", err)
	}
	if len(hash) != 28 {
		t.Fatalf("4×3 BlurHash should be 28 chars, got %d (%q)", len(hash), hash)
	}
	// First char encodes the size flag: (4-1) + (3-1)*9 = 21 → 'L'.
	if hash[0] != 'L' {
		t.Fatalf("size-flag char = %q, want 'L'", hash[0])
	}
	for _, c := range []byte(hash) {
		if !strings.ContainsRune(base83Alphabet, rune(c)) {
			t.Fatalf("non-base83 character %q in %q", c, hash)
		}
	}
}

func TestBlurHashLengthIsCorrect(t *testing.T) {
	src := FromImage(gradient(32, 24), FormatPNG)
	for _, c := range []struct{ x, y, want int }{
		{1, 1, 6},
		{4, 3, 28},
		{9, 9, 6 + (9*9-1)*2},
	} {
		hash, err := src.BlurHash(c.x, c.y)
		if err != nil {
			t.Fatalf("BlurHash(%d,%d): %v", c.x, c.y, err)
		}
		if len(hash) != c.want {
			t.Fatalf("BlurHash(%d,%d) length %d, want %d", c.x, c.y, len(hash), c.want)
		}
	}
}

// TestBlurHashRejectsTooFewSamples pins the rule: an image with fewer
// pixels than the requested components × components is mathematically
// degenerate (you can't fit M independent DCT components into N<M
// samples). Today the encoder returns a base83-looking string anyway;
// the fix surfaces an error.
func TestBlurHashRejectsTooFewSamples(t *testing.T) {
	src := FromImage(solidRGBA(2, 2, color.RGBA{R: 100, G: 50, B: 200, A: 255}), FormatPNG)
	if _, err := src.BlurHash(4, 3); err == nil {
		t.Error("expected error: 2*2 samples < 4*3 components")
	}
	if _, err := src.BlurHash(2, 2); err != nil {
		t.Errorf("4 samples >= 2*2 components should succeed; got %v", err)
	}
}

func TestBlurHashRejectsBadComponents(t *testing.T) {
	src := FromImage(solidRGBA(4, 4, color.RGBA{0, 0, 0, 255}), FormatPNG)
	if _, err := src.BlurHash(0, 4); err == nil {
		t.Fatal("expected error for xComp=0")
	}
	if _, err := src.BlurHash(4, 10); err == nil {
		t.Fatal("expected error for yComp>9")
	}
}

// TestFromImageHasNoOrientation pins the documented quirk: an Image
// constructed via FromImage carries orient=0, so AutoOrient is a no-
// op. Callers wanting EXIF handling must go through Decode/Open.
// TestAnimatedGIFSurfacesFrameCount asserts that a multi-frame GIF
// surfaces FrameCount > 1 in Metadata so callers can detect the
// "I lost N-1 frames" foot-gun instead of silently decoding only
// the first.
func TestAnimatedGIFSurfacesFrameCount(t *testing.T) {
	// Build a synthetic 2-frame GIF.
	var buf bytes.Buffer
	g := &gif.GIF{LoopCount: 0}
	for i := 0; i < 2; i++ {
		f := stdimage.NewPaletted(stdimage.Rect(0, 0, 4, 4),
			color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{uint8(i * 200), 0, 0, 255}})
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				f.SetColorIndex(x, y, 1)
			}
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 10)
	}
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	img, err := DecodeBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}
	md := img.Metadata()
	if md.FrameCount != 2 {
		t.Errorf("Metadata.FrameCount = %d, want 2", md.FrameCount)
	}
}

func TestFromImageHasNoOrientation(t *testing.T) {
	img := FromImage(gradient(8, 8), FormatJPEG)
	if md := img.Metadata(); md.Orientation != 0 {
		t.Errorf("FromImage orientation = %d, want 0", md.Orientation)
	}
}

func TestMetadataReflectsDimensions(t *testing.T) {
	src := FromImage(gradient(40, 30), FormatPNG)
	m := src.Metadata()
	if m.Width != 40 || m.Height != 30 {
		t.Fatalf("Metadata dims = %d×%d, want 40×30", m.Width, m.Height)
	}
	if m.Format != FormatPNG {
		t.Fatalf("Metadata format = %v, want PNG", m.Format)
	}
}

func TestEXIFOrientationParsedFromJPEG(t *testing.T) {
	// Hand-built minimal JPEG with APP1/Exif orientation=6.
	buf := encodeJPEG(t, solidRGBA(4, 4, color.RGBA{10, 20, 30, 255}), 85)
	withExif := insertExifAPP1(buf, 6)
	img, err := DecodeBytes(withExif)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Metadata().Orientation != 6 {
		t.Fatalf("orientation = %d, want 6", img.Metadata().Orientation)
	}
	rotated := img.AutoOrient()
	// 4×4 source rotated 90 should still be 4×4, but orientation must clear.
	if rotated.Metadata().Orientation != 0 {
		t.Fatalf("AutoOrient should clear orientation")
	}
}

// insertExifAPP1 splices a synthetic APP1 segment carrying just an EXIF
// orientation tag into a JPEG byte stream, right after the SOI marker.
func insertExifAPP1(jpegBytes []byte, orientation int) []byte {
	// TIFF header (little-endian) + 1 IFD entry for orientation + 0 next IFD.
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, // little-endian, magic
		0x08, 0x00, 0x00, 0x00, // IFD0 offset
		0x01, 0x00, // 1 entry
		0x12, 0x01, // tag 0x0112 (orientation)
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		byte(orientation), 0x00, 0x00, 0x00, // value
		0x00, 0x00, 0x00, 0x00, // next IFD offset = 0
	}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(exif) + 2 // include the 2-byte length field itself
	app1 := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}
	app1 = append(app1, exif...)

	out := make([]byte, 0, len(jpegBytes)+len(app1))
	out = append(out, jpegBytes[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, jpegBytes[2:]...)
	return out
}

func sameColor(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
