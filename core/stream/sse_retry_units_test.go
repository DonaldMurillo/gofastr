package stream

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetRetryWritesMillisecondsFromSeconds(t *testing.T) {
	rec := httptest.NewRecorder()
	sse := NewSSEWriter(rec)
	sse.SetRetry(5)

	if got := rec.Body.String(); !strings.Contains(got, "retry: 5000\n") {
		t.Fatalf("SetRetry(5) wrote %q; retry field must be milliseconds for five seconds", got)
	}
	if got := rec.Body.String(); !strings.HasSuffix(got, "retry: 5000\n\n") {
		t.Fatalf("SetRetry(5) did not terminate the SSE block: %q", got)
	}
	if !rec.Flushed {
		t.Fatal("SetRetry did not flush the retry directive to the client")
	}
}

// A retry large enough to overflow the seconds-to-milliseconds multiply is
// capped at math.MaxInt64 instead of wrapping negative.
func TestSetRetryCapsAtMaxInt64(t *testing.T) {
	if int64(math.MaxInt) <= math.MaxInt64/1000 {
		t.Skip("int cannot reach the overflow guard on this platform")
	}
	rec := httptest.NewRecorder()
	sse := NewSSEWriter(rec)
	sse.SetRetry(math.MaxInt)

	const want = "retry: 9223372036854775807\n\n"
	if got := rec.Body.String(); got != want {
		t.Fatalf("SetRetry(math.MaxInt) wrote %q; want %q", got, want)
	}
}
