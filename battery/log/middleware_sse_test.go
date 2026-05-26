package log

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// accessMiddleware must forward http.Flusher so SSE / chunked-JSON
// handlers under it can stream. Without the forwarder a downstream
// handler's `w.(http.Flusher)` assertion fails — every SSE endpoint
// behind battery/log returns 500 "streaming unsupported".
func TestAccessMiddlewarePreservesFlusher(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(testWriter(t), nil))
	mw := accessMiddleware(logger, false)

	var seenFlusher bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, seenFlusher = w.(http.Flusher)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sse", nil))
	if !seenFlusher {
		t.Fatal("handler's w.(http.Flusher) assertion failed — access middleware swallows Flush")
	}
}

// testWriter discards log output during the test.
func testWriter(t *testing.T) *discardWriter { t.Helper(); return &discardWriter{} }

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }
