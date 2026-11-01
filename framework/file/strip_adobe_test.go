package file_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// adobeSegment is an APP14 "Adobe" segment: version 100, no flags, and
// the colour transform byte (0 = none, so 4 components read as CMYK).
// extra is appended past the 12 bytes the decoder reads.
func adobeSegment(transform byte, extra []byte) []byte {
	payload := append([]byte{'A', 'd', 'o', 'b', 'e', 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, transform}, extra...)
	segLen := len(payload) + 2
	return append([]byte{0xFF, 0xEE, byte(segLen >> 8), byte(segLen)}, payload...)
}

// cmykJPEG hand-builds an 8x8 baseline JPEG with four components, the
// shape a print workflow or Photoshop CMYK export produces. Each block
// is a zero DC difference and an immediate end-of-block, so the scan is
// one zero byte. Go's encoder cannot write CMYK, hence the hand build.
func cmykJPEG(segs ...[]byte) []byte {
	b := []byte{0xFF, 0xD8}
	for _, s := range segs {
		b = append(b, s...)
	}
	// DQT: table 0, every coefficient 1.
	b = append(b, 0xFF, 0xDB, 0x00, 0x43, 0x00)
	b = append(b, bytes.Repeat([]byte{0x01}, 64)...)
	// SOF0: 8-bit, 8x8, four components sampled 1x1 on table 0.
	b = append(b, 0xFF, 0xC0, 0x00, 0x14, 0x08, 0x00, 0x08, 0x00, 0x08, 0x04)
	for id := byte(1); id <= 4; id++ {
		b = append(b, id, 0x11, 0x00)
	}
	// DHT: DC table 0 and AC table 0, each one 1-bit code for symbol 0.
	b = append(b, 0xFF, 0xC4, 0x00, 0x26)
	for _, class := range []byte{0x00, 0x10} {
		b = append(b, class, 0x01)
		b = append(b, make([]byte, 15)...)
		b = append(b, 0x00)
	}
	// SOS over all four components, then one byte of scan data.
	b = append(b, 0xFF, 0xDA, 0x00, 0x0E, 0x04)
	for id := byte(1); id <= 4; id++ {
		b = append(b, id, 0x00)
	}
	b = append(b, 0x00, 0x3F, 0x00, 0x00)
	return append(b, 0xFF, 0xD9)
}

// TestStripKeepsAdobeColourMarker: the decoder needs the APP14 Adobe
// segment to read a four-component JPEG (and to tell RGB from YCbCr in a
// three-component one). Stripping every APPn turned a valid CMYK upload
// into one framework/image refuses. The segment carries no personal
// data, so it survives, cut to the 12 bytes the decoder reads.
func TestStripKeepsAdobeColourMarker(t *testing.T) {
	t.Parallel()
	body := cmykJPEG(
		app1ExifSegment(tiffWithGPSAndOrientation(1)),
		adobeSegment(0, []byte("hidden-trailer")),
		comSegment("camera serial 4211"),
	)
	if img, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("setup broken: fixture does not decode: %v", err)
	} else if _, ok := img.(*image.CMYK); !ok {
		t.Fatalf("setup broken: fixture decodes as %T, want *image.CMYK", img)
	}

	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(body), "print.jpg", "posts", "photo", file.StripMetadata()); err != nil {
		t.Fatalf("ProcessFileField with StripMetadata: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(store.data))
	if err != nil {
		t.Fatalf("stripped CMYK JPEG no longer decodes: %v", err)
	}
	if _, ok := img.(*image.CMYK); !ok {
		t.Errorf("stripped JPEG decodes as %T, want *image.CMYK", img)
	}
	for _, leak := range []string{"Exif\x00\x00", "GPS\x00", "camera serial", "hidden-trailer"} {
		if bytes.Contains(store.data, []byte(leak)) {
			t.Errorf("SECURITY: [adobe-strip] stripped JPEG keeps %q", leak)
		}
	}
}

// TestStripBakesCMYKOrientation: the orientation bake decodes the
// spliced stream, so it needs the Adobe marker too.
func TestStripBakesCMYKOrientation(t *testing.T) {
	t.Parallel()
	body := cmykJPEG(app1ExifSegment(tiffWithGPSAndOrientation(6)), adobeSegment(0, nil))
	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(body), "print.jpg", "posts", "photo", file.StripMetadata()); err != nil {
		t.Fatalf("ProcessFileField with StripMetadata: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(store.data)); err != nil {
		t.Fatalf("baked CMYK JPEG does not decode: %v", err)
	}
}
