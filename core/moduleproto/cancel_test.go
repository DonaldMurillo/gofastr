package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// module.cancel contract (issue #356). The two halves of this package used
// to disagree about what CancelParams.RequestID is: methods.go called it
// "what module.cancel references" (the proxy's "<module>-<counter>"
// correlation id) while the child keyed its cancel registry by inbound
// frame id and parsed RequestID with fmt.Sscanf("%d"). Together that made
// module.cancel a silent no-op on the proxy path — the production shape
// "notes-42" failed the decimal scan outright, so a client that aborted a
// slow proxied request left the child burning CPU/DB until its own
// deadline — and turned "1-anything" into a misroute that cancelled an
// UNRELATED frame. The fix (audit record, 2026-09): the supervisor sends
// the frame id [Peer.CallWithID] assigned, and the child parse is a strict
// full-string decimal. The tests here pin both halves.

// TestModuleCancelCancelsByFrameID pins the production shape end to end:
// the originator cancels by the frame id CallWithID reported, and the
// serving handler's context is aborted. This replaces the audit proof
// TestModuleCancelAcceptsProductionRequestID, which pinned the same
// workflow against the OLD mint ("<module>-<counter>") and was red from
// the day it was written.
func TestModuleCancelCancelsByFrameID(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	cancelled := make(chan struct{}, 1)
	if err := child.Handle("hang.until.cancelled", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			cancelled <- struct{}{}
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return "timeout", nil
		}
	}); err != nil {
		t.Fatal(err)
	}

	ids := make(chan uint64, 1)
	go func() {
		callCtx, c := context.WithTimeout(context.Background(), 4*time.Second)
		defer c()
		_, _ = host.CallWithID(callCtx, "hang.until.cancelled", nil, func(id uint64) {
			ids <- id
		})
	}()

	frameID := <-ids
	if frameID == 0 {
		t.Fatal("CallWithID reported id 0; frame ids start at 1")
	}

	// Let the hung request reach its handler, then cancel by the reported
	// frame id — exactly what watchCancel sends post-fix.
	time.Sleep(100 * time.Millisecond)
	notifyCtx, nCancel := context.WithTimeout(context.Background(), time.Second)
	defer nCancel()
	if err := host.Notify(notifyCtx, MethodCancel, CancelParams{
		RequestID: strconv.FormatUint(frameID, 10),
	}); err != nil {
		t.Fatalf("Notify cancel: %v", err)
	}

	select {
	case <-cancelled:
		// The reported id was the wire id: the serving handler aborted.
	case <-time.After(600 * time.Millisecond):
		t.Fatalf("module.cancel for the CallWithID-reported frame id %d did not cancel the in-flight request", frameID)
	}
}

// TestCallWithIDReportsAssignedFrameID pins the id-reporting contract:
// ids are reported once per call, monotonically from 1, and are the ids
// the counterparty sees on the wire (cancelling by a reported id works —
// pinned above). Also: no id is reported when none was allocated (the
// inflight cap rejects before allocation).
func TestCallWithIDReportsAssignedFrameID(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle("echo", func(_ context.Context, _ json.RawMessage) (any, error) {
		return "pong", nil
	}); err != nil {
		t.Fatal(err)
	}

	var first, second uint64
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := host.CallWithID(ctx, "echo", nil, func(id uint64) { first = id }); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := host.CallWithID(ctx, "echo", nil, func(id uint64) { second = id }); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("reported ids = %d, %d; want 1, 2 (monotonic from 1)", first, second)
	}

	// Cap a dedicated pair at one in-flight call, occupy it, and confirm
	// the rejected call never reports an id.
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	codecY, _ := NewCodec(connY, connY, 0)
	cappedHost := NewPeer(codecX, RoleHost, WithMaxInflight(1))
	cappedChild := NewPeer(codecY, RoleChild)
	defer func() {
		_ = cappedHost.Close()
		_ = cappedChild.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()
	release := make(chan struct{})
	defer close(release)
	if err := cappedChild.Handle("block", func(context.Context, json.RawMessage) (any, error) {
		<-release
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	cappedHost.Start()
	cappedChild.Start()
	go func() {
		cctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = cappedHost.Call(cctx, "block", nil) // occupies the single slot
	}()
	time.Sleep(100 * time.Millisecond)

	reported := make(chan uint64, 1)
	rejCtx, rejCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer rejCancel()
	if _, err := cappedHost.CallWithID(rejCtx, "block", nil, func(id uint64) { reported <- id }); !errors.Is(err, ErrInflightCap) {
		t.Fatalf("capped call: err = %v, want ErrInflightCap", err)
	}
	select {
	case id := <-reported:
		t.Fatalf("onID fired with id %d for a call rejected at the inflight cap (no id was allocated)", id)
	default:
	}
}

// TestModuleCancelNonCanonicalRequestIDIsNoOp pins the child side: a
// RequestID that is not the canonical decimal string of an in-flight
// inbound frame id must cancel NOTHING. Prefix-matching ("1-…", " 1") is
// the misroute hazard that made one request's cancel abort an unrelated
// request; the name-prefixed production shape ("notes-1") is the old no-op
// — post-fix both are simply ids that name no frame. This replaces the
// audit proof TestModuleCancelRequestIDIgnoresTrailingBytes, which pinned
// the pre-fix prefix-match behaviour and instructed the fix to flip it.
func TestModuleCancelNonCanonicalRequestIDIsNoOp(t *testing.T) {
	// "01" is the one that ParseUint alone lets through: it parses to
	// 1 with no error, so only the canonical round-trip check refuses
	// it. "+1" and " 1" the parser rejects on its own.
	for _, rid := range []string{"notes-1", "1-not-the-frame-id", " 1", "01", "+1"} {
		t.Run(rid, func(t *testing.T) {
			host, child, _, _, cleanup := newPeerPair(t, 0)
			defer cleanup()

			cancelled := make(chan struct{}, 1)
			if err := child.Handle("hang.until.cancelled", func(ctx context.Context, _ json.RawMessage) (any, error) {
				select {
				case <-ctx.Done():
					cancelled <- struct{}{}
					return nil, ctx.Err()
				case <-time.After(1500 * time.Millisecond):
					return "survived", nil
				}
			}); err != nil {
				t.Fatal(err)
			}

			go func() {
				callCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
				defer c()
				_, _ = host.Call(callCtx, "hang.until.cancelled", nil) // frame id 1
			}()
			// Let the hung request reach its handler.
			time.Sleep(100 * time.Millisecond)

			notifyCtx, nCancel := context.WithTimeout(context.Background(), time.Second)
			defer nCancel()
			if err := host.Notify(notifyCtx, MethodCancel, CancelParams{RequestID: rid}); err != nil {
				t.Fatalf("Notify cancel: %v", err)
			}

			select {
			case <-cancelled:
				t.Fatalf("RequestID %q cancelled frame id 1: a non-canonical id must name no frame, not prefix-match an unrelated one", rid)
			case <-time.After(700 * time.Millisecond):
				// No cancellation: the id named no frame.
			}
		})
	}
}
