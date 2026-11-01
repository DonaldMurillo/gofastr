package rtc

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// signalTrap passes every lane message through to an in-process fanout
// except signals, which it fails (fail) or holds until the context ends
// (stall). Holding one signal in Publish lets the queue behind it fill.
type signalTrap struct {
	fanout.Fanout
	fail  bool
	stall chan struct{}
}

func (s *signalTrap) Publish(ctx context.Context, topic string, payload []byte) error {
	if _, body, err := fanout.Unwrap(payload); err == nil && bytes.Contains(body, []byte(`"k":"signal"`)) {
		if s.fail {
			return errors.New("backend down")
		}
		select {
		case <-s.stall:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Fanout.Publish(ctx, topic, payload)
}

// lostSignalReplicas joins pA on one replica and pB on the other through
// trap, and waits until pB's replica knows pA.
func lostSignalReplicas(t *testing.T, trap *signalTrap) (a, b *wsClient) {
	t.Helper()
	s1 := newTestSignaler(t, Config{MaxFramesPerSecond: 1000})
	s2 := newTestSignaler(t, Config{MaxFramesPerSecond: 1000})
	for _, s := range []*Signaler{s1, s2} {
		s.heartbeatEvery = 60 * time.Millisecond
		s.remoteTTL = 2 * time.Second
		if _, err := s.SetFanout(trap); err != nil {
			t.Fatal(err)
		}
	}
	base1, base2 := startSignaler(t, s1), startSignaler(t, s2)
	a, _ = join(t, base1, "room1", "pA")
	t.Cleanup(a.close)
	b, _ = join(t, base2, "room1", "pB")
	t.Cleanup(b.close)
	waitFor(t, 3*time.Second, func() bool { return hasPeer(s2, "room1", "pA") }, "S2 sees pA")
	return a, b
}

// waitSocketClosed reads b until the server closes it or d passes.
func waitSocketClosed(b *wsClient, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := b.recv(time.Until(deadline)); err != nil {
			return errors.Is(err, errClosed) || !isTimeout(err)
		}
	}
	return false
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// A signal to a peer on another replica went through the fanout queue,
// which only logged a failed publish: both sockets stayed open and the
// call waited on an answer that never came (2026-10-27 bug bash R19). The
// sending peer's socket now closes, as on a local overflow, so the client
// renegotiates.
func TestFailedRemoteSignalClosesSender(t *testing.T) {
	trap := &signalTrap{Fanout: fanout.NewInProcess(), fail: true}
	_, b := lostSignalReplicas(t, trap)
	b.send(map[string]any{"kind": "signal", "to": "pA", "type": "offer",
		"data": map[string]any{"sdp": "v=0"}})
	if !waitSocketClosed(b, 3*time.Second) {
		t.Fatal("sender's socket stayed open after its signal failed to publish")
	}
}

// The same for a signal the publish queue dropped on overflow.
func TestDroppedRemoteSignalClosesSender(t *testing.T) {
	trap := &signalTrap{Fanout: fanout.NewInProcess(), stall: make(chan struct{})}
	_, b := lostSignalReplicas(t, trap)
	// Registered after the signalers, so it runs before their Close waits
	// on the parked publish.
	t.Cleanup(func() { close(trap.stall) })
	// The first signal parks in Publish; the queue behind it holds 256.
	go func() {
		for range 300 {
			b.send(map[string]any{"kind": "signal", "to": "pA", "type": "ice",
				"data": map[string]any{"candidate": "c"}})
		}
	}()
	// Well inside the queue's 5s publish timeout, so the close is the
	// overflow's, not the stalled publish failing.
	if !waitSocketClosed(b, 3*time.Second) {
		t.Fatal("sender's socket stayed open after the queue dropped its signal")
	}
}
