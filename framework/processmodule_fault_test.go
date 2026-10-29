package framework

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// A child whose frames fault the host peer kept running in Ready while
// every call to it failed: nothing in the supervisor watched FatalDone,
// though the peer's docs said the supervisor tore the transport down.
// The fault now kills the child, so the exit watcher records a crash and
// the supervise loop starts a new instance.
func TestSupervisorRestartsFaultedModule(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := newTestStore(t)
	sup := newTestSupervisor(t, store, childModeFault)
	d := descriptorForChild(t, childModeFault)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 5*time.Second)

	sl := sup.Slot(d.Name)
	sl.mu.RLock()
	first, peer := sl.instanceID, sl.peer
	sl.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = peer.Call(ctx, moduleproto.MethodHTTP, nil)
	select {
	case <-peer.FatalDone():
	case <-time.After(2 * time.Second):
		t.Fatal("the child's unsolicited response did not fault the host peer")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sl.mu.RLock()
		id, state := sl.instanceID, sl.state
		sl.mu.RUnlock()
		if id != first && state == StateReady {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("a faulted peer (%v) left instance %s running; no restart", peer.FatalError(), first)
}
