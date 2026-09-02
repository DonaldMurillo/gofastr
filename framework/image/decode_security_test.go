package image

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"
)

// tiffHugeDims builds a minimal little-endian TIFF whose IFD declares
// ImageWidth=ImageLength=w (a uint32 LONG). With w near math.MaxUint32
// the int64 pixel-area product overflows negative, so a naive
// `width*height > MaxPixels` guard wraps and lets the bomb through.
//
// The strip metadata (StripOffsets/RowsPerStrip/StripByteCounts/
// BitsPerSample/Compression/Photometric) is enough that x/image/tiff
// proceeds to allocate the destination raster, which panics in
// image.NewGray on the huge rectangle when the guard is bypassed.
func tiffHugeDims(w uint32) []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}
	entries := []entry{
		{0x0100, 4, 1, w}, // ImageWidth (LONG)
		{0x0101, 4, 1, w}, // ImageLength (LONG)
		{0x0102, 3, 1, 1}, // BitsPerSample (SHORT)
		{0x0103, 3, 1, 1}, // Compression = none
		{0x0106, 3, 1, 1}, // PhotometricInterpretation = BlackIsZero
		{0x0111, 4, 1, 8}, // StripOffsets -> offset 8 (into the file)
		{0x0116, 4, 1, w}, // RowsPerStrip
		{0x0117, 4, 1, 1}, // StripByteCounts
	}
	const headerLen = 8
	ifdOffset := uint32(headerLen)
	buf := make([]byte, 0, 256)
	// Header: "II" + 42 + IFD offset.
	buf = append(buf, 'I', 'I', 42, 0)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, ifdOffset)
	buf = append(buf, tmp...)
	// IFD: entry count (SHORT) + 12 bytes per entry + 4-byte next-IFD.
	cnt := make([]byte, 2)
	binary.LittleEndian.PutUint16(cnt, uint16(len(entries)))
	buf = append(buf, cnt...)
	for _, e := range entries {
		eb := make([]byte, 12)
		binary.LittleEndian.PutUint16(eb[0:], e.tag)
		binary.LittleEndian.PutUint16(eb[2:], e.typ)
		binary.LittleEndian.PutUint32(eb[4:], e.count)
		binary.LittleEndian.PutUint32(eb[8:], e.value)
		buf = append(buf, eb...)
	}
	buf = append(buf, 0, 0, 0, 0) // next IFD = 0
	return buf
}

// TestBombGuardRejectsOverflowDims asserts the decompression-bomb guard
// holds even when the declared dimensions are large enough that the
// int64 pixel-area product would overflow, and that malformed/oversized
// geometry surfaces as an error rather than a panic.
func TestBombGuardRejectsOverflowDims(t *testing.T) {
	cases := []struct {
		name string
		w    uint32
	}{
		{"max_uint32", 0xFFFFFFFF}, // product overflows int64 negative
		{"sqrt_overflow", 1 << 16}, // 65536*65536 = 2^32 > 64MP, no overflow
		{"just_over_cap", 1 << 13}, // 8192*8192 = 64MP exactly; +1 each dim trips
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decode panicked instead of erroring: %v", r)
				}
			}()
			_, err := DecodeBytes(tiffHugeDims(c.w))
			if err == nil {
				t.Fatalf("oversized TIFF (%dx%d) decoded without error", c.w, c.w)
			}
			// Must fail closed as a bomb / invalid input, never succeed.
			if !errors.Is(err, ErrDecompressionBomb) && !errors.Is(err, ErrInvalidInput) {
				t.Logf("rejected with: %v", err)
			}
		})
	}
}

// TestDecodePanicBecomesError feeds crafted bytes that drive the stdlib
// codec into a panic path and asserts the package converts it to an
// error instead of crashing the caller's goroutine.
func TestDecodePanicBecomesError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DecodeBytes leaked a panic to the caller: %v", r)
		}
	}()
	// A TIFF that bypasses (or, post-fix, is caught by) the area guard
	// and would otherwise panic in image.NewGray.
	if _, err := DecodeBytes(tiffHugeDims(0xFFFFFFFF)); err == nil {
		t.Fatal("expected error for panic-inducing input, got nil")
	}
}

