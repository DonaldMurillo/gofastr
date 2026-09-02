package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Property: a flooding or malformed counterparty cannot spawn unbounded
// work, and cannot put a served request beyond the reach of cancellation.
//
// peer_test.go's TestServeSideInflightCap states the first half for the
// REQUEST branch. The surfaces below are the same property at the places
// that branch does not cover.

// TestNotifyFloodBoundedByServeCap pins the serve cap on the NOTIFICATION
// branch. dispatch used to `go p.serveNotification(f)` with no cap at all,
// and it spawned BEFORE the handler lookup, so even a stream of frames
// naming methods nobody registered cost one goroutine each, exactly the
// threat WithMaxServeInflight's own doc describes.
func TestNotifyFloodBoundedByServeCap(t *testing.T) {
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	codecY, _ := NewCodec(connY, connY, 0)
	served := NewPeer(codecX, RoleHost, WithMaxServeInflight(4))
	flooder := NewPeer(codecY, RoleChild)
	defer func() {
		_ = served.Close()
		_ = flooder.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()

	release := make(chan struct{})
	var running atomic.Int64
	if err := served.Handle("block", func(ctx context.Context, _ json.RawMessage) (any, error) {
		running.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	served.Start()
	flooder.Start()

	base := runtime.NumGoroutine()
	// Four notifications may occupy the four serve slots.
	for range 4 {
		if err := flooder.Notify(context.Background(), "block", nil); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return running.Load() == 4 })

	// Everything past the cap, registered method or not, must be dropped,
	// not queued onto a fresh goroutine each.
	for range 400 {
		if err := flooder.Notify(context.Background(), "block", nil); err != nil {
			t.Fatal(err)
		}
		if err := flooder.Notify(context.Background(), "no-such-method", nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(150 * time.Millisecond)
	if grew := runtime.NumGoroutine() - base; grew > 64 {
		t.Errorf("800 over-cap notifications grew goroutines by %d; serve side is unbounded", grew)
	}
	close(release)
}

// TestDuplicateInboundIDsStayCancelable pins that two concurrently served
// requests sharing one id are BOTH reachable by Close. The cancel registry
// was keyed by id alone, so the second registration clobbered the first and
// first handler's deferred delete then removed the second, leaving a
// live handler that neither module.cancel nor Peer.Close could reach.
func TestDuplicateInboundIDsStayCancelable(t *testing.T) {
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	served := NewPeer(codecX, RoleHost)
	defer func() { _ = connX.Close(); _ = connY.Close() }()

	entered := make(chan struct{}, 2)
	cancelled := make(chan struct{}, 2)
	if err := served.Handle("hang", func(ctx context.Context, _ json.RawMessage) (any, error) {
		entered <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	served.Start()

	// Two requests, same id: a counterparty is free to reuse ids.
	raw := NewCodecOnly(connY)
	id := uint64(1)
	for range 2 {
		idCopy := id
		if err := raw.WriteFrame(&Frame{JSONRPC: "2.0", ID: &idCopy, Method: "hang"}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not start")
		}
	}

	_ = served.Close()
	for i := range 2 {
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 handlers were cancelled by Close", i)
		}
	}
}

// TestSecondStartPanics pins the documented contract on Peer.Start ("It
// panics if called twice") that the implementation did not enforce. Two
// read loops on one Codec violate the single-reader rule codec.go states.
func TestSecondStartPanics(t *testing.T) {
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	p := NewPeer(codecX, RoleHost)
	defer func() {
		_ = p.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()
	p.Start()
	defer func() {
		if recover() == nil {
			t.Error("second Start did not panic")
		}
	}()
	p.Start()
}

// NewCodecOnly builds a raw codec on conn for tests that need to write
// frames a well-behaved Peer would never emit.
func NewCodecOnly(conn net.Conn) *Codec {
	c, _ := NewCodec(conn, conn, 0)
	return c
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// The confused deputy this design has to rule out is a compromised child
// naming someone else's subject on a reverse call and the host honouring it.
// The defence is structural rather than documentary: the inbound param
// structs carry CallerRef, which has no Subject or Tenant field, so a broker
// cannot read one however carelessly it is written. This test fails if anyone
// widens CallerRef or points an inbound struct back at Caller.
func TestInboundCallerCarriesNoSubject(t *testing.T) {
	t.Parallel()

	// A hostile child writes subject/tenant onto every reverse call it can.
	hostile := []byte(`{"entity":"articles","caller":{"subject":"admin",` +
		`"tenant":"other-co","delegation":"h1"}}`)

	var q EntityQueryParams
	if err := json.Unmarshal(hostile, &q); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if q.Caller.Delegation != "h1" {
		t.Fatalf("delegation must survive; got %q", q.Caller.Delegation)
	}

	// The decoded value must expose nothing but the handle. Checked by
	// reflection so the test binds to the TYPE, not to one call site.
	for _, ty := range []reflect.Type{
		reflect.TypeFor[EntityQueryParams](),
		reflect.TypeFor[EntityMutationParams](),
		reflect.TypeFor[SearchQueryParams](),
		reflect.TypeFor[EventEmitParams](),
	} {
		f, ok := ty.FieldByName("Caller")
		if !ok {
			t.Fatalf("SECURITY: [moduleproto] %s lost its Caller field", ty.Name())
		}
		if f.Type != reflect.TypeFor[CallerRef]() {
			t.Fatalf("SECURITY: [moduleproto] %s.Caller is %s, want CallerRef. "+
				"An inbound reverse call must not carry a child-supplied Subject "+
				"or Tenant — the broker derives authority from Delegation alone.",
				ty.Name(), f.Type)
		}
		for _, bad := range []string{"Subject", "Tenant"} {
			if _, found := f.Type.FieldByName(bad); found {
				t.Fatalf("SECURITY: [moduleproto] CallerRef grew a %s field. "+
					"That is the confused-deputy shape this type exists to "+
					"make unrepresentable.", bad)
			}
		}
	}
}

// Property: a panic in registered handler code at ANY inbound dispatch
// surface becomes a paired JSON-RPC error response (or a silent drop for
// notifications), never an unrecovered panic in the serve goroutine.
//
// core/mcp wraps every app-supplied callback in recover with the explicit
// rationale "critical for stdio, where there is no net/http per-request
// recover net"; moduleproto's Peer invokes Handler with no recover at all.
// A module handler (or a host broker handler fed child-supplied params)
// that panics on hostile input takes down the whole process: one frame
// from a compromised child crashes the host supervisor.
func TestHandlerPanicBecomesErrorResponse(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	_ = child.Handle("boom", func(context.Context, json.RawMessage) (any, error) {
		panic("super-secret-handler-detail")
	})
	_ = child.Handle("boom.note", func(context.Context, json.RawMessage) (any, error) {
		panic("super-secret-note-detail")
	})
	_ = child.Handle("ok", func(context.Context, json.RawMessage) (any, error) {
		return "still-alive", nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Surface 1: inbound REQUEST whose handler panics must come back as a
	// paired error response that does not echo the panic value.
	_, err := host.Call(ctx, "boom", nil)
	if err == nil {
		t.Fatal("panicking request handler produced a success response")
	}
	if we := AsError(err); we == nil || we.Code != CodeInternalError {
		t.Fatalf("panic must surface as wire code %d, got %v", CodeInternalError, err)
	}
	if strings.Contains(err.Error(), "super-secret-handler-detail") {
		t.Fatal("panic value leaked to the counterparty")
	}

	// Surface 2: inbound NOTIFICATION whose handler panics must be
	// dropped, not kill the read loop.
	if err := host.Notify(ctx, "boom.note", nil); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// Either way the peer must stay usable.
	raw, err := host.Call(ctx, "ok", nil)
	if err != nil {
		t.Fatalf("peer unusable after a handler panic: %v", err)
	}
	if string(raw) != `"still-alive"` {
		t.Fatalf("post-panic call result = %s", raw)
	}
}

// Property: module.cancel aborts exactly the handlers serving the named
// frame id — a cancel for one id must leave a concurrently served request
// under a different id running, and a cancel naming an unknown id is a
// no-op. TestDuplicateInboundIDsStayCancelable above pins the same-id
// case; this pins the isolation between ids.
func TestCancelNamesOnlyItsOwnFrame(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	var serving atomic.Int64
	_ = child.Handle("hang", func(ctx context.Context, _ json.RawMessage) (any, error) {
		serving.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	})

	type callRes struct {
		raw json.RawMessage
		err error
	}
	// startCall launches one hang call and reports the wire id it was
	// assigned; two concurrent launches race for ids, so the test must
	// cancel by the reported id, not by launch order.
	startCall := func() (<-chan callRes, <-chan uint64) {
		res := make(chan callRes, 1)
		ids := make(chan uint64, 1)
		go func() {
			ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			raw, err := host.CallWithID(ctx, "hang", nil, func(id uint64) { ids <- id })
			res <- callRes{raw, err}
		}()
		return res, ids
	}

	resA, idA := startCall()
	resB, idB := startCall()
	frameA, frameB := <-idA, <-idB
	waitFor(t, func() bool { return serving.Load() == 2 })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// A cancel naming an id that was never issued is a no-op.
	if err := host.Notify(ctx, MethodCancel, CancelParams{RequestID: "999"}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-resB:
		t.Fatalf("cancel for unknown id 999 aborted an unrelated frame: %v", r.err)
	default:
	}

	// Cancel frame A: only A's call returns.
	if err := host.Notify(ctx, MethodCancel, CancelParams{RequestID: strconv.FormatUint(frameA, 10)}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-resA:
		if r.err == nil {
			t.Fatalf("cancelled call returned success: %s", r.raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel for the named id did not unblock that call")
	}
	select {
	case r := <-resB:
		t.Fatalf("cancel for id %d aborted the unrelated id-%d frame: %v", frameA, frameB, r.err)
	default:
	}

	// Frame B remains cancellable by its own name.
	if err := host.Notify(ctx, MethodCancel, CancelParams{RequestID: strconv.FormatUint(frameB, 10)}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-resB:
		if r.err == nil {
			t.Fatalf("frame %d returned success without being cancelled: %s", frameB, r.raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel for the second named id did not unblock its call")
	}
}

// Property: abandonment tombstones absorb exactly one late response per
// id, and once the FIFO window evicts a tombstone the next response for
// that id is the unsolicited protocol fault the window's own contract
// names. TestLateResponseAfterCallTimeoutIsNotFatal pins the in-window
// drop for a fresh abandonment; these are the window edges: the consumed
// tombstone and the evicted tombstone.
func TestAbandonedWindowEvictionStaysFatal(t *testing.T) {
	// setup builds a lone host peer whose counterparty never responds,
	// makes n abandoned (expired-ctx) calls, and drains the host's
	// outbound frames so the writes complete. It returns the hostile
	// write side so subtests can inject late responses by id.
	setup := func(t *testing.T, n int) (*Peer, net.Conn, func()) {
		t.Helper()
		connX, connY := net.Pipe()
		codecX, _ := NewCodec(connX, connX, 0)
		h := NewPeer(codecX, RoleHost)
		h.Start()
		go func() { _, _ = io.Copy(io.Discard, connY) }()
		expired, expCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer expCancel()
		for range n {
			if _, err := h.Call(expired, "anything", nil); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expired-ctx call: err = %v, want DeadlineExceeded", err)
			}
		}
		return h, connY, func() {
			_ = h.Close()
			_ = connX.Close()
			_ = connY.Close()
			<-h.Done()
		}
	}
	writeLate := func(connY net.Conn, id uint64) {
		raw, _ := json.Marshal(NewSuccessResponse(id, json.RawMessage(`{}`)))
		raw = append(raw, '\n')
		go func() { _, _ = connY.Write(raw) }()
	}

	t.Run("tombstone absorbs exactly one late response", func(t *testing.T) {
		h, connY, cleanup := setup(t, 1)
		defer cleanup()

		// The first late response for the abandoned id is dropped.
		writeLate(connY, 1)
		select {
		case <-h.FatalDone():
			t.Fatal("first late response for an abandoned id must be dropped, not fatal")
		case <-time.After(300 * time.Millisecond):
		}

		// The tombstone is consumed by it: an id gets exactly one paired
		// response, so a second one is unsolicited and must fault.
		writeLate(connY, 1)
		select {
		case <-h.FatalDone():
			if h.FatalError() == nil {
				t.Fatal("FatalDone closed but FatalError is nil")
			}
		case <-time.After(time.Second):
			t.Fatal("replayed response for a consumed tombstone must stay fatal")
		}
	})

	t.Run("evicted tombstone stays fatal", func(t *testing.T) {
		// maxAbandonedIDs+1 abandonments evict id 1's tombstone from the
		// window. Its (very) late response then misses both the pending
		// map and the window — the documented unsolicited fault.
		h, connY, cleanup := setup(t, maxAbandonedIDs+1)
		defer cleanup()

		writeLate(connY, 1)
		select {
		case <-h.FatalDone():
			if h.FatalError() == nil {
				t.Fatal("FatalDone closed but FatalError is nil")
			}
		case <-time.After(time.Second):
			t.Fatal("response for an evicted tombstone must stay fatal")
		}
	})
}

// Property: guard clauses refuse bad input without consuming shared
// state — Handle refuses the built-in method and empty names, and a
// refused Call/Notify leaves the inflight budget untouched.
func TestGuardClausesRefuseWithoutState(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()
	_ = child.Handle("echo", func(context.Context, json.RawMessage) (any, error) {
		return "ok", nil
	})

	if err := host.Handle("", func(context.Context, json.RawMessage) (any, error) { return nil, nil }); err == nil {
		t.Error("Handle must refuse an empty method name")
	}
	if err := host.Handle(MethodCancel, func(context.Context, json.RawMessage) (any, error) { return nil, nil }); err == nil {
		t.Error("Handle must refuse replacing the built-in module.cancel handler")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := host.Call(ctx, "", nil); err == nil {
		t.Error("Call must refuse an empty method")
	}
	if err := host.Notify(ctx, "", nil); err == nil {
		t.Error("Notify must refuse an empty method")
	}

	// The refusals must not leak inflight slots: DefaultMaxInflight is 32,
	// so 40 refused calls followed by a real one only succeeds if every
	// refusal released its slot.
	for range 40 {
		if _, err := host.Call(ctx, "", nil); err == nil {
			t.Fatal("refused Call unexpectedly succeeded")
		}
	}
	if _, err := host.Call(ctx, "echo", nil); err != nil {
		t.Fatalf("refused calls leaked inflight slots: %v", err)
	}
}
