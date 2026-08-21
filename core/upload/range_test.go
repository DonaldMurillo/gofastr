package upload

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Storage.Get returns io.ReadCloser, which erases seekability, so ServeHandler
// could not answer a Range request: a client that dropped 1.8 GB into a 2 GB
// download restarted from zero, and a CDN probing with a range got a 200 with
// the whole body instead of a 206.
//
// The capability exists: LocalStorage.Get already hands back an *os.File,
// and the interface just hid it. RangeGetter is the declared way to ask for it.

func TestServeHandlerAnswersRangeRequest(t *testing.T) {
	h, body := localServeHandler(t)

	req := httptest.NewRequest("GET", "/big.bin", nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("Range request = %d, want 206 (whole body returned instead of a range)", rec.Code)
	}
	if got, want := rec.Body.String(), body[10:20]; got != want {
		t.Errorf("range body = %q, want %q", got, want)
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 10-19/"+itoa(len(body)) {
		t.Errorf("Content-Range = %q", cr)
	}
}

// A backend that can serve ranges must say so, or clients never ask.
func TestServeHandlerAdvertisesAcceptRanges(t *testing.T) {
	h, _ := localServeHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/big.bin", nil))
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Errorf("Accept-Ranges = %q, want %q", rec.Header().Get("Accept-Ranges"), "bytes")
	}
}

// The stored-XSS guard is the reason this handler exists rather than
// http.FileServer. Adding range support must not drop it.
func TestRangeServingKeepsScriptableGuard(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.svg"), []byte("<svg onload=alert(1)>"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := ServeHandler(NewLocalStorage(dir))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x.svg", nil))

	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "attachment" {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff header dropped")
	}
}

// A backend that declines the capability must still serve whole bodies:
// the fallback is the contract, not an error path.
func TestServeHandlerFallsBackWithoutRangeGetter(t *testing.T) {
	h := ServeHandler(nonSeekableStorage{data: "hello world"})
	req := httptest.NewRequest("GET", "/k", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("non-seekable backend = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello world" {
		t.Errorf("body = %q, want the whole object", rec.Body.String())
	}
}

func TestLocalStorageImplementsRangeGetter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	var s Storage = NewLocalStorage(dir)
	rg, ok := s.(RangeGetter)
	if !ok {
		t.Fatal("LocalStorage does not implement RangeGetter")
	}
	rs, err := rg.GetRange(context.Background(), "f.bin")
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	defer rs.Close()
	if _, err := rs.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got, _ := io.ReadAll(rs)
	if string(got) != "456789" {
		t.Errorf("after Seek(4) read %q, want %q", got, "456789")
	}
}

// GetRange must apply the same key sanitization as Get, a capability that
// skipped the traversal check would be a path-traversal hole wearing a
// performance hat.
func TestGetRangeRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	rg := NewLocalStorage(dir)
	if _, err := rg.GetRange(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("SECURITY: GetRange accepted a traversal key")
	}
}

func localServeHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	body := strings.Repeat("abcdefghij", 10)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return ServeHandler(NewLocalStorage(dir)), body
}

type nonSeekableStorage struct{ data string }

func (n nonSeekableStorage) Save(context.Context, string, io.Reader) error { return nil }
func (n nonSeekableStorage) Delete(context.Context, string) error          { return nil }
func (n nonSeekableStorage) Exists(context.Context, string) (bool, error)  { return true, nil }
func (n nonSeekableStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(n.data)), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
