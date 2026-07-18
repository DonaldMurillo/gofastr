package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"testing"
	"time"
)

// newPeerPair wires two Peers (host role + child role) over a synchronous
// in-memory net.Pipe, starts both read loops, and returns a cleanup function.
// This is the substrate for every full-duplex test.
func newPeerPair(t *testing.T, maxFrame int) (*Peer, *Peer, net.Conn, net.Conn, func()) {
	t.Helper()
	connA, connB := net.Pipe()
	codecA, err := NewCodec(connA, connA, maxFrame)
	if err != nil {
		connA.Close()
		connB.Close()
		t.Fatalf("NewCodec A: %v", err)
	}
	codecB, err := NewCodec(connB, connB, maxFrame)
	if err != nil {
		connA.Close()
		connB.Close()
		t.Fatalf("NewCodec B: %v", err)
	}
	host := NewPeer(codecA, RoleHost)
	child := NewPeer(codecB, RoleChild)
	host.Start()
	child.Start()
	cleanup := func() {
		_ = host.Close()
		_ = child.Close()
		_ = connA.Close()
		_ = connB.Close()
		<-host.Done()
		<-child.Done()
	}
	return host, child, connA, connB, cleanup
}

// TestPeerCallRoundTrip: a single Call from host to child returns the child
// handler's result. The simplest full-duplex smoke test.
func TestPeerCallRoundTrip(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) {
		return struct {
			Echo json.RawMessage `json:"echo"`
		}{Echo: p}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := host.Call(ctx, "echo", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var got struct {
		Echo json.RawMessage `json:"echo"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Echo) != `{"k":"v"}` {
		t.Fatalf("echo round-trip wrong: %s", got.Echo)
	}
}

// TestPeerNotifyNoResponse: a notification arrives at the handler, no response
// is written (and no Call hangs).
func TestPeerNotifyNoResponse(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	got := make(chan string, 1)
	if err := child.Handle("ping", func(_ context.Context, p json.RawMessage) (any, error) {
		got <- string(p)
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Notify(ctx, "ping", json.RawMessage(`{"hi":1}`)); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	select {
	case s := <-got:
		if s != `{"hi":1}` {
			t.Fatalf("notify payload wrong: %s", s)
		}
	case <-time.After(time.Second):
		t.Fatal("notification handler did not run")
	}
}

// TestPerDirectionIDCorrelation is THE load-bearing property (design §4.3):
// host-originated id:N and child-originated id:N never collide. Each side's
// pending map is consulted only for RESPONSES it reads, and a request with
// the same numeric id is handled by the request branch (no map lookup).
//
// We pin it by having BOTH peers register echo handlers, BOTH peers originate
// a Call with overlapping ids, and asserting each Call gets its OWN peer's
// handler response — not the other's.
func TestPerDirectionIDCorrelation(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// Host-side handler echoes its role + the params it received.
	if err := host.Handle("h.echo", func(_ context.Context, p json.RawMessage) (any, error) {
		return map[string]any{"from": "host", "in": json.RawMessage(p)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	// Child-side handler echoes its role + the params it received.
	if err := child.Handle("c.echo", func(_ context.Context, p json.RawMessage) (any, error) {
		return map[string]any{"from": "child", "in": json.RawMessage(p)}, nil
	}); err != nil {
		t.Fatal(err)
	}

	// Host issues Call #1 (id=1 on host's counter) to c.echo with "HOST-PAYLOAD".
	// Child issues Call #1 (id=1 on child's counter — same numeric id!) to h.echo
	// with "CHILD-PAYLOAD".
	//
	// If per-direction correlation is broken (e.g. a shared pending map), one
	// Call would receive the other's response and the from-field check below
	// would fail.
	const hostPayload = "HOST-PAYLOAD"
	const childPayload = "CHILD-PAYLOAD"

	type result struct {
		from string
		in   string
		err  error
	}
	hostRes := make(chan result, 1)
	childRes := make(chan result, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		raw, err := host.Call(ctx, "c.echo", map[string]any{"p": hostPayload})
		if err != nil {
			hostRes <- result{err: err}
			return
		}
		var r struct {
			From string          `json:"from"`
			In   json.RawMessage `json:"in"`
		}
		if jErr := json.Unmarshal(raw, &r); jErr != nil {
			hostRes <- result{err: jErr}
			return
		}
		hostRes <- result{from: r.From, in: string(r.In)}
	}()
	go func() {
		defer wg.Done()
		raw, err := child.Call(ctx, "h.echo", map[string]any{"p": childPayload})
		if err != nil {
			childRes <- result{err: err}
			return
		}
		var r struct {
			From string          `json:"from"`
			In   json.RawMessage `json:"in"`
		}
		if jErr := json.Unmarshal(raw, &r); jErr != nil {
			childRes <- result{err: jErr}
			return
		}
		childRes <- result{from: r.From, in: string(r.In)}
	}()

	hr := <-hostRes
	cr := <-childRes
	wg.Wait()

	if hr.err != nil {
		t.Fatalf("host Call err: %v", hr.err)
	}
	if cr.err != nil {
		t.Fatalf("child Call err: %v", cr.err)
	}
	// Host sent to c.echo → child handled → response From must be "child"
	// and payload must round-trip the host's own payload.
	if hr.from != "child" {
		t.Errorf("host got response from=%q, want %q (response misrouted?)", hr.from, "child")
	}
	if !contains(hr.in, hostPayload) {
		t.Errorf("host got payload %q, want %q", hr.in, hostPayload)
	}
	// Child sent to h.echo → host handled → response From must be "host".
	if cr.from != "host" {
		t.Errorf("child got response from=%q, want %q (response misrouted?)", cr.from, "host")
	}
	if !contains(cr.in, childPayload) {
		t.Errorf("child got payload %q, want %q", cr.in, childPayload)
	}
}

// TestInterleavedBidirectional stress-tests per-direction correlation with N
// concurrent Calls from each side. All numeric ids overlap (1..N on each
// side); each Call must receive its own response.
func TestInterleavedBidirectional(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// Both handlers echo back the value of "tag" they were sent.
	echoHandler := func(role string) Handler {
		return func(_ context.Context, p json.RawMessage) (any, error) {
			return map[string]any{"role": role, "echo": json.RawMessage(p)}, nil
		}
	}
	if err := host.Handle("h", echoHandler("host")); err != nil {
		t.Fatal(err)
	}
	if err := child.Handle("c", echoHandler("child")); err != nil {
		t.Fatal(err)
	}

	const N = 16
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	type outcome struct {
		peerRole string // "host" or "child"
		tag      int
		respRole string
		err      error
	}
	out := make(chan outcome, N*2)

	dispatch := func(p *Peer, peerRole, method, respWant string) {
		defer wg.Done()
		for i := 1; i <= N; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				raw, err := p.Call(ctx, method, map[string]any{"tag": i})
				if err != nil {
					out <- outcome{peerRole: peerRole, tag: i, err: err}
					return
				}
				var r struct {
					Role string          `json:"role"`
					Echo json.RawMessage `json:"echo"`
				}
				if jErr := json.Unmarshal(raw, &r); jErr != nil {
					out <- outcome{peerRole: peerRole, tag: i, err: jErr}
					return
				}
				out <- outcome{peerRole: peerRole, tag: i, respRole: r.Role}
			}()
		}
	}
	wg.Add(2)
	go dispatch(host, "host", "c", "child")
	go dispatch(child, "child", "h", "host")

	go func() {
		wg.Wait()
		close(out)
	}()

	hostGotChild := 0
	childGotHost := 0
	for o := range out {
		if o.err != nil {
			t.Errorf("peer=%s tag=%d err=%v", o.peerRole, o.tag, o.err)
			continue
		}
		switch {
		case o.peerRole == "host" && o.respRole == "child":
			hostGotChild++
		case o.peerRole == "child" && o.respRole == "host":
			childGotHost++
		default:
			t.Errorf("peer=%s tag=%d respRole=%q (misroute)", o.peerRole, o.tag, o.respRole)
		}
	}
	if hostGotChild != N {
		t.Errorf("host received %d/%d responses from child", hostGotChild, N)
	}
	if childGotHost != N {
		t.Errorf("child received %d/%d responses from host", childGotHost, N)
	}
}

// TestPeerInflightCapRejects: once maxInflight is reached, the next Call
// returns ErrInflightCap without writing.
func TestPeerInflightCapRejects(t *testing.T) {
	// Dedicated pair with a host whose inflight cap is 2.
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	codecY, _ := NewCodec(connY, connY, 0)
	cappedHost := NewPeer(codecX, RoleHost, WithMaxInflight(2))
	childY := NewPeer(codecY, RoleChild)
	defer func() {
		_ = cappedHost.Close()
		_ = childY.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()
	release := make(chan struct{})
	if err := childY.Handle("block", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	cappedHost.Start()
	childY.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Issue 2 calls — they sit in the child handler's select, holding inflight.
	for range 2 {
		go func() {
			_, _ = cappedHost.Call(ctx, "block", nil)
		}()
	}
	// Give them a moment to register on the child side.
	time.Sleep(100 * time.Millisecond)

	// Third call must hit the inflight cap.
	_, err := cappedHost.Call(ctx, "block", nil)
	if !errors.Is(err, ErrInflightCap) {
		t.Fatalf("expected ErrInflightCap, got %v", err)
	}

	// Release all handlers so the test exits cleanly.
	close(release)
}

// TestPeerMethodNotFoundReplies: an unknown method produces a JSON-RPC error
// response (not a silent drop that hangs the originating Call).
func TestPeerMethodNotFoundReplies(t *testing.T) {
	host, _, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// No handler registered for "missing".
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := host.Call(ctx, "missing", nil)
	if err == nil {
		t.Fatal("expected method-not-found error")
	}
	we := AsError(err)
	if we == nil {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if we.Code != CodeMethodNotFound {
		t.Fatalf("code = %d, want %d", we.Code, CodeMethodNotFound)
	}
}

// TestPeerUnsolicitedRespIsFatal: a response with no matching pending id is a
// protocol violation under correct per-direction correlation; the peer records
// a fatal error.
func TestPeerUnsolicitedRespIsFatal(t *testing.T) {
	host, _, _, connA, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// Inject a raw unsolicited response directly into host's codec.
	resp := NewSuccessResponse(99999, json.RawMessage(`{}`))
	body, _ := json.Marshal(resp)
	body = append(body, '\n')
	// Write to connA's write side (the side the host READS from). Wait —
	// connA is the host's end: it reads from B→A and writes A→B. To inject
	// into host's read, we need to write to connB. We don't have connB here
	// (it's still owned by the closed peer B).
	// Simpler: open a fresh pipe, build a host with no child, and write
	// a response directly.
	_ = body
	_ = connA
	_ = host

	// Fresh substrate.
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	h := NewPeer(codecX, RoleHost)
	h.Start()
	defer func() {
		_ = h.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()

	unsolicited := NewSuccessResponse(777, json.RawMessage(`{}`))
	raw, _ := json.Marshal(unsolicited)
	raw = append(raw, '\n')
	go func() { _, _ = connY.Write(raw) }()

	select {
	case <-h.FatalDone():
	case <-time.After(time.Second):
		t.Fatal("peer did not record fatal on unsolicited response")
	}
	if err := h.FatalError(); err == nil {
		t.Fatal("expected fatal error for unsolicited response")
	}
}

// TestPeerCloseUnblocksCalls: an in-flight Call returns (with ErrClosed or the
// fatal error) when the peer is torn down.
func TestPeerCloseUnblocksCalls(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// Child handler that never returns — must be unblocked by Close.
	stop := make(chan struct{})
	if err := child.Handle("hang", func(ctx context.Context, _ json.RawMessage) (any, error) {
		// Block until EITHER ctx is cancelled (e.g. child.Close) OR the
		// test releases us via stop. host.Close alone unblocks the host's
		// Call via closeCh; this handler just needs to not hang the child.
		select {
		case <-ctx.Done():
		case <-stop:
		}
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	callErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := host.Call(ctx, "hang", nil)
		callErr <- err
	}()

	// Tear down the host peer by closing the transport (the supervisor does
	// this; Peer.Close itself does not own the transport).
	time.Sleep(100 * time.Millisecond)
	_ = host.Close()
	close(stop) // release the child handler so child's read loop can drain

	select {
	case err := <-callErr:
		if err == nil {
			t.Fatal("Call returned nil error on Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not unblock on Close")
	}
}

// TestPeerModuleCancelCancels: when a module.cancel notification arrives for
// an in-flight inbound request id, the handler's ctx is cancelled. This is
// the built-in handler that makes per-call deadline propagation work.
func TestPeerModuleCancelCancels(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	cancelled := make(chan uint64, 1)
	if err := child.Handle("work", func(ctx context.Context, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		// Signal that cancellation occurred. We can't read the inbound id
		// directly from inside the handler, so just observe ctx.Done.
		cancelled <- 1
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = host.Call(ctx, "work", nil)
	}()

	// Give the call a moment to reach the child.
	time.Sleep(100 * time.Millisecond)

	// Send module.cancel for inbound id=1 (host's first call id).
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Notify(ctx, MethodCancel, CancelParams{RequestID: "1"}); err != nil {
		t.Fatalf("Notify cancel: %v", err)
	}

	select {
	case <-cancelled:
		// handler's ctx was cancelled — built-in module.cancel worked.
	case <-time.After(time.Second):
		t.Fatal("handler ctx was not cancelled by module.cancel")
	}
}

// TestPeerConcurrentCallsRoundTrip: many concurrent Calls from one side all
// receive their correct responses (id correlation under load).
func TestPeerConcurrentCallsRoundTrip(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle("square", func(_ context.Context, p json.RawMessage) (any, error) {
		var r struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(p, &r); err != nil {
			return nil, err
		}
		return map[string]any{"r": r.N * r.N}, nil
	}); err != nil {
		t.Fatal(err)
	}

	const N = 32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 1; i <= N; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			raw, err := host.Call(ctx, "square", map[string]any{"n": n})
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", n, err)
				return
			}
			var r struct {
				R int `json:"r"`
			}
			if jErr := json.Unmarshal(raw, &r); jErr != nil {
				errs <- fmt.Errorf("decode %d: %w", n, jErr)
				return
			}
			if r.R != n*n {
				errs <- fmt.Errorf("call %d: got %d want %d", n, r.R, n*n)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestPeerHandlerReturnsError: a handler returning a generic error produces a
// JSON-RPC error response with the internal-error code; a handler returning
// *Error echoes its code.
func TestPeerHandlerReturnsError(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle("generic", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}
	if err := child.Handle("shaped", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, &Error{Code: CodeInvalidParams, Message: "nope"}
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := host.Call(ctx, "generic", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if we := AsError(err); we == nil || we.Code != CodeInternalError {
		t.Fatalf("generic error code wrong: %+v", we)
	}

	_, err = host.Call(ctx, "shaped", nil)
	if err == nil {
		t.Fatal("expected shaped error")
	}
	if we := AsError(err); we == nil || we.Code != CodeInvalidParams {
		t.Fatalf("shaped error code wrong: %+v", we)
	}
}

// TestPeerEOFIsGraceful: a clean EOF on the read side (no Close signal) does
// not register a fatal — it just ends the read loop. io.EOF on a healthy
// transport is the normal shutdown signal.
func TestPeerEOFIsGraceful(t *testing.T) {
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	h := NewPeer(codecX, RoleHost)
	h.Start()

	// Close the OTHER side — host's read sees EOF.
	_ = connY.Close()

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("read loop did not exit on EOF")
	}
	if err := h.FatalError(); err != nil {
		t.Fatalf("EOF should not be fatal, got %v", err)
	}
	_ = connX.Close()
	_ = io.EOF
}

// contains is a tiny helper so we don't import strings just for one check.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestServeSideInflightCap(t *testing.T) {
	// A hostile peer floods requests without awaiting responses. The serve
	// side must bound concurrent handlers and answer overflow with
	// CodeInflightCap instead of spawning unbounded goroutines.
	connX, connY := net.Pipe()
	codecX, _ := NewCodec(connX, connX, 0)
	codecY, _ := NewCodec(connY, connY, 0)
	served := NewPeer(codecX, RoleHost, WithMaxServeInflight(2))
	flooder := NewPeer(codecY, RoleChild, WithMaxInflight(64))
	defer func() {
		_ = served.Close()
		_ = flooder.Close()
		_ = connX.Close()
		_ = connY.Close()
	}()
	release := make(chan struct{})
	if err := served.Handle("block", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	served.Start()
	flooder.Start()

	type res struct {
		raw json.RawMessage
		err error
	}
	results := make(chan res, 3)
	for i := 0; i < 3; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			raw, err := flooder.Call(ctx, "block", nil)
			results <- res{raw, err}
		}()
	}

	// Exactly one of the three must be rejected with CodeInflightCap while
	// the other two sit in the blocked handler.
	var rejected *Error
	select {
	case r := <-results:
		if r.err == nil {
			t.Fatalf("expected the overflow call to fail, got result %s", r.raw)
		}
		rejected = AsError(r.err)
	case <-time.After(3 * time.Second):
		t.Fatal("no overflow rejection arrived; serve side is unbounded")
	}
	if rejected == nil || rejected.Code != CodeInflightCap {
		t.Fatalf("overflow error = %v, want wire code %d", rejected, CodeInflightCap)
	}

	close(release)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("blocked call %d failed after release: %v", i, r.err)
		}
	}
}
