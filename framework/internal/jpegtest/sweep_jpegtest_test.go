package jpegtest

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/internal/jpegcost"
)

func TestFixturesDecodeAndCostWhatTheyClaim(t *testing.T) {
	for _, tc := range []struct {
		name        string
		data        []byte
		comps, w, h int
	}{
		{"scan bomb", ScanBomb(40, 24, 3), 1, 40, 24},
		{"progressive color", Progressive(64, 64, 3, 3), 3, 64, 64},
		{"many blocks", ScanBomb(2048, 2048, 1), 1, 2048, 2048},
	} {
		img, err := jpeg.Decode(bytes.NewReader(tc.data))
		if err != nil {
			t.Fatalf("%s: image/jpeg refused the fixture: %v", tc.name, err)
		}
		if b := img.Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
			t.Fatalf("%s: decoded %v, want %dx%d", tc.name, b, tc.w, tc.h)
		}
		blocks := int64((tc.w+7)/8) * int64((tc.h+7)/8)
		if got, ok := jpegcost.ProgressiveCoeffBytes(tc.data); !ok || got != int64(tc.comps)*blocks*256 {
			t.Fatalf("%s: cost = %d, %v; want %d", tc.name, got, ok, int64(tc.comps)*blocks*256)
		}
	}
}
