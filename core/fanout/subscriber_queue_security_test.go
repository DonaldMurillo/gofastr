package fanout

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Property: panic isolation at extension points. A panicking subscriber
// callback is third-party code running on a goroutine the framework owns
// (SubscriberQueue); an unrecovered panic there terminates the whole
// process, the same class framework/hook guards with runHookSafely.
//
// Surfaces routed through core/fanout.SubscriberQueue (all share this
// primitive, so the property is proven once at the primitive and once
// end-to-end through InProcess):
//   - InProcess.Subscribe
//   - redisFanout.Subscribe (NewRedis)
//   - PostgresFanout.Subscribe (framework/fanout)
//   - PublishQueue (its drain goroutine calls fn too)
//
// Proven by re-exec: the child process subscribes a callback that panics on
// the first payload, publishes two payloads, and must exit 0. Today the
// first panic kills the child (unrecovered goroutine panic), so the parent
// observes a crash exit: the failing assertion.

const panicChildEnv = "GOFASTR_TEST_FANOUT_PANIC_CHILD"

func TestSubscriberPanicDoesNotKillProcess(t *testing.T) {
	if os.Getenv(panicChildEnv) == "1" {
		subscriberPanicChild()
		return // unreachable; subscriberPanicChild exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestSubscriberPanicDoesNotKillProcess$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), panicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subscriber panic killed the process: %v\n--- child output ---\n%s",
			err, out)
	}
	if strings.Contains(string(out), "panic:") {
		t.Fatalf("child reported a panic:\n%s", out)
	}
}

// subscriberPanicChild is the child-side scenario. It never returns.
func subscriberPanicChild() {
	var hits atomic.Int32
	delivered := make(chan struct{}, 1)

	f := NewInProcess()
	cancel, err := f.Subscribe("orders", func(payload []byte) {
		n := hits.Add(1)
		if n == 1 {
			// A third-party subscriber bug: bad cast, nil map write, index
			// out of range on a payload an attacker influenced.
			panic("subscriber bug on first payload")
		}
		delivered <- struct{}{}
	})
	if err != nil {
		os.Exit(10)
	}
	defer cancel()

	// First publish: the callback panics inside the queue goroutine.
	// Without recover() this terminates the process (exit status 2).
	if err := f.Publish(context.Background(), "orders", []byte(`{"a":1}`)); err != nil {
		os.Exit(11)
	}
	// Second publish: proves the subscriber goroutine (and the fanout)
	// survived the first panic and kept delivering.
	if err := f.Publish(context.Background(), "orders", []byte(`{"a":2}`)); err != nil {
		os.Exit(12)
	}
	select {
	case <-delivered:
		os.Exit(0)
	case <-time.After(5 * time.Second):
		// Goroutine died or stalled: second payload never delivered.
		os.Exit(13)
	}
}

// ---------------------------------------------------------------------------
// Property: a slow subscriber is EVICTED (payloads dropped), never allowed
// to backpressure the publisher — send must return promptly even while fn
// is blocked and the queue is full. This is the lossy-lane contract every
// transport goroutine (LISTEN/NOTIFY dispatcher, Redis reader) relies on:
// one stuck callback must not stall the shared dispatch lane.
// ---------------------------------------------------------------------------

func TestSubscriberQueueSendNeverBlocksOnSlowFn(t *testing.T) {
	started := make(chan []byte, 1)
	release := make(chan struct{})
	send, stop := SubscriberQueue(func(p []byte) {
		started <- p
		<-release
	}, 2)
	defer stop()

	send([]byte("first"))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fn never received the first payload")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 10 {
			send([]byte{byte('a' + i)}) // queue (depth 2) overflows immediately
		}
	}()
	select {
	case <-done:
		// All sends returned while fn was still blocked: no backpressure.
	case <-time.After(2 * time.Second):
		t.Fatal("SECURITY: [fanout] send blocked while the subscriber callback was stuck (slow subscriber backpressured the publisher)")
	}
	close(release)
}

// ---------------------------------------------------------------------------
// Property: on overflow the OLDEST queued payload is dropped and the NEWEST
// survives — a consumer catching up must see the freshest state, never a
// stalled prefix of stale payloads.
// ---------------------------------------------------------------------------

func TestSubscriberQueueDropOldestKeepsNewest(t *testing.T) {
	got := make(chan string, 4)
	release := make(chan struct{})
	send, stop := SubscriberQueue(func(p []byte) {
		got <- string(p)
		<-release
	}, 1)
	defer stop()

	send([]byte("p1"))
	select {
	case first := <-got:
		if first != "p1" {
			t.Fatalf("first delivered = %q, want p1", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fn never received p1")
	}
	// fn is now blocked holding p1; queue depth 1. p2 is dropped when p3
	// arrives: the newest payload must be the one that survives.
	send([]byte("p2"))
	send([]byte("p3"))
	close(release)

	select {
	case next := <-got:
		if next != "p3" {
			t.Errorf("delivered after overflow = %q, want p3 (newest must survive, oldest dropped)", next)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fn never received the post-overflow payload")
	}
}

func TestSubscriberQueueSendAfterStopNeverBlocks(t *testing.T) {
	var hits atomic.Int32
	send, stop := SubscriberQueue(func([]byte) { hits.Add(1) }, 4)

	stop()
	stop() // idempotent

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			send([]byte("late")) // must not block or panic after stop
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SECURITY: [fanout] send blocked after stop")
	}
	// NOTE: send-after-stop can still ENQUEUE (its first select sees both
	// <-stopped and q<- ready and picks randomly), and a consumer that has
	// not yet observed the stop may deliver that late payload. The doc
	// promises a "silent no-op"; the implementation makes that
	// probabilistic. The outcome is timing-dependent, so it is FLAGGED for
	// the owner rather than asserted here; what is deterministic is that
	// the sender is never blocked and never panics.
	_ = hits.Load()
}

// reusing its buffer after send cannot mutate what the subscriber receives.
// ---------------------------------------------------------------------------

func TestSubscriberQueueCopiesPayloadOnSend(t *testing.T) {
	got := make(chan []byte, 1)
	send, stop := SubscriberQueue(func(p []byte) { got <- append([]byte(nil), p...) }, 4)
	defer stop()

	buf := []byte("attack")
	send(buf)
	buf[0] = 'X' // caller-side reuse after send

	select {
	case p := <-got:
		if string(p) != "attack" {
			t.Errorf("delivered %q, want the bytes as of send time (queue must copy)", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fn never received the payload")
	}
}
