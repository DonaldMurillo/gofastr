package fanout

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A Fanout.Publish that panics (a host backend bug, a closed client) runs
// on the PublishQueue drainer, a goroutine the framework owns: unrecovered,
// the panic kills the replica on the next mirrored message (round 7
// R7-009, a regression when the queue stopped running through
// SubscriberQueue's deliver). Proven by re-exec: the child must survive
// the panic, report it to the payload's failed callback, and keep
// publishing.

const publishPanicChildEnv = "GOFASTR_TEST_FANOUT_PUBLISH_PANIC_CHILD"

func TestPublishPanicDoesNotKillProcess(t *testing.T) {
	if os.Getenv(publishPanicChildEnv) == "1" {
		publishPanicChild()
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestPublishPanicDoesNotKillProcess$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), publishPanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a panicking Publish killed the process: %v\n--- child output ---\n%s", err, out)
	}
	if strings.Contains(string(out), "panic:") {
		t.Fatalf("child reported an unrecovered panic:\n%s", out)
	}
}

// panicFanout panics on its first Publish and succeeds afterwards.
type panicFanout struct {
	calls     atomic.Int32
	published chan struct{}
}

func (p *panicFanout) Publish(context.Context, string, []byte) error {
	if p.calls.Add(1) == 1 {
		panic("backend bug\x1b[31m")
	}
	p.published <- struct{}{}
	return nil
}

func (p *panicFanout) Subscribe(string, func([]byte)) (func(), error) {
	return func() {}, nil
}

func publishPanicChild() {
	f := &panicFanout{published: make(chan struct{}, 1)}
	send, stop := PublishQueueReporting(f, "t", 8)
	defer stop()
	failed := make(chan error, 1)
	send([]byte("a"), func(err error) { failed <- err })
	select {
	case err := <-failed:
		if err == nil || errors.Is(err, ErrPublishDropped) {
			os.Exit(20)
		}
	case <-time.After(5 * time.Second):
		os.Exit(21) // the panic was not reported
	}
	send([]byte("b"), nil)
	select {
	case <-f.published:
		os.Exit(0)
	case <-time.After(5 * time.Second):
		os.Exit(22) // the drainer died
	}
}
