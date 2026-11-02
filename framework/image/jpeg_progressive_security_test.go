package image_test

import (
	"bytes"
	"errors"
	stdimage "image"
	"image/jpeg"
	"testing"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegtest"
)

// The pixel cap bounds the raster a decode returns, not what image/jpeg
// holds on the way: for a progressive JPEG it keeps a 256-byte block of
// coefficients for every block of every component, for the whole frame.
// A few-hundred-byte progressive 3-component 8192×8192 file sat inside
// the 64 MP cap and allocated ~1 GB (768 MB of it coefficients), against
// ~192 MB for the same frame baseline. Property: a progressive JPEG whose
// coefficient buffer exceeds MaxProgressiveJPEGBytes is refused on its
// frame header, before any pixel is decoded; baseline JPEGs are untouched.
func TestDecodeRefusesProgressiveJPEGOverMemoryBudget(t *testing.T) {
	// Setup proof: the fixture shape decodes, so a refusal is the budget.
	if _, err := jpeg.Decode(bytes.NewReader(jpegtest.Progressive(64, 64, 3, 3))); err != nil {
		t.Fatalf("setup broken: the progressive fixture does not decode: %v", err)
	}
	bomb := jpegtest.Progressive(8192, 8192, 3, 3)
	if len(bomb) > 4096 {
		t.Fatalf("setup: the bomb is %d bytes, want a few hundred", len(bomb))
	}
	if _, err := fwimage.DecodeBytes(bomb); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("a progressive 3-component 8192² JPEG (768 MiB of coefficients): %v, want ErrDecompressionBomb", err)
	}

	// The budget's edge: 64×64×3 is 192 blocks, 49,152 bytes.
	small := jpegtest.Progressive(64, 64, 3, 3)
	if _, err := fwimage.DecodeBytesWithConfig(small, fwimage.Config{MaxProgressiveJPEGBytes: 192 * 256}); err != nil {
		t.Fatalf("a progressive JPEG exactly at its coefficient budget was refused: %v", err)
	}
	if _, err := fwimage.DecodeBytesWithConfig(small, fwimage.Config{MaxProgressiveJPEGBytes: 192*256 - 1}); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("a progressive JPEG one byte past its coefficient budget: %v, want ErrDecompressionBomb", err)
	}

	// A baseline JPEG keeps no coefficients: no budget refuses it.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stdimage.NewRGBA(stdimage.Rect(0, 0, 64, 64)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fwimage.DecodeBytesWithConfig(buf.Bytes(), fwimage.Config{MaxProgressiveJPEGBytes: 1}); err != nil {
		t.Fatalf("a baseline JPEG was refused by the progressive budget: %v", err)
	}
}
