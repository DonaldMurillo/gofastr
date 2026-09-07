// Package us holds the unseated fixtures: long-lived stream surfaces
// with and without a seat acquisition on the admission path, plus the
// silent postures. Names are unrelated to the repo's surfaces on
// purpose: the shape is the open + park + seat test, not the names.
package us

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
)

// events is the package's fan-out source: a channel the parked loops
// below receive from.
func events() <-chan string { return make(chan string, 1) }

// openSSE is the plain oracle shape: header set, park inline, no seat.
func openSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream") // want `long-lived stream opened here`
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// openViaHelpers splits the shape across the package: the header set
// lives in beginStream, the park in holdStream. Reachability, not
// adjacency, is the test.
func openViaHelpers(w http.ResponseWriter, r *http.Request) {
	beginStream(w)
	holdStream(r.Context(), w)
}

func beginStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream") // want `long-lived stream opened here`
	w.WriteHeader(http.StatusOK)
}

func holdStream(ctx context.Context, w http.ResponseWriter) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// openUpgrade is the websocket oracle shape: Upgrade then a read loop.
func openUpgrade(w http.ResponseWriter, r *http.Request) {
	conn := upgradeConn(w, r)
	if conn == nil {
		return
	}
	for {
		if _, err := conn.Read(); err != nil {
			return
		}
	}
}

// fakeConn stands in for the repo's websocket conn.
type fakeConn struct{ c net.Conn }

func (c *fakeConn) Read() ([]byte, error) {
	buf := make([]byte, 1)
	_, err := c.c.Read(buf)
	return buf, err
}

func upgradeConn(w http.ResponseWriter, r *http.Request) *fakeConn {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil
	}
	netConn, rw, err := hj.Hijack() // want `long-lived stream opened here`
	if err != nil {
		return nil
	}
	_ = rw
	return &fakeConn{c: netConn}
}

// openHijackGoPump parks in a spawned goroutine: the go target's read
// loop parks the opener (the harness ws shape).
func openHijackGoPump(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack() // want `long-lived stream opened here`
	if err != nil {
		return
	}
	go pump(conn)
}

func pump(conn net.Conn) {
	buf := make([]byte, 512)
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}

// ---- seated: the fix posture, quiet ---------------------------------

type seatTable struct {
	perCaller map[string]int
}

func (t *seatTable) seatsFor(n int) int {
	if n == 0 {
		return 16
	}
	return n
}

// seatedSSE admits through the house seat policy: same open, same
// park, but a seat acquisition on the path.
func seatedSSE(w http.ResponseWriter, r *http.Request) {
	t := &seatTable{perCaller: map[string]int{}}
	if t.seatsFor(0) <= len(t.perCaller) {
		http.Error(w, "too many streams", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// seatedCounter is the hand-rolled admission spelling: a package-local
// counter over a per-caller map compared against a cap.
func seatedCounter(w http.ResponseWriter, r *http.Request) {
	if !admit(r.RemoteAddr) {
		http.Error(w, "too many streams", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

var subscribers = map[string]int{}

const maxSubs = 16

func admit(caller string) bool {
	subscribers[caller]++
	return len(subscribers) < maxSubs
}

// ---- silent postures -------------------------------------------------

// oneShotSSE opens but never parks: one event, then return (the
// core/mcp ssePostHandler posture).
func oneShotSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "data: done\n\n")
}

// callerDrivenPump ranges over its own channel parameter: the channel
// owner one frame up owns the admission decision (the
// handler.SSEStream posture).
func callerDrivenPump(w http.ResponseWriter, events <-chan string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for ev := range events {
		fmt.Fprintf(w, "data: %s\n\n", ev)
	}
}

// hijackWrapper forwards Hijack for the middleware stack: the wrapper
// posture, quiet however many streams flow through it.
type hijackWrapper struct {
	rw http.ResponseWriter
}

func (h *hijackWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := h.rw.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}

// refuseUpgrade upgrades only to close: no park is reachable (the rtc
// refuse posture).
func refuseUpgrade(w http.ResponseWriter, r *http.Request) {
	conn := upgradeConn(w, r)
	if conn == nil {
		return
	}
	_ = conn.c.Close()
}
