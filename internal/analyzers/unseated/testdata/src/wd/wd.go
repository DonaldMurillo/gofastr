// Package wd holds the nowritedeadline fixtures: client writes inside
// long-lived stream park loops, with and without a write deadline on
// the loop's path, plus the silent postures.
package wd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

func feed() <-chan string { return make(chan string, 1) }

// ---- firing ------------------------------------------------------------

// rawSSE is the kiln / harness web shape (K24, H26): Fprintf plus Flush
// on the response, no deadline anywhere.
func rawSSE(w http.ResponseWriter, r *http.Request) {
	fl := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "data: %s\n\n", ev) // want `client write in a long-lived stream's park loop with no write deadline`
			fl.Flush()
		}
	}
}

// sseWriterLoop is the CRUD event stream / UI stream shape (D44, R27):
// core/stream's SSEWriter arms a fresh write deadline on every write, the
// way WebSocketConn's frame write does, so it is bounded.
func sseWriterLoop(w http.ResponseWriter, r *http.Request) {
	sse := stream.NewSSEWriter(w)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if err := sse.WriteComment("ping"); err != nil {
				return
			}
		}
	}
}

// hijacked is the harness ws shape (H28): the event pump writes frames
// through a bufio writer over the hijacked conn, via a helper.
type hijacked struct {
	conn net.Conn
	bw   *bufio.Writer
}

func serveHijack(w http.ResponseWriter, r *http.Request) {
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	h := &hijacked{conn: conn, bw: rw.Writer}
	go h.pump(r.Context())
}

func (h *hijacked) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-feed():
			h.writeFrame([]byte(ev)) // want `client write in a long-lived stream's park loop with no write deadline`
		}
	}
}

func (h *hijacked) writeFrame(b []byte) {
	_, _ = h.bw.Write(b)
	_ = h.bw.Flush()
}

// controllerFlush flushes through a ResponseController and never arms
// its deadline.
func controllerFlush(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case <-feed():
			_ = rc.Flush() // want `client write in a long-lived stream's park loop with no write deadline`
		}
	}
}

// encodeLoop streams JSON lines through json.NewEncoder(w).
func encodeLoop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = json.NewEncoder(w).Encode(ev) // want `client write in a long-lived stream's park loop with no write deadline`
	}
}

// armedNeverCleared bounds each write but leaves the deadline armed. On
// HTTP/2 the deadline is a stream timer that resets the stream when it
// fires, write pending or not, so the stream dies in its first idle gap
// longer than the deadline (core/stream's SSEWriter before the fix).
func armedNeverCleared(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
			_ = rc.Flush()
		}
	}
}

// ---- quiet: the fix spellings -----------------------------------------

// perWriteDeadline is the core/mcp GET spelling: a closure arms a fresh
// deadline before every write and the loop calls it.
func perWriteDeadline(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(ev string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprintf(w, "data: %s\n\n", ev)
		ok := rc.Flush() == nil
		_ = rc.SetWriteDeadline(time.Time{})
		return ok
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if !write(ev) {
				return
			}
		}
	}
}

// deadlineStream is the core/a2a spelling: the helper the loop writes
// through arms the deadline.
type deadlineStream struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openDeadlineStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	st := &deadlineStream{w: w, rc: http.NewResponseController(w)}
	st.forward(r.Context())
}

func (st *deadlineStream) forward(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-feed():
			st.write(ev)
		}
	}
}

func (st *deadlineStream) write(ev string) {
	_ = st.rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(st.w, "data: %s\n\n", ev)
	_ = st.rc.Flush()
	_ = st.rc.SetWriteDeadline(time.Time{})
}

// brokerCut is the SSEBroker spelling: the stream installs a
// past-deadline cut the evicting path calls.
type sub struct{ cut func() }

func brokerCut(w http.ResponseWriter, r *http.Request, register func(*sub)) {
	rc := http.NewResponseController(w)
	register(&sub{cut: func() { _ = rc.SetWriteDeadline(time.Now()) }})
	sse := stream.NewSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			_ = sse.WriteEvent("e", ev)
		}
	}
}

// connDeadline bounds a hijacked conn's writes with SetDeadline.
func connDeadline(w http.ResponseWriter, r *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_, _ = conn.Write([]byte(ev))
		}
	}
}

// ---- quiet: silent postures -------------------------------------------

// wsConnLoop writes through core/stream's WebSocketConn, whose
// writeFrame arms WSConfig.WriteTimeout per frame.
func wsConnLoop(w http.ResponseWriter, r *http.Request) {
	conn, err := stream.Upgrade(w, r)
	if err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			_ = conn.WriteString(ev)
		}
	}
}

// oneShot writes once with no park: the core/mcp POST answer (P46) is
// this shape and is out of scope here.
func oneShot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: done\n\n")
	w.(http.Flusher).Flush()
}

// oneShotDeadline bounds its one write and returns: no idle gap follows,
// so the deadline it leaves armed never fires on a live stream.
func oneShotDeadline(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprint(w, "data: done\n\n")
	_ = rc.Flush()
}

// callerDriven ranges over its own channel parameter (handler.SSEStream):
// the channel's owner one frame up owns the stream.
func callerDriven(w http.ResponseWriter, events <-chan string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range events {
		fmt.Fprintf(w, "data: %s\n\n", ev)
	}
}

// bufferLoop parks and writes, but into a buffer, not the client.
func bufferLoop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	var buf bytes.Buffer
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			buf.WriteString(ev)
			fmt.Fprintf(&buf, "%s", ev)
		}
	}
}

// noOpen parks and writes to a ResponseWriter but no stream open is on
// its path (no event-stream header, upgrade, hijack, or SSEWriter): not
// a recognised long-lived stream.
func noOpen(w http.ResponseWriter, r *http.Request) {
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "%s\n", ev)
		}
	}
}
