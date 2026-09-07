//go:build red

package a2a

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: every authenticated long-lived stream surface enforces the house
// per-principal seat cap — 16 (core/stream/seats.go::defaultSeatsPerPrincipal, "the same
// default core/mcp's SSE notification stream uses, so the two seat policies in the tree
// read as one number"; rounds 1-4 pinned it for SSE, MCP, ACP, and the CRUD stream).
// The A2A exchange's two SSE stream families are the one remaining surface with no seat
// accounting: one low-privilege principal pins a forwardEvents/pollEvents goroutine, a
// bus channel, and a keepalive ticker per stream (probe: 64 streams admitted, process
// goroutines 9→329).
// Surfaces: core/a2a/server.go::handleSubscribe :1252-1314 (+pollEvents :1316-1360 for
// the no-local-run fallback) and streamSend :1040-1064 (via forwardEvents :1068-1111);
// framework/a2a.go::mountA2A exposes no seat knob to hosts.
// Finding: 20 SubscribeToTask streams and 20 SendStreamingMessage streams held open by
// ONE owner against tasks whose handler never returns — all admitted, zero refusals
// (verified 2026-09-06).
// Fix direction: seat accounting keyed on Config.Owner's principal with the
// core/stream policy shape (cap 16, refuse-at-connect vs evict-oldest), the seat freed
// when forwardEvents/pollEvents return; a knob on mountA2A for hosts that legitimately
// fan out further.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const a2aSeatCap = 16 // core/stream defaultSeatsPerPrincipal parity

// a2aSeatStream is one flood probe. Unlike harness.openStream it tolerates a
// connect-time refusal (error status / non-SSE content type) instead of
// fataling, because refusal is the behavior the fixed server must exhibit.
type a2aSeatStream struct {
	firstData    chan struct{} // closed once the first data: event arrives
	eof          chan struct{} // closed when the body hits EOF
	events       atomic.Int64  // data events + keepalive comments: each proves liveness
	closeFirstDo sync.Once
	body         io.ReadCloser
}

// a2aSeatOpen dials one streaming method as owner. admitted=true means: HTTP
// 200, text/event-stream, and one data event within timeout.
func a2aSeatOpen(t *testing.T, h *harness, owner, method string, params any, timeout time.Duration) (*a2aSeatStream, bool) {
	t.Helper()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": "seat-1", "method": method, "params": params}
	b, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal seat request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("build seat request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", owner)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false // transport-level refusal
	}
	st := &a2aSeatStream{firstData: make(chan struct{}), eof: make(chan struct{}), body: resp.Body}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		// Refused at connect: not an SSE admission. Close now so a
		// server-side handler is not left blocked on a write.
		_ = resp.Body.Close()
		return st, false
	}
	go st.pump(resp.Body)
	select {
	case <-st.firstData:
		return st, true
	case <-st.eof:
		return st, false // SSE headers, but the stream closed before any event
	case <-time.After(timeout):
		return st, false // no snapshot within the bound: not seated
	}
}

// pump drains the SSE body forever, counting events. It must never stop
// reading: a stalled reader would backpressure the server's keepalive writes
// and kill the very stream being probed.
func (st *a2aSeatStream) pump(body io.Reader) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				st.events.Add(1)
				st.markFirstData()
				data = nil
			}
		case strings.HasPrefix(line, ":"):
			st.events.Add(1) // keepalive comment: liveness proof
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data"), " "))
		}
	}
	st.markFirstData()
	close(st.eof)
}

func (st *a2aSeatStream) markFirstData() {
	st.closeFirstDo.Do(func() { close(st.firstData) })
}

// live reports whether a stream admitted earlier is still streaming: another
// event (the 20ms harness keepalive guarantees traffic) arrives within
// timeout, or the body has ended.
func (st *a2aSeatStream) live(timeout time.Duration) bool {
	before := st.events.Load()
	deadline := time.After(timeout)
	for {
		select {
		case <-st.eof:
			return false
		case <-deadline:
			return false
		default:
			if st.events.Load() > before {
				return true
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// a2aSeatFlood opens `total` streams as ONE owner and returns how many are
// still admitted and streaming afterwards.
func a2aSeatFlood(t *testing.T, h *harness, owner, method string, paramsFor func() any) (seated, total int) {
	t.Helper()
	total = 20
	var held []*a2aSeatStream
	for range total {
		if st, ok := a2aSeatOpen(t, h, owner, method, paramsFor(), 2*time.Second); ok {
			held = append(held, st)
		}
	}
	for _, st := range held {
		if st.live(2 * time.Second) {
			seated++
		}
	}
	return seated, total
}

func TestSubscribeRedSeatCapEnforced(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-release // task stays WORKING: every stream below stays open
		return tc.Complete(TextPart("done"))
	})
	defer close(release)

	// Mint one task whose run is resident and blocked, as the flood owner
	// (the store is owner-scoped, so the subscriber must be the creator).
	task := h.send("flood", map[string]any{"returnImmediately": true})
	h.waitTask("flood", task.ID, TaskStateWorking, 2*time.Second)

	seated, total := a2aSeatFlood(t, h, "flood", MethodSubscribeToTask, func() any {
		return map[string]any{"id": task.ID}
	})
	if seated > a2aSeatCap {
		t.Errorf("SECURITY: [a2a-sse-seat-flood] one principal holds %d concurrent SubscribeToTask SSE streams on one task (bound %d): each pins a forwardEvents goroutine, a bus channel, and a keepalive ticker, and handleSubscribe carries no per-principal seat accounting — %d of %d dials were seated with zero refusals", seated, a2aSeatCap, seated, total)
	}
	if refused := total - seated; refused < total-a2aSeatCap {
		t.Errorf("SECURITY: [a2a-sse-seat-flood] only %d of %d SubscribeToTask streams were refused or closed at the %d-seat bound: the exchange must answer the overflow at connect (or evict-oldest), not seat every caller", refused, total, a2aSeatCap)
	}
}

func TestStreamSendRedSeatCapEnforced(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		<-release // every task stays WORKING: every stream stays open
		return tc.Complete(TextPart("done"))
	})
	defer close(release)

	// Same flood shape through the other SSE family: each dial sends a
	// fresh message, minting a distinct task per stream, all ONE principal.
	seated, total := a2aSeatFlood(t, h, "flood", MethodSendStreamingMessage, func() any {
		return streamSendParams("")
	})
	if seated > a2aSeatCap {
		t.Errorf("SECURITY: [a2a-sse-seat-flood] one principal holds %d concurrent SendStreamingMessage SSE streams across %d distinct tasks (bound %d): each pins a run goroutine, a bus channel, and a keepalive ticker, and streamSend carries no per-principal seat accounting", seated, seated, a2aSeatCap)
	}
	if refused := total - seated; refused < total-a2aSeatCap {
		t.Errorf("SECURITY: [a2a-sse-seat-flood] only %d of %d SendStreamingMessage streams were refused or closed at the %d-seat bound: the exchange must answer the overflow at connect (or evict-oldest), not seat every caller", refused, total, a2aSeatCap)
	}
}
