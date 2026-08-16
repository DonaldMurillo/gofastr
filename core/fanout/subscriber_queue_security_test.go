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
// process — the same class framework/hook guards with runHookSafely.
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
// observes a crash exit — the failing assertion.

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
