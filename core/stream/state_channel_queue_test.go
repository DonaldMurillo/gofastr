package stream

import (
	"testing"
	"testing/synctest"
	"time"
)

// gatedSource is a bannerSource whose SnapshotFor blocks while gate is
// set: a Run loop wedged behind one slow hydration, so the job queue
// fills behind it.
type gatedSource struct {
	*bannerSource
	gate chan struct{}
}

func (g *gatedSource) SnapshotFor(role string) (bannerState, uint64) {
	if role == "gated" {
		<-g.gate
	}
	return g.bannerSource.SnapshotFor(role)
}

// wedgeChannel registers healthy, then parks the Run loop inside a
// gated conn's SnapshotFor and publishes n events behind it.
func wedgeChannel(t *testing.T, closeOnOverflow bool, n int) (c *StateChannel[string, bannerState, bannerEvent], healthy, gated *WebSocketConn, release func()) {
	src := &gatedSource{bannerSource: newBannerSource(), gate: make(chan struct{})}
	c = NewStateChannel[string, bannerState, bannerEvent](src)
	c.CloseOnOverflow(closeOnOverflow)
	go c.Run()
	t.Cleanup(func() {
		c.Stop()
		time.Sleep(2 * time.Second)
	})
	healthy = newChannelConn(2 * losslessQueueDepth)
	c.Connect("admin", healthy)
	recvEnvelope(t, healthy)

	gated = newChannelConn(2 * losslessQueueDepth)
	go c.Connect("gated", gated)
	synctest.Wait() // the loop is parked in the gated SnapshotFor
	for range n {
		c.Publish("banner", bannerEvent{Action: "tick"})
	}
	return c, healthy, gated, func() { close(src.gate); synctest.Wait() }
}

func isClosed(conn *WebSocketConn) bool {
	select {
	case <-conn.Closed():
		return true
	default:
		return false
	}
}

// TestQueueOverflowClosesListeners: with CloseOnOverflow, an event the
// job queue cannot take is not lost in silence. Every connection that
// would have received it is closed, the one registered before the
// stall and the one hydrating during it, so each reconnects and
// re-hydrates.
func TestQueueOverflowClosesListeners(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, healthy, gated, release := wedgeChannel(t, true, losslessQueueDepth+10)
		release()
		if !isClosed(healthy) || !isClosed(gated) {
			t.Fatalf("an event was dropped at the full queue; healthy closed=%v gated closed=%v, want both", isClosed(healthy), isClosed(gated))
		}
	})
}

// TestLosslessQueueHoldsBurst: under CloseOnOverflow the queue holds a
// burst well past the lossy depth, so a busy room is not torn down by
// a short stall.
func TestLosslessQueueHoldsBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, healthy, gated, release := wedgeChannel(t, true, losslessQueueDepth-1)
		release()
		if isClosed(healthy) || isClosed(gated) {
			t.Fatal("a burst within the lossless depth closed a connection")
		}
		if got := len(healthy.sendBuffer); got != losslessQueueDepth-1 {
			t.Fatalf("healthy conn queued %d events, want %d", got, losslessQueueDepth-1)
		}
	})
}

// TestLossyQueueStillDrops: without CloseOnOverflow the queue keeps its
// lossy depth and drops past it, the Hub posture presence-shaped
// sources rely on; no connection closes.
func TestLossyQueueStillDrops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, healthy, gated, release := wedgeChannel(t, false, lossyQueueDepth+10)
		release()
		if isClosed(healthy) || isClosed(gated) {
			t.Fatal("lossy channel closed a connection on a full queue")
		}
		// The gated snapshot job is in flight, so the queue holds
		// lossyQueueDepth events behind it.
		if got := len(healthy.sendBuffer); got != lossyQueueDepth {
			t.Fatalf("healthy conn queued %d events, want %d", got, lossyQueueDepth)
		}
	})
}
