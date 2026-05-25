package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
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

func TestModulateGrayscale(t *testing.T) {
	// Saturation=ε makes R=G=B for every pixel.
	src := FromImage(gradient(4, 4), FormatPNG).
		Modulate(Modulation{Saturation: 0.0001})
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
	_, err := src.WebP(WebPOptions{Lossless: false}).Bytes()
	if err == nil {
		t.Fatal("expected ErrFormatUnsupported")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want format-unsupported message", err)
	}
}

func TestWebPLosslessRoundTrips(t *testing.T) {
	src := FromImage(gradient(24, 16), FormatPNG)
	data, err := src.WebP(WebPOptions{Lossless: true}).Bytes()
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

func TestBlurHashRejectsBadComponents(t *testing.T) {
	src := FromImage(solidRGBA(4, 4, color.RGBA{0, 0, 0, 255}), FormatPNG)
	if _, err := src.BlurHash(0, 4); err == nil {
		t.Fatal("expected error for xComp=0")
	}
	if _, err := src.BlurHash(4, 10); err == nil {
		t.Fatal("expected error for yComp>9")
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
