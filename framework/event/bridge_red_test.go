//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass round 2 (tests-only; no fix applied).
// Property: panic isolation at extension points — AttachFanout hands the
// publisher goroutine (framework-owned) to host-supplied backend code
// (fanout.Fanout.Publish); a panic there is an unrecovered goroutine panic
// and terminates the whole process, the same class core/fanout guards for
// subscriber callbacks (subscriber_queue_security_test.go) and framework/hook
// guards with runHookSafely.
// Surfaces: bridge.go publisher goroutine `f.Publish(ctx, fanoutTopic, data)`
// — reached from every Emit / EmitStrict / EmitAsync on a bridged bus.
// Finding: no recover in the publisher goroutine, so a backend bug (nil map
// write, bad cast on a payload) in ANY fanout implementation kills every
// replica running a bridged event bus, taking down request serving with it.
// Fix direction: recover around the Publish call (or the goroutine loop),
// log the panic like the marshal-failure path does, and keep draining the
// queue — the lane is already documented lossy best-effort.
// Severity: production-facing.
package event_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

const redBridgePanicChildEnv = "GOFASTR_TEST_EVENT_BRIDGE_PANIC_CHILD"

func TestEventBridgeRedPublishPanicContained(t *testing.T) {
	if os.Getenv(redBridgePanicChildEnv) == "1" {
		redBridgePublishPanicChild()
		return // unreachable; the child exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestEventBridgeRedPublishPanicContained$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), redBridgePanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [bridge-publish-panic] a panicking fanout backend killed the whole process from the bridge's publisher goroutine: %v\n--- child output ---\n%s", err, out)
	}
	if strings.Contains(string(out), "panic:") {
		t.Fatalf("SECURITY: [bridge-publish-panic] child reported a panic:\n%s", out)
	}
}

// redPanicFanout panics on its first Publish (a backend bug) and succeeds
// afterwards, so survival is observable: the second emit must still be
// published by a living publisher goroutine.
type redPanicFanout struct {
	calls atomic.Int32
}

func (f *redPanicFanout) Publish(_ context.Context, _ string, _ []byte) error {
	if f.calls.Add(1) == 1 {
		panic("fanout backend bug on first publish")
	}
	return nil
}

func (f *redPanicFanout) Subscribe(_ string, _ func([]byte)) (func(), error) {
	return func() {}, nil
}

// redBridgePublishPanicChild is the child-side scenario. It never returns.
func redBridgePublishPanicChild() {
	eb := event.NewEventBus()
	f := &redPanicFanout{}
	stop, err := event.AttachFanout(eb, f)
	if err != nil {
		os.Exit(10)
	}
	defer stop()

	// First emit: the tap enqueues synchronously; the publisher goroutine
	// calls f.Publish, which panics. Without recover() this terminates the
	// process (exit status 2).
	if err := eb.Emit(context.Background(), event.Event{Type: "red.ping", Data: map[string]any{"n": 1}}); err != nil {
		os.Exit(11)
	}
	// Second emit: proves the publisher goroutine (and the process) survived
	// the first panic and kept draining the bridge queue.
	if err := eb.Emit(context.Background(), event.Event{Type: "red.ping", Data: map[string]any{"n": 2}}); err != nil {
		os.Exit(12)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if f.calls.Load() < 2 {
		os.Exit(13) // publisher goroutine died or stalled: second publish never happened
	}
	os.Exit(0)
}
