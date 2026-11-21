package framework

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/internal/pmtestkit"
)

// modeRunner re-execs the test binary in whichever child mode it holds
// at spawn time, so a test can fix the module between spawns.
type modeRunner struct {
	inner Runner
	mode  atomic.Value // pmtestkit.Mode
}

func (r *modeRunner) Start(ctx context.Context, spec ChildSpec) (RunningChild, error) {
	spec.ExtraEnv = append(append([]string{}, spec.ExtraEnv...), pmtestkit.Env(r.mode.Load().(pmtestkit.Mode), nil)...)
	return r.inner.Start(ctx, spec)
}

// TestGenerationBumpRearmsFailed: Failed is terminal until the operator
// bumps the generation, the documented recovery lever. The bump reset
// the circuit and then left the slot Failed (2026-10-27 bug bash P34).
func TestGenerationBumpRearmsFailed(t *testing.T) {
	if os.Getenv(pmtestkit.EnvName) != "" {
		return
	}
	ctx := context.Background()
	store := newTestStore(t)
	d := descriptorForChild(t, pmtestkit.ModeEcho)
	runner := &modeRunner{inner: &TrustedProcessRunner{}}
	runner.mode.Store(pmtestkit.ModeBadDigest)
	sup := newBareTestSupervisor(t, store, runner)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(ctx, d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForStateTB(t, sup, d.Name, StateFailed, 8*time.Second)

	// Without a bump the slot stays Failed across polls.
	runner.mode.Store(pmtestkit.ModeEcho)
	time.Sleep(300 * time.Millisecond)
	if info, _ := sup.Info(d.Name); info.State != StateFailed {
		t.Fatalf("a Failed slot moved to %s with no generation bump", info.State)
	}
	if _, err := store.BumpGeneration(ctx, d.Name); err != nil {
		t.Fatalf("bump: %v", err)
	}
	waitForStateTB(t, sup, d.Name, StateReady, 8*time.Second)
}
