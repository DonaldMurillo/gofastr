package file_test

import (
	"bytes"
	"context"
	"image/jpeg"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegtest"
)

// StripMetadata's orientation bake decodes the JPEG behind a pixel cap
// only. A progressive JPEG may carry any number of scans, each about ten
// bytes and each a full walk of the frame, so an upload with an
// orientation-6 EXIF segment bought decode CPU in proportion to its own
// size times the frame. Property: the bake refuses a JPEG over the
// pipeline's scan cap (framework/image.DefaultMaxJPEGScans) before it
// decodes, and accepts one at the cap, which pins the two caps together.
func TestStripMetadataRefusesJPEGScanBomb(t *testing.T) {
	orient := app1ExifSegment(tiffWithGPSAndOrientation(6))
	bomb := jpegWithSegments(t, jpegtest.ScanBomb(64, 64, fwimage.DefaultMaxJPEGScans+1), orient)
	// Setup proof: the fixture decodes, so a refusal is the scan cap.
	if _, err := jpeg.Decode(bytes.NewReader(bomb)); err != nil {
		t.Fatalf("setup broken: fixture does not decode: %v", err)
	}
	if _, err := file.ProcessFileField(context.Background(), &captureStorage{},
		bytes.NewReader(bomb), "bomb.jpg", "posts", "photo", file.StripMetadata()); err == nil {
		t.Errorf("StripMetadata decoded a JPEG of %d scans for its orientation bake; want a refusal past %d",
			fwimage.DefaultMaxJPEGScans+1, fwimage.DefaultMaxJPEGScans)
	}

	atCap := jpegWithSegments(t, jpegtest.ScanBomb(64, 64, fwimage.DefaultMaxJPEGScans), orient)
	if _, err := file.ProcessFileField(context.Background(), &captureStorage{},
		bytes.NewReader(atCap), "ok.jpg", "posts", "photo", file.StripMetadata()); err != nil {
		t.Errorf("a JPEG at the pipeline's scan cap was refused by the strip path: %v", err)
	}
}

// The flat scan cap priced the bake by the frame: 64 scans over a 64 MP
// frame cost ~2.8 s of decode for a few KB. Property: the bake applies
// the pipeline's scans × pixels budget (framework/image.
// DefaultMaxJPEGScanPixels) before it decodes. Count-based: both
// refusals land on the header, before any pixel is decoded.
func TestStripMetadataBudgetsJPEGScansByPixels(t *testing.T) {
	orient := app1ExifSegment(tiffWithGPSAndOrientation(6))
	for _, tc := range []struct {
		side, scans int
	}{
		{8192, fwimage.DefaultMaxJPEGScans}, // the pixel cap, the flat cap's scans
		// A 16 MP frame one scan past the budget pins the strip budget
		// no looser than the pipeline's.
		{4096, int(fwimage.DefaultMaxJPEGScanPixels/(4096*4096)) + 1},
	} {
		bomb := jpegWithSegments(t, jpegtest.ScanBomb(tc.side, tc.side, tc.scans), orient)
		if _, err := file.ProcessFileField(context.Background(), &captureStorage{},
			bytes.NewReader(bomb), "bomb.jpg", "posts", "photo", file.StripMetadata()); err == nil {
			t.Errorf("StripMetadata decoded %d scans of a %d² JPEG for its orientation bake; want a refusal past %d pixel-scans",
				tc.scans, tc.side, fwimage.DefaultMaxJPEGScanPixels)
		}
	}
}
