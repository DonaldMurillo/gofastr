package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCompressReadsEveryAcceptEncodingLine: Compress read
// Header.Get("Accept-Encoding"), the first field line only, so a client
// listing "br" then "gzip" on separate lines (RFC 9110 §5.3) was served
// identity.
func TestCompressReadsEveryAcceptEncodingLine(t *testing.T) {
	body := strings.Repeat("compress me ", 400)
	h := Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header["Accept-Encoding"] = []string{"br", "gzip"}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q, want gzip from the second Accept-Encoding line", got)
	}
}

// TestCompressHoldsImmutableOnSecondLine: an immutable directive on a
// second Cache-Control line is as immutable as one on the first; the
// writer read only the first line, so such a body was streamed rather
// than held whole for the compressed-copy cache.
func TestCompressHoldsImmutableOnSecondLine(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header()["Cache-Control"] = []string{"public", "max-age=31536000, immutable"}
	w := &compressWriter{rw: rec, accepts: true, status: http.StatusOK}
	if !w.holdsWhole() {
		t.Fatal("an immutable directive on the second Cache-Control line was not seen")
	}
}
