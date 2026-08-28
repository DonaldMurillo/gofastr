package log

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deadlineRecorder struct {
	http.ResponseWriter
	writeDeadlineSet bool
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.writeDeadlineSet = true
	return nil
}

// Pins #260's battery half: the access-log wrapper must let
// http.NewResponseController reach the underlying writer's deadlines.
// battery/log is the production access-log path, so this is the one a
// deployed streaming handler actually sits behind.
func TestAccessLogWriterUnwrapsForResponseController(t *testing.T) {
	base := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var innerErr error
	h := accessMiddleware(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerErr = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Second))
	}))
	h.ServeHTTP(base, httptest.NewRequest(http.MethodGet, "/", nil))

	if innerErr != nil {
		t.Fatalf("SetWriteDeadline through access-log wrapper: %v", innerErr)
	}
	if !base.writeDeadlineSet {
		t.Fatal("deadline never reached the underlying writer")
	}
}
