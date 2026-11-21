package framework

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

type remoteToggleCoordinator struct{ reconciles *atomic.Int32 }

func (c remoteToggleCoordinator) Reconcile(string) { c.reconciles.Add(1) }
func (remoteToggleCoordinator) LocalInfo(string) (ProcessModuleInfo, bool) {
	return ProcessModuleInfo{}, false
}

func TestModuleCoordinatorReplacementDuringRemoteToggle(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("example", ModuleManifest{})
	var reconciles atomic.Int32
	pc := remoteToggleCoordinator{reconciles: &reconciles}
	mm.SetProcessCoordinator(pc)
	payload := fanout.Wrap("remote-node", []byte(`{"name":"example","enabled":false}`))
	start := make(chan struct{})
	var calls sync.WaitGroup
	calls.Go(func() {
		<-start
		for range 500 {
			mm.handleRemoteToggle(payload)
		}
	})
	calls.Go(func() {
		<-start
		for range 500 {
			mm.SetProcessCoordinator(pc)
		}
	})
	close(start)
	calls.Wait()
	if got := reconciles.Load(); got != 500 {
		t.Fatalf("reconciles = %d, want 500", got)
	}
}
