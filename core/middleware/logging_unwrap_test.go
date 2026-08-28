package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineWriter fakes the net/http response writer's deadline surface
// so ResponseController has something real to reach at the bottom of
// the wrapper chain.
type deadlineWriter struct {
	http.ResponseWriter
	writeDeadlineSet bool
}

func (d *deadlineWriter) SetWriteDeadline(t time.Time) error {
	d.writeDeadlineSet = true
	return nil
}

// Pins #260: http.NewResponseController must resolve SetWriteDeadline
// through the logging wrapper via Unwrap. Before the fix it returned a
// silent ErrNotSupported, so streaming handlers behind logging had no
// per-write deadline and a half-open client pinned the goroutine.
func TestLoggingWriterUnwrapsForResponseController(t *testing.T) {
	base := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	var innerErr error
	h := Logging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		innerErr = rc.SetWriteDeadline(time.Now().Add(time.Second))
	}))
	h.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/", nil))

	if innerErr != nil {
		t.Fatalf("SetWriteDeadline through logging wrapper: %v", innerErr)
	}
	if !base.writeDeadlineSet {
		t.Fatal("deadline never reached the underlying writer")
	}
}
