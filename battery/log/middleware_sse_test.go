package log

import (
	"bufio"
	"log/slog"
	"net"
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

// accessMiddleware must forward http.Hijacker so WebSocket / connection-
// upgrade handlers under it can take over the conn. Without the forwarder a
// downstream handler's `w.(http.Hijacker)` assertion fails — every WS upgrade
// behind battery/log breaks with "does not support hijacking".
func TestAccessMiddlewarePreservesHijacker(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(testWriter(t), nil))
	mw := accessMiddleware(logger, false)

	var seenHijacker bool
	var hijackErr error
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		seenHijacker = ok
		if ok {
			// The wrapper must forward to the real Hijacker, not return
			// "unsupported" against a writer that does support it.
			_, _, hijackErr = hj.Hijack()
		}
	}))

	handler.ServeHTTP(&hijackableWriter{}, httptest.NewRequest("GET", "/ws", nil))
	if !seenHijacker {
		t.Fatal("handler's w.(http.Hijacker) assertion failed — access middleware swallows Hijack")
	}
	if hijackErr != nil {
		t.Fatalf("Hijack not forwarded to underlying writer: %v", hijackErr)
	}
}

// hijackableWriter is a ResponseWriter that supports hijacking, modelling the
// real net/http server writer that core/stream/websocket.go upgrades against.
type hijackableWriter struct {
	httptest.ResponseRecorder
}

func (w *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

var _ http.Hijacker = (*hijackableWriter)(nil)

// testWriter discards log output during the test.
func testWriter(t *testing.T) *discardWriter { t.Helper(); return &discardWriter{} }

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }
