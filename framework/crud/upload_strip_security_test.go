package crud

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins the auto-CRUD wiring for the 2026-09-04 metadata decision:
// originals are stored verbatim by default, and CrudHandler.
// StripUploadMetadata (framework.WithStripUploadMetadata) strips the
// EXIF segment from a multipart upload before storage.
// Property: a stored original carries its metadata unless the host opted
// into stripping on the handler.
// Surfaces: framework/crud/crud_upload.go saveFilePart.
func TestMultipartCreateStripsMetadataWhenOptedIn(t *testing.T) {
	for _, strip := range []bool{false, true} {
		ch, _ := covUploadHandler(t)
		dir := t.TempDir()
		ch.Storage = upload.NewLocalStorage(dir)
		ch.StripUploadMetadata = strip

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("caption", "exif")
		fw, _ := mw.CreateFormFile("photo", "pic.jpg")
		_, _ = fw.Write(jpegWithExifMarker(t))
		mw.Close()

		req := withTestUser(httptest.NewRequest("POST", "/media", &buf), "u1")
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("strip=%v: multipart create = %d, body=%s", strip, rec.Code, rec.Body.String())
		}
		stored := onlyStoredFile(t, dir)
		hasExif := bytes.Contains(stored, []byte("Exif\x00\x00"))
		if strip && hasExif {
			t.Fatalf("SECURITY: [upload-strip] StripUploadMetadata set but the stored original keeps its EXIF segment")
		}
		if !strip && !hasExif {
			t.Fatalf("default must store the original verbatim; the EXIF segment is gone")
		}
	}
}

// jpegWithExifMarker is a decodable JPEG with an APP1 Exif segment
// inserted after SOI.
func jpegWithExifMarker(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 30, A: 255})
		}
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	raw := enc.Bytes()
	payload := append([]byte("Exif\x00\x00"), []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x00")...)
	seg := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg...)
	return append(out, raw[2:]...)
}

func onlyStoredFile(t *testing.T, dir string) []byte {
	t.Helper()
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no stored file under %s", dir)
	}
	b, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
