package moduleproto

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"runtime"
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
	for i := 0; i < 4; i++ {
		if err := flooder.Notify(context.Background(), "block", nil); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return running.Load() == 4 })

	// Everything past the cap, registered method or not, must be dropped,
	// not queued onto a fresh goroutine each.
	for i := 0; i < 400; i++ {
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
	for i := 0; i < 2; i++ {
		idCopy := id
		if err := raw.WriteFrame(&Frame{JSONRPC: "2.0", ID: &idCopy, Method: "hang"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not start")
		}
	}

	_ = served.Close()
	for i := 0; i < 2; i++ {
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
		reflect.TypeOf(EntityQueryParams{}),
		reflect.TypeOf(EntityMutationParams{}),
		reflect.TypeOf(SearchQueryParams{}),
		reflect.TypeOf(EventEmitParams{}),
	} {
		f, ok := ty.FieldByName("Caller")
		if !ok {
			t.Fatalf("SECURITY: [moduleproto] %s lost its Caller field", ty.Name())
		}
		if f.Type != reflect.TypeOf(CallerRef{}) {
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
