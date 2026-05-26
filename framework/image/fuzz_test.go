package image

import (
	"bytes"
	"testing"
)

// FuzzCountGIFFrames feeds random byte slices through the GIF frame
// counter. The parser walks block markers (0x2C / 0x21 / 0x3B) and
// sub-block length chains; bounds errors here would be reachable from
// any DecodeBytes call on an attacker-controlled GIF. Run via
// `go test -fuzz=FuzzCountGIFFrames -fuzztime=30s ./framework/image/`.
func FuzzCountGIFFrames(f *testing.F) {
	// Seed with a couple of canonical prefixes so the fuzzer starts
	// near real GIF structure.
	f.Add([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00"))
	f.Add([]byte("GIF87a\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		// Just must not panic / hang. The function's contract is
		// "returns frame count ≥ 1, never reads OOB".
		n := countGIFFrames(data)
		if n < 1 {
			t.Errorf("frame count %d < 1", n)
		}
	})
}

// FuzzReadJPEGOrientation walks the EXIF orientation reader over
// random bytes. The parser handles JPEG markers, an APP1 segment, and
// a mini TIFF stream — three nested bounds-check surfaces.
func FuzzReadJPEGOrientation(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	f.Add([]byte{0xFF, 0xD8})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		o := readJPEGOrientation(data)
		if o < 0 || o > 8 {
			t.Errorf("orientation %d out of 0..8", o)
		}
	})
}

// FuzzSniff exercises the format sniffer with random short inputs —
// the function reads up to the first 12 bytes and must never panic
// regardless of input shape.
func FuzzSniff(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte("RIFF\x00\x00\x00\x00WEBP"))
	f.Add([]byte("GIF87a\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		got := Sniff(data)
		if got < FormatUnknown || got > FormatWebP {
			t.Errorf("Sniff returned out-of-range format %v", got)
		}
	})
}

// TestSniffBoundaryShortInputs pins the explicit short-input guard.
// Any input < 12 bytes must return FormatUnknown without indexing
// past the slice.
func TestSniffBoundaryShortInputs(t *testing.T) {
	for n := 0; n < 12; n++ {
		buf := bytes.Repeat([]byte{0xFF}, n)
		if got := Sniff(buf); got != FormatUnknown {
			t.Errorf("Sniff(%d-byte all-0xFF) = %v, want FormatUnknown", n, got)
		}
	}
}
