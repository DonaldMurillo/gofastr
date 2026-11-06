package file_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegtest"
)

func TestStripStuffingCannotBypassBudget(t *testing.T) {
	clean := jpegtest.Progressive(8192, 8192, 3, 0)
	frame := bytes.Index(clean, []byte{0xFF, 0xC2})
	if frame <= 2 {
		t.Fatal("fixture has no frame after metadata")
	}
	stuffed := append(append(append([]byte{}, clean[:frame]...), 0xFF, 0), clean[frame:]...)
	data := jpegWithSegments(t, stuffed, []byte{0xFF, 0xE0, 0, 7, 'J', 'F', 'I', 'F', 0}, app1ExifSegment(tiffWithGPSAndOrientation(6)))
	_, err := file.ProcessFileField(context.Background(), &captureStorage{}, bytes.NewReader(data), "stuffed.jpg", "posts", "photo", file.StripMetadata())
	if err == nil || !strings.Contains(err.Error(), "coefficients") {
		t.Fatalf("stuffed header reached decoder instead of memory cap: %v", err)
	}
}
