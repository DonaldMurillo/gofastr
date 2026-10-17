package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// BenchmarkTimeoutPage measures what Timeout adds to a page-sized
// response: about 96 KiB written in 4 KiB pieces, the shape an SSR page
// with inlined CSS reaches the writer in. Compare it against
// BenchmarkTimeoutPageBare, the same handler with no middleware.
func BenchmarkTimeoutPage(b *testing.B) {
	benchPage(b, Timeout(time.Minute)(pageHandler()))
}

func BenchmarkTimeoutPageBare(b *testing.B) {
	benchPage(b, pageHandler())
}

func pageHandler() http.Handler {
	chunk := bytes.Repeat([]byte("x"), 4<<10)
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		for range 24 {
			_, _ = w.Write(chunk)
		}
	})
}

// discardWriter is a ResponseWriter that keeps nothing, so the benchmark
// counts the middleware's allocations and not a recorder's.
type discardWriter struct{ h http.Header }

func (d *discardWriter) Header() http.Header         { return d.h }
func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardWriter) WriteHeader(int)             {}

func benchPage(b *testing.B, h http.Handler) {
	r := httptest.NewRequest("GET", "/", nil)
	b.ReportAllocs()
	b.SetBytes(96 << 10)
	for b.Loop() {
		h.ServeHTTP(&discardWriter{h: http.Header{}}, r)
	}
}
