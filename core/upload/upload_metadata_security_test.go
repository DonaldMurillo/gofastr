package upload

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

func multipartBodyWithPartContentType(t *testing.T, filename, partContentType string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	hdr.Set("Content-Type", partContentType)
	part, err := w.CreatePart(hdr)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestUploadHandler_MetadataMimeTypeUsesDetectedContent_NotPartHeaderDangerous(t *testing.T) {
	dir := tmpDir(t)
	handler := Handler(Config{Storage: NewLocalStorage(dir)})

	body, contentType := multipartBodyWithPartContentType(t, "note.txt", "image/png", []byte("plain text body"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var meta Metadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.MimeType != "text/plain; charset=utf-8" {
		t.Fatalf("SECURITY: [upload] metadata mime_type trusted multipart header %q, want detected text/plain; charset=utf-8. Attack: MIME spoofing through stored upload metadata.", meta.MimeType)
	}
}

func TestUploadHandler_MetadataMimeTypeUsesDetectedContent_NotPartHeaderBenign(t *testing.T) {
	dir := tmpDir(t)
	handler := Handler(Config{Storage: NewLocalStorage(dir)})

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	body, contentType := multipartBodyWithPartContentType(t, "image.png", "text/plain", png)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var meta Metadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.MimeType != "image/png" {
		t.Fatalf("SECURITY: [upload] metadata mime_type trusted multipart header %q, want detected image/png. Attack: safe content mislabeled through attacker-controlled upload metadata.", meta.MimeType)
	}
}
