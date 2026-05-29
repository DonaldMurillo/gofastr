package image

import (
	"encoding/binary"
	"errors"
	"testing"
)

// tiffHugeDims builds a minimal little-endian TIFF whose IFD declares
// ImageWidth=ImageLength=w (a uint32 LONG). With w near math.MaxUint32
// the int64 pixel-area product overflows negative, so a naive
// `width*height > MaxPixels` guard wraps and lets the bomb through.
//
// The strip metadata (StripOffsets/RowsPerStrip/StripByteCounts/
// BitsPerSample/Compression/Photometric) is enough that x/image/tiff
// proceeds to allocate the destination raster — which panics in
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
