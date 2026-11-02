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
