package stream

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// TestFanoutReceiveSurvivesBlockSubscriber is the regression for the
// slow=block wedge: a slow=block subscriber on broker B whose channel is full
// (its Subscribe loop stalled on a slow TCP write) must NOT wedge broker B's
// fanout-receive lane. A healthy subscriber must still receive a remote event
// published on broker A.
//
// Before the fix the fanout-receive callback invoked deliverLocal, whose
// block-mode send `select { case ch<-evt: case <-done: }` blocked forever
// once the block subscriber's bounded channel was full and its loop stopped
// draining — stalling delivery to EVERY other subscriber on B. Remote-origin
// delivery now uses deliverFromFanout, which is always drop-oldest.
func TestFanoutReceiveSurvivesBlockSubscriber(t *testing.T) {
	f := fanout.NewInProcess()
	brokerA := NewSSEBroker(SSEBrokerConfig{Topic: "t", Fanout: f})
	brokerB := NewSSEBroker(SSEBrokerConfig{Topic: "t", Fanout: f})
	defer brokerA.Close()
	defer brokerB.Close()

	// block sub: cap 1, block mode, loop never drains (we never serve it),
	// so its channel fills and stays full — the exact condition that wedged
	// the old deliverLocal-on-fanout-receive path.
	blockSub := &subscriber{
		ch:       make(chan sseEvent, 1),
		done:     make(chan struct{}),
		slowMode: sseSlowBlock,
	}
	healthySub := &subscriber{
		ch:       make(chan sseEvent, 8),
		done:     make(chan struct{}),
		slowMode: sseSlowDropOldest,
	}
	brokerB.mu.Lock()
	brokerB.subscribers["block"] = blockSub
	brokerB.subscribers["healthy"] = healthySub
	brokerB.mu.Unlock()

	// Fill the block sub's channel (cap 1) so a blocking send would wedge.
	blockSub.ch <- sseEvent{Name: "msg", Data: "fill"}

	// Remote publish from A. Under the bug this wedged B's fanout-receive
	// goroutine; the healthy subscriber never saw the marker.
	brokerA.Publish("msg", "marker")

	select {
	case evt := <-healthySub.ch:
		if evt.Data != "marker" {
			t.Fatalf("healthy subscriber got %q, want marker", evt.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fanout receive wedged: healthy subscriber never got the remote marker while a block subscriber was stalled")
	}
}

// TestBlockSubDisconnectUnblocksLocalPublish: when a slow=block subscriber's
// Subscribe loop exits and owns its own removal, it must close(sub.done) so a
// concurrent LOCAL Publish blocked on the block-mode channel send unblocks
// instead of hanging forever. (Local Publish keeps block-mode backpressure;
// the disconnect unblocks it via the done channel — the symmetric guard to
// deliverFromFanout's drop-oldest.)
func TestBlockSubDisconnectUnblocksLocalPublish(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{Topic: "t"})
	blockSub := &subscriber{
		ch:       make(chan sseEvent, 1),
		done:     make(chan struct{}),
		slowMode: sseSlowBlock,
	}
	blockSub.ch <- sseEvent{Name: "msg", Data: "fill"} // full
	b.mu.Lock()
	b.subscribers["block"] = blockSub
	b.mu.Unlock()

	// Simulate Subscribe's disconnect defer for a self-owned removal.
	go func() {
		time.Sleep(50 * time.Millisecond)
		b.mu.Lock()
		if cur, ok := b.subscribers["block"]; ok && cur == blockSub {
			delete(b.subscribers, "block")
			close(blockSub.done)
		}
		b.mu.Unlock()
	}()

	done := make(chan struct{})
	go func() {
		b.deliverLocal("msg", "after-disconnect", "") // block send selects on <-done
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deliverLocal stayed blocked after the block subscriber was removed and done closed")
	}
}
