package upload

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Helpers ---

func newMultipartBody(t *testing.T, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("creating form file: %v", err)
	}
	part.Write([]byte(content))
	w.Close()
	return &buf, w.FormDataContentType()
}

func tmpDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// --- Handler tests ---

func TestUploadValidFileSucceeds(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{
		MaxSize:      1 << 20, // 1 MB
		AllowedTypes: []string{"text/plain; charset=utf-8", "application/octet-stream"},
		AllowedExts:  []string{"txt"},
		Storage:      storage,
	}

	handler := Handler(cfg)

	body, contentType := newMultipartBody(t, "hello.txt", "hello world")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify file was saved
	exists, err := storage.Exists(context.Background(), "hello.txt")
	if err != nil {
		t.Fatalf("checking existence: %v", err)
	}
	if !exists {
		t.Fatal("file should exist in storage after upload")
	}
}

func TestRejectFileTooLarge(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{
		MaxSize: 10, // 10 bytes max
		Storage: storage,
	}

	handler := Handler(cfg)

	body, contentType := newMultipartBody(t, "big.txt", "this content is definitely more than ten bytes")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRejectDisallowedMIME(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{
		MaxSize:      1 << 20,
		AllowedTypes: []string{"image/png"},
		Storage:      storage,
	}

	handler := Handler(cfg)

	// Write a plain text file — not image/png
	body, contentType := newMultipartBody(t, "test.txt", "plain text content")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRejectDisallowedExtension(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{
		MaxSize:      1 << 20,
		AllowedExts:  []string{"png", "jpg"},
		Storage:      storage,
	}

	handler := Handler(cfg)

	body, contentType := newMultipartBody(t, "evil.exe", "binary content here")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not allowed") {
		t.Fatalf("expected extension rejection message, got: %s", rr.Body.String())
	}
}

func TestPathTraversalSanitized(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{
		MaxSize: 1 << 20,
		Storage: storage,
	}

	handler := Handler(cfg)

	// Attempt path traversal via filename
	body, contentType := newMultipartBody(t, "../../../etc/passwd", "malicious")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Ensure the file was NOT saved outside the storage dir
	savedAs := SanitizeFilename("../../../etc/passwd")
	if savedAs == "passwd" || savedAs == "etc_passwd" || strings.Contains(savedAs, "..") || strings.Contains(savedAs, "/") {
		// OK — just make sure it's safe
		if strings.Contains(savedAs, "..") || strings.Contains(savedAs, "/") {
			t.Fatalf("sanitized name is unsafe: %q", savedAs)
		}
	}

	// Verify no file was created outside base dir
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "passwd") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected file to be saved with sanitized name inside storage dir")
	}

	// Verify traversal path doesn't exist above
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "etc", "passwd")); err == nil {
		t.Fatal("file should not have been created outside storage dir")
	}
}

// --- Local storage tests ---

func TestLocalStorageSaveAndGetRoundTrip(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)
	ctx := context.Background()

	content := "round trip test content"
	err := storage.Save(ctx, "testfile.txt", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	rc, err := storage.Get(ctx, "testfile.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatalf("reading: %v", err)
	}

	if buf.String() != content {
		t.Fatalf("expected %q, got %q", content, buf.String())
	}
}

func TestLocalStorageDelete(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)
	ctx := context.Background()

	err := storage.Save(ctx, "deleteme.txt", strings.NewReader("delete me"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	exists, err := storage.Exists(ctx, "deleteme.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("file should exist before delete")
	}

	err = storage.Delete(ctx, "deleteme.txt")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err = storage.Exists(ctx, "deleteme.txt")
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("file should not exist after delete")
	}
}

func TestLocalStorageRejectsPathTraversal(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)
	ctx := context.Background()

	err := storage.Save(ctx, "../../etc/passwd", strings.NewReader("evil"))
	if err == nil {
		t.Fatal("expected error for path traversal key")
	}
}

// --- Validation unit tests ---

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"normal.txt", "normal.txt"},
		{"../../../etc/passwd", "passwd"},
		{"..\\..\\windows\\system32", "system32"},
		{".hidden", "hidden"},
		{"file\x00name.txt", "filename.txt"},
		{"/absolute/path/file.txt", "file.txt"},
		{"", "upload"},
		{"...", "upload"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
			// Ensure no path traversal in result
			if strings.Contains(got, "..") {
				t.Errorf("result contains ..: %q", got)
			}
			if strings.Contains(got, "/") || strings.Contains(got, "\\") {
				t.Errorf("result contains path separator: %q", got)
			}
		})
	}
}

func TestValidateSizeAllowsUnderMax(t *testing.T) {
	if err := ValidateSize(50, 100); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSizeRejectsOverMax(t *testing.T) {
	if err := ValidateSize(200, 100); err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestValidateExtAllowsListed(t *testing.T) {
	if err := ValidateExt("photo.jpg", []string{"jpg", "png"}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateExtRejectsUnlisted(t *testing.T) {
	if err := ValidateExt("virus.exe", []string{"jpg", "png"}); err == nil {
		t.Fatal("expected error for unlisted extension")
	}
}

func TestValidateMIMEReadsAndResets(t *testing.T) {
	content := []byte("plain text content for mime test")
	reader := bytes.NewReader(content)

	err := ValidateMIME(reader, []string{"text/plain; charset=utf-8", "application/octet-stream"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify reader was reset
	if reader.Len() != len(content) {
		t.Fatalf("reader not reset: Len()=%d, want %d", reader.Len(), len(content))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{Storage: storage}
	handler := Handler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestMissingFileField(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)

	cfg := Config{Storage: storage}
	handler := Handler(cfg)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLocalStorageSubdirectoryCreation(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)
	ctx := context.Background()

	err := storage.Save(ctx, "sub/dir/file.txt", strings.NewReader("nested"))
	if err != nil {
		t.Fatalf("Save with subdirs: %v", err)
	}

	rc, err := storage.Get(ctx, "sub/dir/file.txt")
	if err != nil {
		t.Fatalf("Get with subdirs: %v", err)
	}
	defer rc.Close()

	exists, err := storage.Exists(ctx, "sub/dir/file.txt")
	if err != nil {
		t.Fatalf("Exists with subdirs: %v", err)
	}
	if !exists {
		t.Fatal("expected file to exist")
	}
}

func TestLocalStorageSaveAndExists(t *testing.T) {
	dir := tmpDir(t)
	storage := NewLocalStorage(dir)
	ctx := context.Background()

	exists, err := storage.Exists(ctx, "nonexistent.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("nonexistent file should not exist")
	}

	_ = storage.Save(ctx, "exists.txt", strings.NewReader("data"))

	exists, err = storage.Exists(ctx, "exists.txt")
	if err != nil {
		t.Fatalf("Exists after save: %v", err)
	}
	if !exists {
		t.Fatal("saved file should exist")
	}
}

func TestMain(m *testing.M) {
	fmt.Println("running upload tests")
	os.Exit(m.Run())
}
