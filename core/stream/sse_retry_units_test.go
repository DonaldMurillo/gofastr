package stream

import (
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
