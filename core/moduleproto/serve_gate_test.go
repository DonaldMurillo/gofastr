package moduleproto

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// A serve gate refuses a request with its own error and the handler never
// runs; a request the gate passes is served. The supervisor uses it to shut
// a draining module's reverse calls once its grant is revoked.
func TestServeGateRefusesRequest(t *testing.T) {
	connA, connB := net.Pipe()
	t.Cleanup(func() { _ = connA.Close(); _ = connB.Close() })
	codecA, _ := NewCodec(connA, connA, 0)
	codecB, _ := NewCodec(connB, connB, 0)
	var closed atomic.Bool
	var ran atomic.Int32
	host := NewPeer(codecA, RoleHost, WithServeGate(func(method string) *Error {
		if closed.Load() && method == "host.x" {
			return &Error{Code: CodeCapabilityDenied, Message: "draining"}
		}
		return nil
	}))
	ok := func(context.Context, json.RawMessage) (any, error) { ran.Add(1); return "ok", nil }
	_ = host.Handle("host.x", ok)
	_ = host.Handle("host.y", ok)
	child := NewPeer(codecB, RoleChild)
	host.Start()
	child.Start()
	t.Cleanup(func() { _ = host.Close(); _ = child.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := child.Call(ctx, "host.x", nil); err != nil {
		t.Fatalf("open gate: %v", err)
	}
	closed.Store(true)
	_, err := child.Call(ctx, "host.x", nil)
	if e := AsError(err); e == nil || e.Code != CodeCapabilityDenied {
		t.Fatalf("closed gate: err = %v, want CodeCapabilityDenied", err)
	}
	if _, err := child.Call(ctx, "host.y", nil); err != nil {
		t.Fatalf("ungated method: %v", err)
	}
	if got := ran.Load(); got != 2 {
		t.Errorf("handler ran %d times, want 2: a refused request reached it", got)
	}
	// A notification is a call with no answer; the gate drops it too.
	if err := child.Notify(ctx, "host.x", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Call(ctx, "host.y", nil); err != nil {
		t.Fatal(err)
	}
	if got := ran.Load(); got != 3 {
		t.Errorf("handler ran %d times, want 3: a refused notification reached it", got)
	}
}

// A gate that panics refuses the call instead of killing the process.
func TestServeGatePanicRefuses(t *testing.T) {
	connA, connB := net.Pipe()
	t.Cleanup(func() { _ = connA.Close(); _ = connB.Close() })
	codecA, _ := NewCodec(connA, connA, 0)
	codecB, _ := NewCodec(connB, connB, 0)
	host := NewPeer(codecA, RoleHost, WithServeGate(func(string) *Error { panic("boom") }))
	_ = host.Handle("host.x", func(context.Context, json.RawMessage) (any, error) { return "ok", nil })
	child := NewPeer(codecB, RoleChild)
	host.Start()
	child.Start()
	t.Cleanup(func() { _ = host.Close(); _ = child.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := child.Call(ctx, "host.x", nil); AsError(err) == nil {
		t.Fatalf("panicking gate: err = %v, want a refusal", err)
	}
}
