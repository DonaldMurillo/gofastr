package image_test

import (
	"bytes"
	"errors"
	stdimage "image"
	"image/jpeg"
	"math/rand/v2"
	"testing"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegtest"
)

// The pixel guard bounded the frame a decode materializes, never how many
// times the decoder walks it. A progressive JPEG may carry any number of
// scans, and an AC scan that is one end-of-band run per 32,767 blocks
// costs about ten bytes while image/jpeg revisits every block of the
// frame: at the 64 MP cap each scan was ~37ms of CPU, so a 10 KB upload of
// 100 scans held a core for four seconds and the cost kept growing with
// the file. Property: decode work is bounded by the pixel cap times a
// scan cap, refused on the bytes before any pixel is decoded.

func TestDecodeRefusesJPEGScanBomb(t *testing.T) {
	bomb := jpegtest.ScanBomb(64, 64, fwimage.DefaultMaxJPEGScans+1)
	// Setup proof: the stdlib decodes the fixture, so a refusal can only
	// be the scan cap, not a malformed stream.
	if _, err := fwimage.DecodeBytesWithConfig(bomb, fwimage.Config{MaxJPEGScans: fwimage.DefaultMaxJPEGScans + 1}); err != nil {
		t.Fatalf("setup broken: fixture with an allowance does not decode: %v", err)
	}
	_, err := fwimage.DecodeBytes(bomb)
	if !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("DecodeBytes of a %d-scan JPEG: %v, want ErrDecompressionBomb", fwimage.DefaultMaxJPEGScans+1, err)
	}
	if _, err := fwimage.Decode(bytes.NewReader(bomb)); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("Decode of a %d-scan JPEG: %v, want ErrDecompressionBomb", fwimage.DefaultMaxJPEGScans+1, err)
	}
}

func TestDecodeAcceptsJPEGAtScanCap(t *testing.T) {
	if _, err := fwimage.DecodeBytes(jpegtest.ScanBomb(64, 64, fwimage.DefaultMaxJPEGScans)); err != nil {
		t.Fatalf("a JPEG with exactly DefaultMaxJPEGScans scans was refused: %v", err)
	}
}

// A flat scan cap still priced a decode by the frame: at the cap, a
// 6.5 KB progressive JPEG of 64 scans over a 64 MP frame held a core for
// ~2.8 s, seven times one scan. Property: the scan budget counts scans
// times pixels, so a frame at the pixel cap gets few scans while a small
// image keeps the flat cap's 64. Count-based, never timed: the refusal
// happens on the header and the scan count, before any pixel is decoded.
func TestDecodeBudgetsJPEGScansByPixels(t *testing.T) {
	const side = 8192 // 8192×8192 is DefaultMaxPixels
	if int64(side*side) != fwimage.DefaultMaxPixels {
		t.Fatalf("setup: %d² is not DefaultMaxPixels", side)
	}
	big := jpegtest.ScanBomb(side, side, fwimage.DefaultMaxJPEGScans)
	if _, err := fwimage.DecodeBytes(big); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("a %d-scan JPEG at the 64 MP pixel cap: %v, want ErrDecompressionBomb", fwimage.DefaultMaxJPEGScans, err)
	}
	// Raising the flat ceiling does not lift the budget.
	if _, err := fwimage.DecodeBytesWithConfig(big, fwimage.Config{MaxJPEGScans: 1000}); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("MaxJPEGScans raised: a 64 MP %d-scan JPEG: %v, want ErrDecompressionBomb", fwimage.DefaultMaxJPEGScans, err)
	}
	// One scan past the default budget at the pixel cap is refused.
	over := int(fwimage.DefaultMaxJPEGScanPixels/fwimage.DefaultMaxPixels) + 1
	if _, err := fwimage.DecodeBytes(jpegtest.ScanBomb(side, side, over)); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("a %d-scan JPEG at the pixel cap: %v, want ErrDecompressionBomb", over, err)
	}

	// The budget's edge, on a small frame with an explicit budget: ten
	// walks of 64×64 pass, eleven are refused.
	cfg := fwimage.Config{MaxJPEGScanPixels: 10 * 64 * 64}
	if _, err := fwimage.DecodeBytesWithConfig(jpegtest.ScanBomb(64, 64, 10), cfg); err != nil {
		t.Fatalf("10 scans of 64×64 inside a 10-walk budget were refused: %v", err)
	}
	if _, err := fwimage.DecodeBytesWithConfig(jpegtest.ScanBomb(64, 64, 11), cfg); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("11 scans of 64×64 past a 10-walk budget: %v, want ErrDecompressionBomb", err)
	}
	// The default budget scales with a host's MaxPixels: ten walks of it.
	small := fwimage.Config{MaxPixels: 64 * 64}
	if _, err := fwimage.DecodeBytesWithConfig(jpegtest.ScanBomb(64, 64, 11), small); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("11 scans of a frame at a host's MaxPixels: %v, want ErrDecompressionBomb", err)
	}
}

// The scan cap counts the scans the decoder runs, not FF DA byte pairs: a
// motion photo (Pixel, Samsung) is a one-scan JPEG with a video clip
// appended after EOI, and an APPn payload (an ICC profile, a MakerNote
// preview) is skipped by its length. Compressed bytes hold an FF DA pair
// about once per 64 KiB, so counting pairs over the whole file refused a
// phone photo with a 6 MiB clip as a decompression bomb.
func TestDecodeCountsOnlyScansTheDecoderRuns(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stdimage.NewRGBA(stdimage.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatal(err)
	}
	base := buf.Bytes()
	r := rand.New(rand.NewPCG(1, 2))
	noise := make([]byte, 6<<20)
	for i := range noise {
		noise[i] = byte(r.UintN(256))
	}
	if n := bytes.Count(noise, []byte{0xFF, 0xDA}); n <= fwimage.DefaultMaxJPEGScans {
		t.Fatalf("setup: noise holds only %d FF DA pairs", n)
	}
	// An APP2 segment, after SOI, full of FF DA pairs.
	app := []byte{0xFF, 0xE2, 0xFF, 0xFF}
	payload := bytes.Repeat([]byte{0xFF, 0xDA}, (0xFFFF-2)/2)
	app = append(app, payload...)
	withApp := append(append(append([]byte{}, base[:2]...), app...), base[2:]...)
	for name, photo := range map[string][]byte{
		"video after EOI": append(append([]byte{}, base...), noise...),
		"APP2 payload":    withApp,
		// Bytes after EOI that a walk past it would read as 100 empty
		// SOS segments.
		"marker-shaped trailer": append(append([]byte{}, base...), bytes.Repeat([]byte{0xFF, 0xDA, 0x00, 0x02}, 100)...),
	} {
		if _, err := jpeg.Decode(bytes.NewReader(photo)); err != nil {
			t.Fatalf("%s: setup: the stdlib refuses it: %v", name, err)
		}
		if _, err := fwimage.DecodeBytes(photo); err != nil {
			t.Errorf("%s: a one-scan JPEG was refused: %v", name, err)
		}
	}
}
