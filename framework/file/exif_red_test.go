//go:build red

package file_test

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// CONTRACT-QUESTION red: the upload pipeline parses exactly one EXIF tag
// (orientation) and re-encodes derived variants (metadata dropped), but the
// stored ORIGINAL keeps the full EXIF block — GPS coordinates, camera
// serials, embedded thumbnails. No doc claims stripping, so this asserts a
// policy the framework has not chosen yet. Delete or promote per maintainer
// decision: strip metadata from stored originals (keep it configurable for
// hosts that need it), or document preservation as deliberate.
// Property: stored image originals do not carry attacker/user-supplied
// privacy metadata unless the host opted into keeping it.
// Surfaces: framework/file/filefield.go::ProcessFileField (stores the
// original verbatim via store.Save), core/upload/serve.go (serves it back).
// Finding: a JPEG with an EXIF APP1 segment uploaded through an Image
// file-field is persisted byte-for-byte; the GPS block survives to every
// later download.
// Fix direction: strip EXIF (and equivalent metadata segments) from the
// stored original on the image path, or gate preservation behind an explicit
// ProcessOption that defaults off.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// redJPEGWithExif encodes a 1x1 JPEG and splices an APP1 (Exif) segment
// right after the SOI marker, mirroring framework/image's
// insertExifAPP1 test helper (not importable across test packages).
func redJPEGWithExif(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 9, G: 8, B: 7, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	// Minimal TIFF/EXIF payload; the property is segment presence, not IFD
	// validity: "Exif\x00\x00" + little-endian TIFF header + one IFD entry
	// tagged as GPS (0x8825) carrying plainly recognizable bytes.
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // TIFF header, IFD0 at offset 8
		0x01, 0x00, // 1 entry
		0x25, 0x88, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 'G', 'P', 'S', 0x00, // GPSInfo tag
		0x00, 0x00, 0x00, 0x00, // next IFD = 0
	}
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(exif) + 2
	app1 := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}, exif...)

	out := make([]byte, 0, buf.Len()+len(app1))
	out = append(out, buf.Bytes()[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, buf.Bytes()[2:]...)
	return out
}

func TestFileFieldRedStripsStoredEXIF(t *testing.T) {
	store := &captureStorage{}
	withExif := redJPEGWithExif(t)
	if !bytes.Contains(withExif, []byte("Exif\x00\x00")) {
		t.Fatal("fixture does not carry an EXIF segment — setup broken")
	}

	ff, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(withExif), "photo.jpg", "posts", "avatar")
	if err != nil {
		t.Fatalf("ProcessFileField rejected the upload outright (%v) — if that is now the policy, this test is green", err)
	}
	if ff == nil || ff.URL == "" {
		t.Fatal("no stored file — setup no longer reaches the sink")
	}
	if bytes.Contains(store.data, []byte("Exif\x00\x00")) || bytes.Contains(store.data, []byte("GPS\x00")) {
		t.Errorf("SECURITY: [exif-passthrough] the stored image original keeps its EXIF block (%d bytes incl. GPS-tagged segment); "+
			"every later download of %s serves the uploader's privacy metadata", len(store.data), ff.URL)
	}
}
