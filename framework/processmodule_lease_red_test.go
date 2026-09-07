//go:build red

package framework

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a store unreachable past LeaseTTL must drain AND RECOVER when the store
// returns — the fail-closed drain documented at [SQLProcessModuleStore],
// [SupervisorConfig].LeaseTTL, and handleLeaseExpired ("proxy will see not-Ready via
// leaseFailing") is an availability guard, not a one-way kill.
// Surfaces: processmodule_supervisor.go::handleLeaseExpired (:945 sets
// leaseFailing=true, :972 keeps state=Ready with child=nil); processmodule_proxy.go:175
// leaseFailingUnsafe() is a hardcoded `return false` stub whose promised replacement
// never landed; moduleSlot.snapshot() never copies leaseFailing; nothing ever writes
// leaseFailing=false; reconcile (:805-876) has no Ready-with-nil-child recovery case
// (state==Ready + desired.Enabled hits "Healthy; nothing to do" and never respawns).
// Finding: any GetDesired outage > LeaseTTL (default 5s — pool exhaustion, sqlite lock
// contention, failover) permanently 503s every proxied module route and every module
// tool call even after the DB returns. leaseFailing has exactly one write site (`=
// true`) so Info().LeaseFailing can never clear, and the drained slot keeps state=Ready
// so reconcile never re-arms the spawn path — a one-way availability kill from ordinary
// load.
// Fix direction: on the first successful refreshDesired after a lease-expired drain,
// clear leaseFailing and route the (childless) Ready slot through the spawn path (or
// transition it to a state reconcile acts on); make snapshot() copy leaseFailing and
// replace the leaseFailingUnsafe() stub so proxy/tools gates read the real flag.
type redLeaseFlakyStore struct {
	*SQLProcessModuleStore
	broken atomic.Bool // set: GetDesired fails with a DB-shaped error
}

// GetDesired delegates to the real SQLite store unless broken is set. The
// SQLProcessModuleStore's db handle is unexported, so the ProcessModuleStore
// interface is the sanctioned seam for simulating a GetDesired outage that
// leaves every other method (heartbeats, Enable, ...) healthy.
func (s *redLeaseFlakyStore) GetDesired(ctx context.Context, module string) (DesiredState, error) {
	if s.broken.Load() {
		return DesiredState{}, errors.New("sql: database is closed (simulated GetDesired outage)")
	}
	return s.SQLProcessModuleStore.GetDesired(ctx, module)
}

// redLeaseSlotPID reads the slot's live child pid (in-package accessor; -1 when
// no child is running).
func redLeaseSlotPID(sup *ProcessModuleSupervisor, name string) int {
	sl := sup.Slot(name)
	if sl == nil {
		return -1
	}
	sl.mu.RLock()
	child := sl.child
	sl.mu.RUnlock()
	if child == nil {
		return -1
	}
	return child.Pid()
}

// TestLeaseRedRecoversAfterOutage pins drain-then-RECOVER: with GetDesired
// failing past LeaseTTL the module drains (503, pinned by
// TestSupervisor_StoreUnreachableDrains); once the store returns, the slot must
// clear LeaseFailing, respawn a NEW child (new instance/pid), and serve 200
// again within a bounded window (a few poll intervals + one spawn deadline).
func TestLeaseRedRecoversAfterOutage(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	store := &redLeaseFlakyStore{SQLProcessModuleStore: newTestStore(t)}
	sup := newTestSupervisor(t, store, childModeEcho)
	d := descriptorForChild(t, childModeEcho)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("setup broken: register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(context.Background(), d.Name); err != nil {
		t.Fatalf("setup broken: enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 5*time.Second)

	// Baseline child identity (pid via the in-package slot, instance via Info).
	origInfo, err := sup.Info(d.Name)
	if err != nil {
		t.Fatalf("setup broken: info: %v", err)
	}
	origPID := redLeaseSlotPID(sup, d.Name)
	if origPID <= 0 {
		t.Fatalf("setup broken: no live child pid before outage (pid=%d)", origPID)
	}

	// Break GetDesired. LeaseTTL is 500ms in the test supervisor config.
	store.broken.Store(true)
	drainDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(drainDeadline) {
		if info, _ := sup.Info(d.Name); info.LeaseFailing {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info, _ := sup.Info(d.Name); !info.LeaseFailing {
		t.Fatalf("setup broken: lease should be failing after outage past LeaseTTL")
	}
	// Pinned today (setup confirmation, mirrors TestSupervisor_StoreUnreachableDrains):
	// the drained module 503s while the store is unreachable.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	sup.serveProxy(d.Name, "echo", rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("setup broken: during outage proxy status = %d, want 503", rec.Code)
	}

	// Store returns. Recovery must follow within a bounded window: 3x poll
	// interval (150ms) + LeaseTTL (500ms) + one spawn incl. handshake
	// (SpawnDeadline 3s). 8s leaves generous margin; cap vs the test deadline.
	storeReturned := time.Now()
	store.broken.Store(false)
	recoverDeadline := time.Now().Add(8 * time.Second)
	if dl, ok := t.Deadline(); ok {
		if maxWait := time.Until(dl) - 4*time.Second; maxWait < 8*time.Second {
			recoverDeadline = time.Now().Add(maxWait)
		}
	}
	var (
		lastInfo ProcessModuleInfo
		lastPID  = -1
		ok       bool
	)
	for time.Now().Before(recoverDeadline) {
		info, err := sup.Info(d.Name)
		if err != nil {
			t.Fatalf("setup broken: info after store return: %v", err)
		}
		lastInfo = info
		lastPID = redLeaseSlotPID(sup, d.Name)
		if !info.LeaseFailing && info.State == StateReady &&
			info.InstanceID != origInfo.InstanceID &&
			lastPID > 0 && lastPID != origPID {
			ok = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Errorf("SECURITY: [processmodule-lease-recovery] store returned but module never recovered: "+
			"LeaseFailing=%t State=%s instance=%q (orig %q) childPID=%d (orig %d) after %s — a GetDesired "+
			"outage past LeaseTTL is a one-way kill: leaseFailing has no clear path so Info() reports the "+
			"module failed forever, and when the drained slot keeps state=Ready reconcile treats it as "+
			"healthy and never re-arms the spawn path",
			lastInfo.LeaseFailing, lastInfo.State, lastInfo.InstanceID, origInfo.InstanceID,
			lastPID, origPID, time.Since(storeReturned))
		return
	}
	// Recovery observed: the route must actually serve again.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/echo", nil)
	sup.serveProxy(d.Name, "echo", rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("SECURITY: [processmodule-lease-recovery] after recovery serveProxy status = %d, want 200 (body %q)",
			rec2.Code, rec2.Body.String())
		return
	}
	if !strings.Contains(rec2.Body.String(), `"ok":true`) {
		t.Errorf("SECURITY: [processmodule-lease-recovery] after recovery serveProxy body = %q, want echo child JSON",
			rec2.Body.String())
	}
}