// jpegHugeDims builds a minimal JPEG (SOI + SOF0 + SOS + EOI) whose
// frame header declares w×h. SOF0 carries 16-bit dimensions, so
// 65535×65535 (~4.3 GP) is the format's ceiling and clears the 64 MP
// cap without any arithmetic overflow. The SOS marker is required:
// image/jpeg's DecodeConfig parses up to it.
func jpegHugeDims(w, h uint16) []byte {
	return []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xC0, 0x00, 0x0B, // SOF0, len 11 (1 component)
		0x08,                  // sample precision
		byte(h >> 8), byte(h), // height
		byte(w >> 8), byte(w), // width
		0x01,             // 1 component
		0x01, 0x11, 0x00, // component id, sampling, quant table
		0xFF, 0xDA, 0x00, 0x08, // SOS, len 8
		0x01, 0x01, 0x00, // 1 component selector, DC/AC table 0
		0x00, 0x3F, 0x00, // spectral selection, approximation
		0xFF, 0xD9, // EOI
	}
}

// gifHugeDims builds a header-only GIF whose Logical Screen Descriptor
// declares w×h, followed by the block trailer.
func gifHugeDims(w, h uint16) []byte {
	b := []byte("GIF89a")
	b = binary.LittleEndian.AppendUint16(b, w)
	b = binary.LittleEndian.AppendUint16(b, h)
	b = append(b, 0, 0, 0) // packed, bg index, aspect
	b = append(b, 0x3B)    // trailer
	return b
}

// pngHugeDims builds a signature + IHDR with a valid CRC declaring w×h.
func pngHugeDims(w, h uint32) []byte {
	ihdr := []byte{
		'I', 'H', 'D', 'R',
		byte(w >> 24), byte(w >> 16), byte(w >> 8), byte(w),
		byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h),
		8, 6, 0, 0, 0, // depth 8, RGBA, defaults
	}
	out := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, ihdr...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(ihdr))
}

// bmpHugeDims builds a 54-byte BITMAPINFOHEADER BMP declaring w×h,
// 24 bpp, no compression.
func bmpHugeDims(w, h int32) []byte {
	b := []byte("BM")
	b = binary.LittleEndian.AppendUint32(b, 54) // file size (unused by decoder)
	b = append(b, 0, 0, 0, 0)                   // reserved
	b = binary.LittleEndian.AppendUint32(b, 54) // pixel data offset
	b = binary.LittleEndian.AppendUint32(b, 40) // DIB header size
	b = binary.LittleEndian.AppendUint32(b, uint32(w))
	b = binary.LittleEndian.AppendUint32(b, uint32(h))
	b = binary.LittleEndian.AppendUint16(b, 1)  // planes
	b = binary.LittleEndian.AppendUint16(b, 24) // bpp
	b = binary.LittleEndian.AppendUint32(b, 0)  // BI_RGB
	// image size, x/y pixels-per-metre, colours used, colours important.
	b = append(b, make([]byte, 20)...)
	return b
}

// webpContainer wraps body in a RIFF/WEBP container under fourcc.
func webpContainer(fourcc string, body []byte) []byte {
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+len(body)))
	out = append(out, "WEBP"...)
	out = append(out, fourcc...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

// webpVP8LHugeDims builds a RIFF/WEBP container whose VP8L chunk
// header declares w×h (14-bit-per-axis lossless, 16384 is the ceiling).
func webpVP8LHugeDims(w, h int) []byte {
	wm1, hm1 := uint32(w-1), uint32(h-1)
	body := []byte{0x2F}
	body = append(body,
		byte(wm1),
		byte(wm1>>8)|byte(hm1<<6),
		byte(hm1>>2),
		byte(hm1>>10),
	)
	return webpContainer("VP8L", body)
}

// webpVP8XHugeCanvas builds a RIFF/WEBP container with only a VP8X
// chunk declaring a w×h canvas (24-bit-per-axis).
func webpVP8XHugeCanvas(w, h int) []byte {
	wm1, hm1 := uint32(w-1), uint32(h-1)
	body := []byte{0x00, 0, 0, 0} // flags: no alpha, no extras
	body = append(body, byte(wm1), byte(wm1>>8), byte(wm1>>16))
	body = append(body, byte(hm1), byte(hm1>>8), byte(hm1>>16))
	return webpContainer("VP8X", body)
}

// TestBombGuardCoversEverySniffedFormat pins the decode-side bomb guard
// across EVERY format Sniff admits, not just the PNG/TIFF shapes the
// earlier tests crafted. Property: a container header that declares an
// over-cap canvas is rejected by the pixel-area guard, on every format,
// before any pixel decode runs. BMP and WebP were added to the decoder
// set after the PNG/TIFF tests existed and had no pinned surface here.
func TestBombGuardCoversEverySniffedFormat(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"jpeg_max_dims", jpegHugeDims(65535, 65535)},
		{"png_max_dims", pngHugeDims(65535, 65535)},
		{"gif_max_dims", gifHugeDims(65535, 65535)},
		{"bmp_65535_square", bmpHugeDims(65535, 65535)},
		{"tiff_plain_over_cap", tiffHugeDims(10_000)},               // 100 MP, no overflow trick
		{"webp_vp8l_max_dims", webpVP8LHugeDims(16384, 16384)},      // 268 MP
		{"webp_vp8x_huge_canvas", webpVP8XHugeCanvas(46340, 46340)}, // 2.1 GP, still ≤ 2^31 so x/image accepts the canvas
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("decode panicked instead of erroring: %v", r)
				}
			}()
			if got := Sniff(c.data); got == FormatUnknown {
				t.Fatalf("fixture does not sniff as a known format (%d bytes)", len(c.data))
			}
			_, err := DecodeBytes(c.data)
			if !errors.Is(err, ErrDecompressionBomb) {
				t.Fatalf("over-cap %s must trip the bomb guard; got err=%v", c.name, err)
			}
		})
	}
}

