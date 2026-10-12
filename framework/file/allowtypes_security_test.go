package file_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// AllowTypes refuses an upload whose sniffed type is not listed, before
// anything is saved, whatever its filename claims.
func TestProcessFileField_AllowTypes(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	pdf := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<<>>\nendobj\n")
	allow := file.AllowTypes(file.ImageTypes...)
	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(png), "logo.png", "customers", "logo", allow); err != nil {
		t.Fatalf("a PNG was refused: %v", err)
	}
	store = &captureStorage{}
	_, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(pdf), "logo.png", "customers", "logo", allow)
	if !errors.Is(err, file.ErrFileFieldType) {
		t.Fatalf("a PDF named .png = %v, want ErrFileFieldType", err)
	}
	if store.key != "" {
		t.Fatalf("a refused upload was saved under %q", store.key)
	}
	// A blob the sniffer cannot name is not an image because its
	// filename says so.
	_, err = file.ProcessFileField(context.Background(), &captureStorage{}, bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7}), "logo.png", "customers", "logo", allow)
	if !errors.Is(err, file.ErrFileFieldType) {
		t.Fatalf("an unknown blob named .png = %v, want ErrFileFieldType", err)
	}
}
