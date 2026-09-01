package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// TestLateResponseAfterCallTimeoutIsNotFatal pins the availability contract
// for origination-side cancellation:
//
//	Peer.Call returns on ctx expiry WITHOUT closing the peer, and the
//	counterparty's response for that id — which the design GUARANTEES will
//	arrive ("a request always gets a paired response so the originating
//	Call unblocks", buildResponse) — must not be treated as a terminal
//	protocol fault.
//
// Reachability (framework/processmodule_proxy.go): every proxied HTTP call
// gets a per-call deadline context; when it expires, Call returns
// DeadlineExceeded and watchCancel sends module.cancel, which makes the
// child abort its handler and answer with an error response for that exact
// id. deliverResponse then finds no pending entry (Call already deleted it),
// and — because the peer is not closed — raises "unsolicited response" as a
// FATAL fault, which the supervisor answers by tearing down the child
// process and every other in-flight request to that module.
//
// A late response to a Call that gave up is an expected event on a healthy
// peer, not a desynchronization. The fatal should apply to responses that
// never matched ANY originated id in a way we cannot attribute — at minimum,
// a gave-up-but-was-originated id must be distinguished from a never-existed
// id.
func TestLateResponseAfterCallTimeoutIsNotFatal(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// A handler that answers AFTER the host's call deadline: the module.http
	// shape under module.cancel (handler observes ctx done / work simply
	// outlasts the caller's budget, then responds).
	if err := child.Handle("slow.echo", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-time.After(300 * time.Millisecond):
			return "late", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}); err != nil {
		t.Fatal(err)
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := host.Call(callCtx, "slow.echo", nil); err == nil {
		t.Fatal("expected the Call to time out")
	}

	// The child's paired response lands after Call already gave up. Give it
	// ample time to be delivered, then check the host peer is still healthy.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-host.FatalDone():
			t.Fatalf(
				"SECURITY: [moduleproto] late response to a Call that returned on ctx expiry was treated as a terminal fault: %v — one expired per-call deadline kills the whole module peer and every concurrent in-flight request (see processmodule_proxy.go callCtx + watchCancel)",
				host.FatalError(),
			)
		case <-host.Done():
			t.Fatalf("SECURITY: [moduleproto] read loop exited after a late response: %v", host.FatalError())
		case <-time.After(50 * time.Millisecond):
			// still healthy at this tick; keep waiting out the window
		}
	}

	// The peer must still work: a fresh Call must succeed.
	freshCtx, freshCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer freshCancel()
	if _, err := host.Call(freshCtx, "slow.echo", nil); err != nil {
		t.Fatalf("peer unusable after a late response: %v", err)
	}
}

// TestUnsolicitedResponseStillFatalAfterAbandonment pins the OTHER
// direction of the late-response contract: suppression is scoped to ids
// this peer actually originated and gave up on. A response naming an id
// that was NEVER originated stays a terminal fault even while abandonment
// tombstones exist — the tombstone set must not become a blanket pardon
// for unsolicited responses. (The no-abandonment case is pinned by
// TestPeerUnsolicitedRespIsFatal in peer_test.go.)
func TestUnsolicitedResponseStillFatalAfterAbandonment(t *testing.T) {
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	h := NewPeer(codecX, RoleHost)
	h.Start()
	defer func() {
		_ = h.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()
	// Drain the host's outbound writes (net.Pipe is synchronous; nothing
	// else is reading connY).
	go func() { _, _ = io.Copy(io.Discard, connY) }()

	// Abandon id 1: originate a call whose ctx is already expired. The
	// frame is written (and drained above), Call returns ctx.Err, and the
	// id is tombstoned as abandoned.
	expiredCtx, expCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expCancel()
	if _, err := h.Call(expiredCtx, "anything", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired-ctx call: err = %v, want DeadlineExceeded", err)
	}

	// A response for an id that was never originated must still fault.
	unsolicited := NewSuccessResponse(999, json.RawMessage(`{}`))
	raw, _ := json.Marshal(unsolicited)
	raw = append(raw, '\n')
	go func() { _, _ = connY.Write(raw) }()

	select {
	case <-h.FatalDone():
		if err := h.FatalError(); err == nil {
			t.Fatal("FatalDone closed but FatalError is nil")
		}
	case <-time.After(time.Second):
		t.Fatal("unsolicited response for a never-originated id must stay fatal even while an abandonment tombstone exists")
	}
}
