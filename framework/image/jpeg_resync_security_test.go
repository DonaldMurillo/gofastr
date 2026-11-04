package image_test

import (
	"bytes"
	"errors"
	"image/jpeg"
	"testing"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegtest"
)

func TestJPEGJunkCannotBypassBudget(t *testing.T) {
	clean := jpegtest.Progressive(64, 64, 3, 3)
	frame := bytes.Index(clean, []byte{0xFF, 0xC2})
	if frame <= 2 {
		t.Fatal("fixture has no frame after metadata")
	}
	for _, junk := range [][]byte{{0x42}, {0xFF, 0}, {0x42, 0xFF, 0, 0x55}, {0xFF, 0xFF}} {
		data := append(append(append([]byte{}, clean[:frame]...), junk...), clean[frame:]...)
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("fixture with junk %x does not decode: %v", junk, err)
		}
		if _, err := fwimage.DecodeBytesWithConfig(data, fwimage.Config{MaxProgressiveJPEGBytes: 49152}); err != nil {
			t.Errorf("junk %x at budget: %v", junk, err)
		}
		if _, err := fwimage.DecodeBytesWithConfig(data, fwimage.Config{MaxProgressiveJPEGBytes: 49151}); !errors.Is(err, fwimage.ErrDecompressionBomb) {
			t.Errorf("junk %x bypassed coefficient budget: %v", junk, err)
		}
	}
}
