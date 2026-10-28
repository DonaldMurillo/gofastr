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

// clearedOnlyFirst wipes the inherited deadline once, then arms one per
// write and never clears it: the clear comes before every arm, so each
// write still leaves a timer running through the idle gap.
func clearedOnlyFirst(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
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

func batches() <-chan chan string { return make(chan chan string, 1) }

// clearedAfterInnerLoop clears per batch, but the inner loop parks on
// the batch under the deadline its last write armed.
func clearedAfterInnerLoop(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for batch := range batches() {
		for ev := range batch {
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
			_ = rc.Flush()
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedPerChunk writes a batch's chunks from a slice, which never
// parks, and clears before waiting on the next event.
func clearedPerChunk(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for _, chunk := range []string{ev, ev} {
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		_ = rc.Flush()
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedPerChunkPolled checks for cancellation between chunks with a
// select that has a default, which never blocks, so the per-event clear
// still comes before the only wait.
func clearedPerChunkPolled(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.Flush()
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

func groups() <-chan chan chan string { return make(chan chan chan string, 1) }

// clearedAfterPolledOperand drains with a select that has a default,
// but the select evaluates its operand first: <-sources waits on an
// empty, open group under the deadline the last write armed.
func clearedAfterPolledOperand(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for sources := range groups() {
	drain:
		for {
			select {
			case ev := <-(<-sources):
				_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
				fmt.Fprintf(w, "data: %s\n\n", ev)
				_ = rc.Flush()
			default:
				break drain
			}
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterPolledSend forwards with a select that has a default; the
// send's value, <-sources, is evaluated first and waits the same way.
func clearedAfterPolledSend(w http.ResponseWriter, r *http.Request, relay chan chan string) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for sources := range groups() {
	drain:
		for {
			select {
			case relay <- (<-sources):
				_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
				fmt.Fprintf(w, "data: relayed\n\n")
				_ = rc.Flush()
			default:
				break drain
			}
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterPolledDest drains into an index read from slots: the
// select evaluates a receive's destination once the case is chosen, so
// <-slots waits before the clause body runs.
func clearedAfterPolledDest(w http.ResponseWriter, r *http.Request, slots chan int) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for events := range batches() {
		saved := []string{""}
	drain:
		for {
			select {
			case saved[<-slots] = <-events:
				_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
				fmt.Fprintf(w, "data: %s\n\n", saved[0])
				_ = rc.Flush()
			default:
				break drain
			}
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterSendSelect hands each chunk on through a select with no
// default: it waits on relay even with no receive case, under the
// deadline the previous write armed.
func clearedAfterSendSelect(w http.ResponseWriter, r *http.Request, relay chan string) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			select {
			case relay <- ev:
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterSend is the same wait as a bare send.
func clearedAfterSend(w http.ResponseWriter, r *http.Request, relay chan string) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			relay <- ev
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterPolledBody polls for cancellation, but the default clause
// itself waits on relay.
func clearedAfterPolledBody(w http.ResponseWriter, r *http.Request, relay chan string) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
				relay <- ev
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterInnerRange waits on a channel range ahead of the arm, in
// a counted loop that holds it.
func clearedAfterInnerRange(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			for range feed() {
				break
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

type frames struct{}

func (frames) ReadFrame() ([]byte, error) { return nil, nil }

// clearedAfterInnerRead reads a frame from the client between writes.
func clearedAfterInnerRead(w http.ResponseWriter, r *http.Request, in frames) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		for i := 0; i < 2; i++ {
			_, _ = in.ReadFrame()
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// clearedAfterLoop clears once the loop ends: every wait for the next
// event still runs under the deadline the last write armed.
func clearedAfterLoop(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
		fmt.Fprintf(w, "data: %s\n\n", ev)
		_ = rc.Flush()
	}
	_ = rc.SetWriteDeadline(time.Time{})
}

// helperClearedAfterLoop is the same gap through a helper that arms.
type helperAfterLoop struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openHelperAfterLoop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &helperAfterLoop{w: w, rc: http.NewResponseController(w)}
	for ev := range feed() {
		h.arm()
		fmt.Fprintf(h.w, "data: %s\n\n", ev)
		_ = h.rc.Flush()
	}
	_ = h.rc.SetWriteDeadline(time.Time{})
}

func (h *helperAfterLoop) arm() { _ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) } // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`

func sliceBatches() <-chan []string { return make(chan []string, 1) }

// pacedSliceBatch paces the writes of each batch: the range over the
// slice does not wait, but its body does, under the last write's
// deadline.
func pacedSliceBatch(w http.ResponseWriter, r *http.Request, pace *time.Ticker) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for batch := range sliceBatches() {
		for _, ev := range batch {
			<-pace.C
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			fmt.Fprintf(w, "data: %s\n\n", ev)
			_ = rc.Flush()
		}
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// deferredInLoop defers the clear inside the loop: it runs when the
// handler returns, not before the next wait.
func deferredInLoop(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
		defer rc.SetWriteDeadline(time.Time{})
		fmt.Fprintf(w, "data: %s\n\n", ev)
		_ = rc.Flush()
	}
}

// clearedOnErrorOnly clears when the write fails; a write that succeeds
// leaves the deadline armed through the next wait.
func clearedOnErrorOnly(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
		if _, err := fmt.Fprintf(w, "data: %s\n\n", ev); err != nil {
			_ = rc.SetWriteDeadline(time.Time{})
			return
		}
		_ = rc.Flush()
	}
}

// clearedOnSkipOnly clears before skipping an event it cannot encode,
// and in a panic it does not survive; the events it writes leave the
// deadline armed.
func clearedOnSkipOnly(w http.ResponseWriter, r *http.Request, encode func(string) (string, error)) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
		body, err := encode(ev)
		switch {
		case err != nil:
			_ = rc.SetWriteDeadline(time.Time{})
			continue
		case body == "":
			_ = rc.SetWriteDeadline(time.Time{})
			panic("empty event")
		}
		fmt.Fprintf(w, "data: %s\n\n", body)
		_ = rc.Flush()
	}
}

// deferredOnErrorOnly registers its deferred clear only when the write
// fails; a write that succeeds returns with the deadline armed, and the
// caller waits for the next event under it.
type deferredOnError struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openDeferredOnError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &deferredOnError{w: w, rc: http.NewResponseController(w)}
	for ev := range feed() {
		if h.send(ev) != nil {
			return
		}
	}
}

func (h *deferredOnError) send(ev string) error {
	_ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
	if _, err := fmt.Fprintf(h.w, "data: %s\n\n", ev); err != nil {
		defer h.rc.SetWriteDeadline(time.Time{})
		return err
	}
	return h.rc.Flush()
}

// clearedOnGotoOnly jumps out on a failed write; only that path clears.
func clearedOnGotoOnly(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
		if _, err := fmt.Fprintf(w, "data: %s\n\n", ev); err != nil {
			_ = rc.SetWriteDeadline(time.Time{})
			goto done
		}
		_ = rc.Flush()
	}
done:
}

// deferredInSelectLoop defers the clear inside a select loop: it runs
// when the handler returns, after every later wait.
func deferredInSelectLoop(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
			defer rc.SetWriteDeadline(time.Time{})
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// ackedHelper writes, then waits for the client's ack before it
// returns: the deferred clear runs after that wait.
type ackedHelper struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	acks chan struct{}
}

func openAckedHelper(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &ackedHelper{w: w, rc: http.NewResponseController(w), acks: make(chan struct{})}
	for ev := range feed() {
		h.send(ev)
	}
}

func (h *ackedHelper) send(ev string) {
	_ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) // want `write deadline armed on an http.ResponseController in a long-lived stream and never cleared`
	defer h.rc.SetWriteDeadline(time.Time{})
	fmt.Fprintf(h.w, "data: %s\n\n", ev)
	_ = h.rc.Flush()
	<-h.acks
}

// ---- quiet: the fix spellings -----------------------------------------

// drainedUnlabeled drains a burst in an inner loop it leaves with a bare
// break, clearing on the way out: the break stays in the event loop.
func drainedUnlabeled(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for first := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprintf(w, "data: %s\n\n", first)
		for {
			select {
			case ev := <-feed():
				fmt.Fprintf(w, "data: %s\n\n", ev)
				continue
			default:
			}
			_ = rc.Flush()
			_ = rc.SetWriteDeadline(time.Time{})
			break
		}
	}
}

// polledClearBreak clears in a select's default clause that ends in a
// break, which leaves only the select.
func polledClearBreak(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprintf(w, "data: %s\n\n", ev)
		select {
		case <-r.Context().Done():
			return
		default:
			_ = rc.Flush()
			_ = rc.SetWriteDeadline(time.Time{})
			break
		}
	}
}

// drainedThenCleared writes the first event of a burst, drains what is
// already buffered without waiting, and clears on the way out of the
// drain: break drain stays inside the event loop, before its next wait.
func drainedThenCleared(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for first := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprintf(w, "data: %s\n\n", first)
	drain:
		for {
			select {
			case ev := <-feed():
				fmt.Fprintf(w, "data: %s\n\n", ev)
			default:
				_ = rc.Flush()
				_ = rc.SetWriteDeadline(time.Time{})
				break drain
			}
		}
	}
}

// caseHelper arms, writes and clears inside one switch case that
// returns: the clear is on the arm's own path.
type caseHelper struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openCaseHelper(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &caseHelper{w: w, rc: http.NewResponseController(w)}
	for ev := range feed() {
		h.send("event", ev)
	}
}

func (h *caseHelper) send(kind, ev string) {
	switch kind {
	case "event":
		_ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		fmt.Fprintf(h.w, "data: %s\n\n", ev)
		_ = h.rc.Flush()
		_ = h.rc.SetWriteDeadline(time.Time{})
		return
	}
}

// gatedHelper waits for a write slot before it arms, then defers its
// clear: the only wait comes before the arm, so the clear at return
// still lands before the caller waits again.
type gatedHelper struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	gate chan struct{}
}

func openGatedHelper(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &gatedHelper{w: w, rc: http.NewResponseController(w), gate: make(chan struct{}, 1)}
	for ev := range feed() {
		h.send(r.Context(), ev)
	}
}

func (h *gatedHelper) send(ctx context.Context, ev string) {
	select {
	case h.gate <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-h.gate }()
	_ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	defer h.rc.SetWriteDeadline(time.Time{})
	fmt.Fprintf(h.w, "data: %s\n\n", ev)
	_ = h.rc.Flush()
}

// clearedOnBothPaths clears on the error path and again after the
// flush.
func clearedOnBothPaths(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	for ev := range feed() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "data: %s\n\n", ev); err != nil {
			_ = rc.SetWriteDeadline(time.Time{})
			return
		}
		_ = rc.Flush()
		_ = rc.SetWriteDeadline(time.Time{})
	}
}

// deferredHelper writes through a helper that defers its clear before
// it arms: every return clears before the loop waits again.
type deferredHelper struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openDeferredHelper(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &deferredHelper{w: w, rc: http.NewResponseController(w)}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			h.send(ev)
		}
	}
}

func (h *deferredHelper) send(ev string) {
	defer h.rc.SetWriteDeadline(time.Time{})
	_ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(h.w, "data: %s\n\n", ev)
	_ = h.rc.Flush()
}

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

// helperArmed is the core/stream SSEWriter spelling: a helper arms the
// deadline, and every caller clears it after its flush.
type helperArmed struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openHelperArmed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &helperArmed{w: w, rc: http.NewResponseController(w)}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			h.send(ev)
		}
	}
}

func (h *helperArmed) arm() { _ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) }

func (h *helperArmed) send(ev string) {
	h.arm()
	fmt.Fprintf(h.w, "data: %s\n\n", ev)
	_ = h.rc.Flush()
	_ = h.rc.SetWriteDeadline(time.Time{})
}

// twoBranch arms through a helper on both branches of send, and the
// loop clears after each send.
type twoBranch struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func openTwoBranch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	h := &twoBranch{w: w, rc: http.NewResponseController(w)}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			h.send(ev)
			_ = h.rc.SetWriteDeadline(time.Time{})
		}
	}
}

func (h *twoBranch) arm() { _ = h.rc.SetWriteDeadline(time.Now().Add(10 * time.Second)) }

func (h *twoBranch) send(ev string) {
	if len(ev) > 64 {
		h.arm()
		fmt.Fprintf(h.w, "data: %s\n\n", ev[:64])
	} else {
		h.arm()
		fmt.Fprintf(h.w, "data: %s\n\n", ev)
	}
	_ = h.rc.Flush()
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
