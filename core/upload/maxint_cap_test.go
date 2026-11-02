package upload

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Config.MaxSize = math.MaxInt64 reads as "no practical limit", but the
// handler capped the body with http.MaxBytesReader(w, r.Body,
// maxSize+multipartFramingSlack), and the sum wrapped negative: the
// reader refused the first byte, so every upload answered 413. The cap
// now saturates at MaxInt64.
func TestUploadMaxIntMaxSizeAcceptsAnUpload(t *testing.T) {
	h := Handler(Config{Storage: nopStorage{}, MaxSize: math.MaxInt64})
	body, ct := streamedUpload(t, 16)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("status = 413 for a 16-byte upload with MaxSize = MaxInt64; the body cap wrapped negative")
	}
}
