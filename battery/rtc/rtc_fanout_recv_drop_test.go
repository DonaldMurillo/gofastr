package rtc

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// recvDropTrap is an in-process fanout whose subscriptions lose the
// first signal they are handed, the way a backend's subscriber queue
// drops its oldest payload on overflow. Only the target's replica acts
// on a signal (the sender's drops its own envelopes), so this is a loss
// on the receive side.
type recvDropTrap struct {
	fanout.Fanout
	drop bool
}

func (r *recvDropTrap) Subscribe(topic string, fn func([]byte)) (func(), error) {
	var dropped atomic.Bool
	return r.Fanout.Subscribe(topic, func(p []byte) {
		if _, body, err := fanout.Unwrap(p); err == nil && r.drop &&
			bytes.Contains(body, []byte(`"k":"signal"`)) && dropped.CompareAndSwap(false, true) {
			return
		}
		fn(p)
	})
}

// passThrough wraps f in a signalTrap that holds nothing.
func passThrough(f fanout.Fanout) *signalTrap {
	open := make(chan struct{})
	close(open)
	return &signalTrap{Fanout: f, stall: open}
}

// sendTwoSignals sends an offer then an ICE candidate from b to pA.
func sendTwoSignals(b *wsClient) {
	b.send(map[string]any{"kind": "signal", "to": "pA", "type": "offer",
		"data": map[string]any{"sdp": "v=0"}})
	b.send(map[string]any{"kind": "signal", "to": "pA", "type": "ice",
		"data": map[string]any{"candidate": "c"}})
}

// The publish half of a lost cross-replica signal closes the sender
// (R19). A signal the receiving replica's queue dropped was still lost
// in silence: both sockets stayed open waiting on an offer that never
// came (round 7 R7-061). Signals carry a per-pair sequence now, and the
// target's socket closes on a gap so the client reconnects and the pair
// renegotiates.
func TestRecvDroppedSignalClosesTarget(t *testing.T) {
	trap := &recvDropTrap{Fanout: fanout.NewInProcess(), drop: true}
	a, b := lostSignalReplicas(t, passThrough(trap))
	sendTwoSignals(b)
	if !waitSocketClosed(a, 3*time.Second) {
		t.Fatal("target's socket stayed open after the lane dropped a signal to it")
	}
}

// The control: two signals delivered in order leave the target open and
// reach it both.
func TestInOrderSignalsKeepTargetOpen(t *testing.T) {
	trap := &recvDropTrap{Fanout: fanout.NewInProcess()}
	a, b := lostSignalReplicas(t, passThrough(trap))
	sendTwoSignals(b)
	got := 0
	deadline := time.Now().Add(3 * time.Second)
	for got < 2 && time.Now().Before(deadline) {
		env, err := a.recvEnv(time.Until(deadline))
		if err != nil {
			t.Fatalf("target socket failed after %d signals: %v", got, err)
		}
		if env.Type == "signal" {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("target got %d signals, want 2", got)
	}
	a.expectSilence(t, 300*time.Millisecond)
}

// A reconnect on either side restarts a count without reading as a loss:
// a new target seat (Order) restarts the sender's numbers at 1, and a
// sender whose socket restarted its count (1 after 7) is a new run.
func TestSignalSeqRestartsNotGaps(t *testing.T) {
	var from, to roomPeer
	if n := from.nextSignalSeqLocked("pA", 100); n != 1 {
		t.Fatalf("first seq = %d, want 1", n)
	}
	from.nextSignalSeqLocked("pA", 100)
	if n := from.nextSignalSeqLocked("pA", 200); n != 1 {
		t.Fatalf("seq after the target's new seat = %d, want 1", n)
	}
	for _, step := range []struct {
		seq uint64
		gap bool
	}{{1, false}, {2, false}, {7, true}, {1, false}, {2, false}, {0, false}} {
		if got := to.signalGapLocked("pB", step.seq); got != step.gap {
			t.Fatalf("seq %d: gap = %v, want %v", step.seq, got, step.gap)
		}
	}
	var fresh roomPeer
	if !fresh.signalGapLocked("pB", 2) {
		t.Fatal("a first signal numbered 2 hid the loss of number 1")
	}
}
