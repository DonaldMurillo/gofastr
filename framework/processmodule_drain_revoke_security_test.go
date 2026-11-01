package framework

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A revoked grant used to stay usable for the whole drain: the supervisor
// gave the child up to DrainPerModule to finish, and the broker answered
// its reverse calls under the old grant until it exited. A drain caused by
// a disable, or by an upgrade whose new grant set drops one the child
// holds, now refuses host.* calls at once. An upgrade that takes nothing
// away keeps them open, so the child can finish queued work (issue #614).
func TestDrainRefusesRevokedReverseCalls(t *testing.T) {
	held := []access.Permission{"orders:read", "orders:write"}
	cases := []struct {
		name  string
		state ProcessState
		next  []access.Permission
		want  bool // reverse call served during the drain
	}{
		{"disable", StateDrainingDisable, nil, false},
		{"revoke", StateDrainingUpgrade, []access.Permission{"orders:read"}, false},
		{"upgrade", StateDrainingUpgrade, held, true},
		{"widen", StateDrainingUpgrade, []access.Permission{"orders:*"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reverseCallDuringDrain(t, held, tc.state, tc.next); got != tc.want {
				t.Errorf("SECURITY: reverse call served = %v, want %v", got, tc.want)
			}
		})
	}
}

// reverseCallDuringDrain runs one drain of a child that makes a host.*
// call from its module.drain handler, and reports whether it was served.
func reverseCallDuringDrain(t *testing.T, held []access.Permission, state ProcessState, next []access.Permission) bool {
	t.Helper()
	child := newStalledChild(t)
	child.exitOnEOF = true
	childCodec, err := moduleproto.NewCodec(child.stdinR, child.stdoutW, 0)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan bool, 1)
	childPeer := moduleproto.NewPeer(childCodec, moduleproto.RoleChild)
	_ = childPeer.Handle(moduleproto.MethodDrain, func(ctx context.Context, _ json.RawMessage) (any, error) {
		_, err := childPeer.Call(ctx, moduleproto.MethodHostEventEmit, nil)
		served <- err == nil
		return moduleproto.DrainResult{}, nil
	})
	childPeer.Start()
	t.Cleanup(func() { _ = childPeer.Close() })

	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sup.cfg.DrainPerModule = 2 * time.Second
	store := newTestStore(t)
	_ = store.Install(context.Background(), DesiredState{Module: "m", ArtifactSHA256: hex256("aa")})
	sup.store = store
	sup.lease = map[string]time.Time{}
	sup.now = time.Now
	sl := &moduleSlot{name: "m", sup: sup, child: child, state: StateReady}
	peer, closed := sl.newHostPeer(child.codec)
	_ = peer.Handle(moduleproto.MethodHostEventEmit, func(context.Context, json.RawMessage) (any, error) {
		return "ok", nil
	})
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sl.peer, sl.liveGrants, sl.reverseClosed = peer, held, closed

	sl.transitionDrain(state, 2, next)
	select {
	case ok := <-served:
		return ok
	case <-time.After(3 * time.Second):
		t.Fatal("the child's drain-time reverse call never returned")
		return false
	}
}

// reconcile is blocked inside an upgrade drain, so a revoke or disable
// stored while the drain ran reached the child only once it exited: up to
// DrainPerModule of reverse calls under the revoked grant. The drain now
// watches the stored row and closes the gate when one lands.
func TestDrainClosesOnMidDrainRevoke(t *testing.T) {
	held := []access.Permission{"orders:read", "orders:write"}
	cases := []struct {
		name  string
		apply func(context.Context, ProcessModuleStore) error
	}{
		{"revoke", func(ctx context.Context, st ProcessModuleStore) error {
			_, err := st.SetEffectiveGrants(ctx, "m", []access.Permission{"orders:read"})
			return err
		}},
		{"disable", func(ctx context.Context, st ProcessModuleStore) error { return st.SetEnabled(ctx, "m", false) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := midDrainReverseCalls(t, held, tc.apply)
			if !before {
				t.Fatal("the upgrade drain refused a reverse call before any revoke")
			}
			if after {
				t.Error("SECURITY: a reverse call was served after a mid-drain " + tc.name)
			}
		})
	}
}

// midDrainReverseCalls runs an upgrade drain that keeps every grant. The
// child calls host.* once, the test applies change to the stored row, and
// the child calls again after a few poll intervals. It reports whether each
// call was served.
func midDrainReverseCalls(t *testing.T, held []access.Permission, change func(context.Context, ProcessModuleStore) error) (bool, bool) {
	t.Helper()
	const poll = 20 * time.Millisecond
	child := newStalledChild(t)
	child.exitOnEOF = true
	childCodec, err := moduleproto.NewCodec(child.stdinR, child.stdoutW, 0)
	if err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Install(ctx, DesiredState{Module: "m", Enabled: true, ArtifactSHA256: hex256("aa"), EffectiveGrants: held}); err != nil {
		t.Fatal(err)
	}
	var before, after bool
	childPeer := moduleproto.NewPeer(childCodec, moduleproto.RoleChild)
	_ = childPeer.Handle(moduleproto.MethodDrain, func(ctx context.Context, _ json.RawMessage) (any, error) {
		_, err := childPeer.Call(ctx, moduleproto.MethodHostEventEmit, nil)
		before = err == nil
		if err := change(context.Background(), store); err != nil {
			t.Errorf("change the stored row: %v", err)
		}
		time.Sleep(10 * poll)
		_, err = childPeer.Call(ctx, moduleproto.MethodHostEventEmit, nil)
		after = err == nil
		return moduleproto.DrainResult{}, nil
	})
	childPeer.Start()
	t.Cleanup(func() { _ = childPeer.Close() })

	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sup.cfg.DrainPerModule = 3 * time.Second
	sup.cfg.PollInterval = poll
	sup.store = store
	sup.lease = map[string]time.Time{}
	sup.now = time.Now
	sl := &moduleSlot{name: "m", sup: sup, child: child, state: StateReady}
	peer, closed := sl.newHostPeer(child.codec)
	_ = peer.Handle(moduleproto.MethodHostEventEmit, func(context.Context, json.RawMessage) (any, error) {
		return "ok", nil
	})
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sl.peer, sl.liveGrants, sl.reverseClosed = peer, held, closed

	sl.transitionDrain(StateDrainingUpgrade, 2, held)
	return before, after
}
