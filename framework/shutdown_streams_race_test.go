package framework

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// hijackHook is a ResponseWriter whose Hijack runs before first, the
// way a drain can land between a stream joining the open set and its
// connection being handed back.
type hijackHook struct {
	http.ResponseWriter
	before func()
	conn   net.Conn
}

func (h *hijackHook) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.before()
	return h.conn, bufio.NewReadWriter(bufio.NewReader(h.conn), bufio.NewWriter(h.conn)), nil
}

// A drain that ran after Hijack joined the set, but before the
// connection was recorded, found nothing to close and finished; the
// hijack then handed the handler a live connection outside shutdown.
// A connection that arrives after its stream was ended is closed and
// the hijack refused.
func TestHijackAfterEndIsClosed(t *testing.T) {
	a := &App{}
	server, client := net.Pipe()
	defer client.Close()
	w := &hijackHook{ResponseWriter: httptest.NewRecorder(), conn: server,
		before: func() { a.streams.endAll(context.Background()) }}
	var hijackErr error
	var got net.Conn
	h := a.endStreamsOnShutdown(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got, _, hijackErr = http.NewResponseController(w).Hijack()
	}))
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !errors.Is(hijackErr, errDraining) || got != nil {
		t.Fatalf("hijack across the drain: conn %v, err %v; want errDraining", got, hijackErr)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("the late connection is still open: %v", err)
	}
}

// An HTTP/2 event stream joins the set when it flushes; a Hijack it then
// tries fails (HTTP/2 has none) and must not take the stream back out:
// the drain would then never end it.
func TestFailedHijackKeepsStream(t *testing.T) {
	a := &App{}
	var ctxErr error
	h := a.endStreamsOnShutdown(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_ = http.NewResponseController(w).Flush()
		if _, _, err := http.NewResponseController(w).Hijack(); err == nil {
			t.Error("the recorder hijacked")
		}
		a.streams.endAll(context.Background())
		ctxErr = r.Context().Err()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events", nil))
	if ctxErr == nil {
		t.Fatal("the drain did not end an event stream whose hijack failed")
	}
}

// flushErrWriter reports a flush failure, as an HTTP/2 stream the
// client reset does.
type flushErrWriter struct {
	http.ResponseWriter
	err error
}

func (f *flushErrWriter) FlushError() error { return f.err }

// The wrapper's void Flush hid FlushError from ResponseController, so a
// stream never learned its client had reset it.
func TestFlushErrorPassesThrough(t *testing.T) {
	a := &App{}
	reset := errors.New("stream reset")
	var got error
	h := a.endStreamsOnShutdown(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		got = http.NewResponseController(w).Flush()
	}))
	h.ServeHTTP(&flushErrWriter{ResponseWriter: httptest.NewRecorder(), err: reset},
		httptest.NewRequest(http.MethodGet, "/events", nil))
	if !errors.Is(got, reset) {
		t.Fatalf("Flush through the wrapper = %v, want %v", got, reset)
	}
}

// A WebSocket whose peer never answers the close frame held its TCP
// connection and read loop for the whole WSConfig.CloseTimeout after
// Shutdown stopped waiting, under batteries already torn down. What is
// still open when the budget ends is closed.
func TestUnackedCloseIsForced(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	a.Router().Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = stream.Upgrade(w, r, stream.WSConfig{
			CheckOrigin:  func(*http.Request) bool { return true },
			CloseTimeout: time.Minute,
		})
	}))
	addr, done := startStreamApp(t, a)
	conn, br := wsHandshake(t, addr, "/ws")
	shutdownFast(t, a, done)
	// The close frame, which this peer never answers, then the end.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.Copy(io.Discard, br); err != nil {
		t.Fatalf("connection still open after Shutdown returned: %v", err)
	}
}

// A WebSocket handed over after its stream was ended (Upgrade's hook
// runs after the hijack) closes with 1001 at once.
func TestLateWebSocketClosed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	opened := make(chan *stream.WebSocketConn, 1)
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := stream.Upgrade(w, r, stream.WSConfig{CheckOrigin: func(*http.Request) bool { return true }, CloseTimeout: 50 * time.Millisecond})
		if err == nil {
			opened <- c
		}
	}))
	conn, br := wsHandshake(t, ln.Addr().String(), "/ws")
	l := &longLived{cancel: func() {}, set: &openStreams{}}
	l.end()
	l.setWS(<-opened)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0]&0x0f != 0x8 {
		t.Fatalf("no close frame for a WebSocket handed over after the end: % x %v", hdr, err)
	}
	if code := uint16(hdr[2])<<8 | uint16(hdr[3]); code != shutdownCloseCode {
		t.Fatalf("close code %d, want %d", code, shutdownCloseCode)
	}
}
