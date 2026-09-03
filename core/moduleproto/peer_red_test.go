//go:build red

package moduleproto

// RED TEST — open finding, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
// Property: a Handler is third-party code at an extension point; a panic in
// one must not escape the Peer. Panic isolation is the repo's stated standard
// for every extension point (framework/hook/hook.go runHookSafely,
// core/fanout/subscriber_queue.go deliver): recover and convert, never
// propagate. The Handler doc covers returned errors only; this pins parity
// for panics.
// Surfaces: peer.go buildResponse :633 h(ctx, f.Params), invoked on the bare
// per-request serve goroutine dispatch spawns at :534 — no recover anywhere
// between the handler and that goroutine.
// Finding: a panicking handler unwinds through buildResponse onto the serve
// goroutine, and an unrecovered panic on ANY goroutine terminates the whole
// process. The originating Call also never receives the paired response
// buildResponse's own contract promises ("never nil for a request; a request
// always gets a paired response so the originating Call unblocks").
// Fix direction: deferred recover around the h() call in buildResponse that
// converts the panic into NewErrorResponse(id, CodeInternalError,
// "handler panic: ..."), mirroring runHookSafely.
// Severity: production-facing — the host peer serves reverse host.*
// capability calls, so one panicking handler kills the entire host process
// (and every unrelated in-flight request with it), not just the one call.

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

// TestPeerRedHandlerPanicContained calls buildResponse directly under
// defer/recover so the failure lands on a clean SECURITY assertion instead of
// the binary-killing crash it would produce on the real serve goroutine: if
// the panic escapes this function, it escapes to that goroutine too.
func TestPeerRedHandlerPanicContained(t *testing.T) {
	connA, connB := net.Pipe()
	defer func() { _ = connA.Close(); _ = connB.Close() }()
	served := NewPeer(NewCodecOnly(connA), RoleHost)
	defer func() { _ = served.Close() }()

	if err := served.Handle("m", func(ctx context.Context, _ json.RawMessage) (any, error) {
		panic("handler boom")
	}); err != nil {
		t.Fatal(err)
	}

	req := NewRequest(7, "m", nil)
	var resp *Frame
	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		resp = served.buildResponse(req)
		return false
	}()
	if panicked {
		t.Fatal("SECURITY: [moduleproto] handler panic escaped buildResponse; on the " +
			"serve goroutine (peer.go :534) it terminates the whole process and the " +
			"originating Call hangs with no paired response — convert it like " +
			"hook.runHookSafely")
	}
	if resp == nil {
		t.Fatal("buildResponse returned nil for a request whose handler panicked; " +
			"contract: a request always gets a paired response")
	}
	if resp.IDValue() != 7 {
		t.Fatalf("panic response not paired to id 7: %+v", resp)
	}
	if resp.Error == nil {
		t.Fatalf("panic must surface as a paired error response, got success: %+v", resp)
	}
	if resp.Error.Code != CodeInternalError {
		t.Fatalf("want CodeInternalError on a contained panic, got %d", resp.Error.Code)
	}
}

// RED TEST — open finding, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
// Property: the same containment standard on the notification branch — a
// panicking notification Handler must not escape the goroutine dispatch
// spawns at peer.go :587. There is no response channel to unblock, but the
// goroutine (and the process) must survive and keep serving later frames.
// Surfaces: peer.go serveNotification :668 _, _ = h(ctx, f.Params) on the
// notification goroutine :587.
// Finding: identical shape to the request branch, no recover; the panic kills
// the process, so every request the peer would have served after the
// notification dies with it.
// Fix direction: deferred recover inside serveNotification (or its dispatch
// goroutine) that logs-and-swallows, mirroring runHookSafely.
// Severity: production-facing for the same reason as the request branch.
func TestPeerRedNotificationPanicContained(t *testing.T) {
	connA, connB := net.Pipe()
	defer func() { _ = connA.Close(); _ = connB.Close() }()
	served := NewPeer(NewCodecOnly(connA), RoleHost)
	defer func() { _ = served.Close() }()

	if err := served.Handle("n", func(ctx context.Context, _ json.RawMessage) (any, error) {
		panic("notification boom")
	}); err != nil {
		t.Fatal(err)
	}

	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		served.serveNotification(NewNotification("n", nil))
		return false
	}()
	if panicked {
		t.Fatal("SECURITY: [moduleproto] handler panic escaped serveNotification; on " +
			"the notification goroutine (peer.go :587) it terminates the whole " +
			"process — recover it like hook.runHookSafely so the peer keeps serving")
	}

	// The worker must keep serving after one bad notification: a follow-up
	// inbound request still gets its paired success response.
	if err := served.Handle("ok", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return "still-alive", nil
	}); err != nil {
		t.Fatal(err)
	}
	if resp := served.buildResponse(NewRequest(3, "ok", nil)); !resp.IsSuccess() {
		t.Fatalf("peer did not keep serving after a contained notification "+
			"panic: %+v", resp)
	}
}
