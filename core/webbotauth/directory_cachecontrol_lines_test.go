package webbotauth

import (
	"net/http"
	"testing"
	"time"
)

// TestResponseTTLReadsEveryCacheControlLine: the directory cache read
// Header.Get("Cache-Control"), the first field line only, so a max-age
// on a second line (RFC 9110 §5.3) was missed and the entry fell back to
// Expires or the default.
func TestResponseTTLReadsEveryCacheControlLine(t *testing.T) {
	h := http.Header{"Cache-Control": {"public", "max-age=600"}}
	d, ok := responseTTL(h, time.Now())
	if !ok {
		t.Fatal("max-age on the second Cache-Control line was not read")
	}
	if d != clampTTL(600*time.Second) {
		t.Fatalf("ttl %v, want the clamped 600s", d)
	}
}
