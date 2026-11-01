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