// TestBombGuardNonPositiveDims pins the pre-area guard: a header that
// declares a non-positive axis is never decoded, on every format. A
// zero axis is the degenerate shape of the same bomb family; some
// codecs reject it inside their own DecodeConfig, the pipeline's
// ErrInvalidInput guard catches the rest. The property is fail-closed.
func TestBombGuardNonPositiveDims(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"gif_zero_logical_screen", gifHugeDims(0, 0)},
		{"png_zero_height", pngHugeDims(64, 0)},
		{"jpeg_zero_height", jpegHugeDims(64, 0)},
		{"bmp_zero_height", bmpHugeDims(64, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sniff(c.data); got == FormatUnknown {
				t.Fatalf("fixture does not sniff as a known format")
			}
			_, err := DecodeBytes(c.data)
			if err == nil {
				t.Fatalf("%s: zero dimension decoded without error", c.name)
			}
		})
	}
}

// TestDecodeTruncatedHeadersNeverPanic pins the recover() fence from
// the other side: every byte-prefix of a crafted header for every
// format must come back as an error, never a panic and never a
// half-decoded image. Truncated container headers are the cheapest
// attacker shape there is.
func TestDecodeTruncatedHeadersNeverPanic(t *testing.T) {
	fulls := [][]byte{
		jpegHugeDims(2, 2),
		pngHugeDims(2, 2),
		gifHugeDims(2, 2),
		bmpHugeDims(2, 2),
		tiffHugeDims(2),
		webpVP8LHugeDims(2, 2),
		webpVP8XHugeCanvas(2, 2),
	}
	for fi, full := range fulls {
		for n := 0; n <= len(full); n++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("format %d prefix %d/%d panicked: %v", fi, n, len(full), r)
					}
				}()
				img, err := DecodeBytes(full[:n])
				if err == nil {
					t.Fatalf("format %d prefix %d/%d decoded successfully", fi, n, len(full))
				}
				if img != nil {
					t.Fatalf("format %d prefix %d/%d returned image alongside error", fi, n, len(full))
				}
			}()
		}
	}
}

// TestWebPCanvasFrameMismatchRejected pins, at this package's boundary,
// the webp canvas/frame defense: a VP8X chunk declaring a small canvas
// followed by a VP8L chunk declaring huge dimensions must never decode
// at the larger geometry. Either the pipeline's bomb guard or the
// codec's own mismatch check refuses the file; which one fires is not
// the contract — "refused, never decoded oversized" is.
func TestWebPCanvasFrameMismatchRejected(t *testing.T) {
	wm1, hm1 := uint32(7), uint32(7)
	vp8x := []byte{0x00, 0, 0, 0, // flags
		byte(wm1), byte(wm1 >> 8), byte(wm1 >> 16),
		byte(hm1), byte(hm1 >> 8), byte(hm1 >> 16),
	}
	vp8l := webpVP8LHugeDims(16384, 16384)
	vp8lBody := vp8l[len(vp8l)-5:]
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+len(vp8x)+8+len(vp8lBody)))
	out = append(out, "WEBP"...)
	out = append(out, "VP8X"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(vp8x)))
	out = append(out, vp8x...)
	out = append(out, "VP8L"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(vp8lBody)))
	out = append(out, vp8lBody...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mismatched webp panicked: %v", r)
		}
	}()
	img, err := DecodeBytes(out)
	if err == nil {
		b := img.Bounds()
		t.Fatalf("mismatched webp decoded at %dx%d without error", b.Dx(), b.Dy())
	}
}
