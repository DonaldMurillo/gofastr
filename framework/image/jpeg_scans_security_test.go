package image_test

import (
	"bytes"
	"errors"
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
