package moduleproto

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

// servingHost starts a host Peer on one end of a pipe with a "wait" handler
// that blocks until its ctx ends, sends it one request from the raw other
// end, and returns once the handler is running. ended closes when the
// handler's ctx is cancelled.
func servingHost(t *testing.T) (h *Peer, raw net.Conn, ended <-chan struct{}) {
	t.Helper()
	connX, connY := net.Pipe()
	codec, err := NewCodec(connX, connX, 0)
	if err != nil {
		t.Fatal(err)
	}
	h = NewPeer(codec, RoleHost)
	started, done := make(chan struct{}), make(chan struct{})
	if err := h.Handle("wait", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(done)
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	h.Start()
	t.Cleanup(func() {
		_ = h.Close()
		_ = connX.Close()
		_ = connY.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, connY) }()
	if _, err := connY.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"wait"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	return h, connY, done
}

func waitEnded(t *testing.T, ended <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal(what)
	}
}

// A protocol fault ends the peer: the handlers it was serving are
// cancelled with it. setFatal marked the peer closed, so the Close that
// followed returned before its cancel loop and a handler waiting on
// ctx.Done() lived on.
func TestFaultCancelsServedHandlers(t *testing.T) {
	h, raw, ended := servingHost(t)
	if _, err := raw.Write([]byte(`{"jsonrpc":"2.0","id":999,"result":{}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	<-h.FatalDone()
	waitEnded(t, ended, "handler still running after the protocol fault")
}

// Close cancels served handlers while the transport is still up.
func TestCloseCancelsServedHandlers(t *testing.T) {
	h, _, ended := servingHost(t)
	_ = h.Close()
	waitEnded(t, ended, "handler still running after Close")
}

// The transport ending (the child exited) cancels served handlers too.
func TestReadEOFCancelsServedHandlers(t *testing.T) {
	h, raw, ended := servingHost(t)
	_ = raw.Close()
	<-h.Done()
	waitEnded(t, ended, "handler still running after the read loop exited")
}
