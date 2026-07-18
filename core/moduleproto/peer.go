package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// diagnostic/policy only: the codec and Frame type are symmetric, and a Peer
// of either role can both originate and serve requests. The supervisor uses
// Role to decide policy (e.g. only the host role sends module.cancel; the
// child role's handlers are subject to it).
type Role int

const (
	// RoleHost is the host-side endpoint: originates module.* requests and
	// serves host.* reverse requests.
	RoleHost Role = iota
	// RoleChild is the module-side endpoint: serves module.* requests and
	// originates host.* reverse requests.
	RoleChild
)

// String makes Role log-friendly.
func (r Role) String() string {
	switch r {
	case RoleHost:
		return "host"
	case RoleChild:
		return "child"
	default:
		return fmt.Sprintf("role(%d)", int(r))
	}
}

// Handler is the server-side callback for an inbound request or notification.
// params is the raw JSON of the Frame.Params field; the handler unmarshals it
// into the typed shape it expects for its method. Returning a non-nil err
// causes the Peer to write a JSON-RPC error response with the standard
// internal-error code; for finer-grained codes, return a [*Error].
//
// For notifications (no id) the result is ignored and no response is written;
// only err is observed (and only for local logging — a notification cannot
// produce a wire response).
type Handler func(ctx context.Context, params json.RawMessage) (result any, err error)

// Peer is a full-duplex endpoint over a [Codec]. Both the host and the child
// construct one; each Peer serves inbound requests AND originates outbound
// requests concurrently over the SAME connection. This is the load-bearing
// break from mcpclient, which could originate but not serve.
//
// Per-direction ID correlation (design §4.3 — the most-tested property):
//
//   - Each Peer owns an independent id counter ([Peer.nextID], atomic, starts
//     at 0; the first [Peer.Call] uses id=1).
//   - Each Peer owns its own pending map.
//   - The read loop consults its local pending map ONLY for frames that are
//     responses (method == "" && id present). A request with the same numeric
//     id is handled by the request branch, which echoes the id back without
//     touching the map.
//
// Therefore host-originated id:7 and child-originated id:7 NEVER collide.
// See peer_test.go's interleaved bidirectional test, which pins this property.
//
// Cancellation (design §4.4 module.cancel):
//
//   - The Peer installs a built-in handler for [MethodCancel]. When a
//     module.cancel notification arrives with a given request_id, the Peer
//     cancels the context of the inbound request currently serving that id
//     (the request_id is the inbound frame's id, encoded as a string). The
//     child's handlers observe ctx.Done and abort.
//   - Origination-side cancellation (the host sending module.cancel when ITS
//     Call's ctx expires) is policy, NOT codec behavior. The supervisor wires
//     it by calling [Peer.Notify] from a goroutine that watches ctx.Done.
type Peer struct {
	codec *Codec
	role  Role

	maxInflight      int
	maxServeInflight int

	// Per-direction id counter (the load-bearing field). Atomic; starts at 0
	// so the first Add(1) returns 1 — ids begin at 1, never 0.
	nextID atomic.Uint64

	mu            sync.Mutex // guards pending, inflight, handlers, cancel
	pending       map[uint64]chan *Frame
	inflight      int
	serveInflight int
	handlers      map[string]Handler
	cancels       map[uint64]context.CancelFunc // inbound-request id → cancel

	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{} // closed once on Close(); unblocks in-flight Calls
	done      chan struct{} // closed when read loop exits

	fatalMu sync.Mutex
	fatal   error         // first terminal protocol fault, if any
	fatalCh chan struct{} // closed once on first setFatal; unblocks in-flight Calls
}

// PeerOption configures a Peer at construction.
type PeerOption func(*Peer)

// WithMaxInflight overrides the default in-flight cap ([DefaultMaxInflight])
// for originated requests.
func WithMaxInflight(n int) PeerOption {
	return func(p *Peer) {
		if n > 0 {
			p.maxInflight = n
		}
	}
}

// WithMaxServeInflight overrides the default cap ([DefaultMaxInflight]) on
// concurrently SERVED inbound requests. This bound is what protects a peer
// from a flooding counterparty: the origination-side cap only limits requests
// a peer sends, so a hostile peer that never awaits responses could otherwise
// spawn one handler goroutine per frame it writes. Overflow requests are
// answered immediately with a [CodeInflightCap] error response — never
// silently dropped (a drop would hang the caller) and never queued.
func WithMaxServeInflight(n int) PeerOption {
	return func(p *Peer) {
		if n > 0 {
			p.maxServeInflight = n
		}
	}
}

// WithHandler registers a handler at construction. Repeat to register more.
func WithHandler(method string, h Handler) PeerOption {
	return func(p *Peer) {
		if p.handlers == nil {
			p.handlers = make(map[string]Handler)
		}
		p.handlers[method] = h
	}
}

// NewPeer constructs a Peer over codec with the given role. The Peer does NOT
// start its read loop until [Peer.Start] is called.
func NewPeer(codec *Codec, role Role, opts ...PeerOption) *Peer {
	p := &Peer{
		codec:            codec,
		role:             role,
		maxInflight:      DefaultMaxInflight,
		maxServeInflight: DefaultMaxInflight,
		pending:          make(map[uint64]chan *Frame),
		handlers:         make(map[string]Handler),
		cancels:          make(map[uint64]context.CancelFunc),
		closeCh:          make(chan struct{}),
		done:             make(chan struct{}),
		fatalCh:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}
	// Built-in module.cancel handler. Registered under the canonical method
	// name; user handlers cannot override it (Handle checks and refuses).
	p.handlers[MethodCancel] = builtinCancelHandler(p)
	return p
}

// Role returns the Peer's role.
func (p *Peer) Role() Role { return p.role }

// Handle registers (or replaces) a handler for an inbound method. Registering
// [MethodCancel] is refused — that handler is built-in. Registering a handler
// after [Peer.Start] is allowed (the read loop consults the map under the
// mutex), but doing so concurrently with traffic is unusual.
func (p *Peer) Handle(method string, h Handler) error {
	if method == MethodCancel {
		return fmt.Errorf("moduleproto: %s is a built-in handler", method)
	}
	if method == "" {
		return fmt.Errorf("moduleproto: empty method name")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[method] = h
	return nil
}

// Start launches the read loop. It panics if called twice (the read loop must
// be single-threaded — see [Codec]).
func (p *Peer) Start() {
	go p.readLoop()
}

// Done returns a channel closed when the read loop has exited (clean EOF,
// fatal codec error, or [Peer.Close]). Callers waiting on an in-flight Call
// should also select on Done.
func (p *Peer) Done() <-chan struct{} { return p.done }

// FatalError returns the first terminal error observed by the read loop, or
// nil if the loop ended cleanly (EOF or Close).
func (p *Peer) FatalError() error {
	p.fatalMu.Lock()
	defer p.fatalMu.Unlock()
	return p.fatal
}

func (p *Peer) setFatal(err error) {
	p.fatalMu.Lock()
	defer p.fatalMu.Unlock()
	if p.fatal == nil {
		p.fatal = err
		close(p.fatalCh)
		// Mark closed so the read loop's next error is treated as graceful
		// and so new Call/Notify return ErrClosed. The Peer does NOT own
		// its transport; the supervisor closes the transport in response
		// to FatalDone() firing, which in turn lets the blocked read loop
		// exit and close p.done.
		p.closed.Store(true)
	}
}

// FatalDone returns a channel closed when the first terminal protocol fault is
// observed. The supervisor observes FatalDone, calls FatalError to retrieve
// the cause, then tears down the transport (which lets the read loop exit and
// Done fire). In-flight Calls also select on FatalDone and return the fatal
// error promptly without waiting for transport teardown.
func (p *Peer) FatalDone() <-chan struct{} { return p.fatalCh }

// Call originates a request with the given method and params, waits for the
// paired response, and returns the result's raw bytes. params may be nil
// (no params field), a typed value (JSON-marshaled), or [json.RawMessage]
// (passed through). The id is drawn from this Peer's monotonic counter — the
// first Call uses id=1, the next id=2, etc.
//
// Call is safe for concurrent use by multiple goroutines.
//
// Failure modes:
//
//   - ctx expired: the pending entry is removed and ctx.Err() is returned.
//     Call does NOT automatically emit module.cancel — that is supervisor
//     policy. Origination-side cancellation is a [Peer.Notify] away.
//   - inflight cap reached: returns [ErrInflightCap] without writing anything.
//   - Peer closed (or read loop exited): returns [ErrClosed] (or the terminal
//     [Peer.FatalError] if one was observed).
//   - the child returned a JSON-RPC error: returns it as a [*Error].
//   - codec write failed (e.g. over-cap): returns that terminal error.
func (p *Peer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if p.closed.Load() {
		return nil, ErrClosed
	}
	if method == "" {
		return nil, fmt.Errorf("moduleproto: empty method")
	}

	// Inflight cap. Call-local error, not a protocol fault.
	p.mu.Lock()
	if p.inflight >= p.maxInflight {
		p.mu.Unlock()
		return nil, ErrInflightCap
	}
	p.inflight++
	p.mu.Unlock()

	// id allocation. Add(1) returns 1 on the first call → ids start at 1.
	id := p.nextID.Add(1)

	// Register the pending reply channel BEFORE writing so a fast response
	// can't arrive before we're listening.
	ch := make(chan *Frame, 1) // buffered: deliver() never blocks
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()

	// Cleanup on all return paths.
	defer func() {
		p.mu.Lock()
		// If the response was delivered, the entry is already gone; delete
		// is a no-op then. Otherwise this Call is giving up on its id.
		delete(p.pending, id)
		p.inflight--
		p.mu.Unlock()
	}()

	paramsRaw, err := marshalParams(params)
	if err != nil {
		return nil, fmt.Errorf("moduleproto: marshal params: %w", err)
	}
	idCopy := id
	frame := &Frame{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Method:  method,
		Params:  paramsRaw,
	}
	if err := p.codec.WriteFrame(frame); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closeCh:
		return nil, ErrClosed
	case <-p.fatalCh:
		return nil, p.FatalError()
	case <-p.done:
		// Read loop exited. Surface the terminal error if any, else ErrClosed.
		if ferr := p.FatalError(); ferr != nil {
			return nil, ferr
		}
		return nil, ErrClosed
	case f := <-ch:
		if f.Error != nil {
			return nil, f.Error
		}
		return f.Result, nil
	}
}

// Notify sends a notification (no id, no response expected) with the given
// method and params. notifications are one-way; the peer does not acknowledge.
//
// [MethodCancel] is special: the Peer has a built-in handler for it on the
// receive side, but the origination side is the supervisor's policy (only the
// host sends module.cancel). Notify itself places no role restriction — the
// policy lives above the codec.
func (p *Peer) Notify(ctx context.Context, method string, params any) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if method == "" {
		return fmt.Errorf("moduleproto: empty method")
	}
	paramsRaw, err := marshalParams(params)
	if err != nil {
		return fmt.Errorf("moduleproto: marshal params: %w", err)
	}
	frame := &Frame{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}
	// Notify honors ctx only for the case where the peer is shutting down —
	// the write itself is synchronous and bounded by max_frame_bytes.
	done := make(chan error, 1)
	go func() {
		done <- p.codec.WriteFrame(frame)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closeCh:
		return ErrClosed
	case <-p.fatalCh:
		return p.FatalError()
	case err := <-done:
		return err
	case <-p.done:
		return ErrClosed
	}
}

// Close shuts the peer down. It is idempotent. After Close returns, Call and
// Notify return [ErrClosed]. The Peer does NOT own its Codec's underlying
// io.Closer (the supervisor wires os.Stdin / a net.Conn / etc.); closing the
// transport is the supervisor's job. Close signals the read loop to exit on
// the next read (which will return EOF or a closed-pipe error).
//
// To actually tear down a child, the supervisor: (1) closes the write side so
// the child observes EOF and exits cleanly; (2) after a deadline, kills the
// process; (3) waits. That sequence is the design's Close = stdin.Close →
// Kill → Wait (§4.6 lift list) and lives above this package.
func (p *Peer) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	// Cancel any in-flight inbound handlers so they don't linger.
	p.mu.Lock()
	for id, cancel := range p.cancels {
		cancel()
		delete(p.cancels, id)
	}
	p.mu.Unlock()
	// Close closeCh so in-flight outbound Calls/Notifies unblock with
	// ErrClosed promptly — they no longer need to wait for the read loop to
	// observe the transport teardown.
	close(p.closeCh)
	return nil
}

// ----- read loop -----

func (p *Peer) readLoop() {
	defer close(p.done)
	for {
		f, err := p.codec.ReadFrame()
		if err != nil {
			// io.EOF on a Close()d peer is graceful; do not record it as fatal.
			if p.closed.Load() {
				return
			}
			if !errors.Is(err, io.EOF) {
				p.setFatal(err)
			}
			return
		}
		p.dispatch(f)
	}
}

// dispatch routes an inbound Frame to the request/notification/response branch.
// This is where the per-direction correlation invariant lives.
func (p *Peer) dispatch(f *Frame) {
	switch {
	case f.Method != "" && f.ID != nil:
		// Inbound REQUEST. Dispatch to a handler in its own goroutine so a
		// slow handler cannot stall the read loop (and therefore cannot
		// stall unrelated responses or other concurrent requests). The
		// handler writes a paired response with the same id. The request
		// branch never consults p.pending — that map is reserved for the
		// response branch below. This is the load-bearing rule: a request
		// with the same numeric id as one of our originated requests
		// cannot collide with it.
		//
		// Serve-side concurrency is bounded by maxServeInflight — the
		// origination-side cap only limits requests WE send, so without
		// this bound a flooding counterparty could spawn one goroutine
		// per frame it writes. Overflow gets an immediate CodeInflightCap
		// error response (never a silent drop, which would hang the
		// caller's pending Call).
		p.mu.Lock()
		if p.serveInflight >= p.maxServeInflight {
			p.mu.Unlock()
			_ = p.writeFrame(NewErrorResponse(*f.ID, CodeInflightCap,
				"serve inflight cap reached", nil))
			return
		}
		p.serveInflight++
		p.mu.Unlock()
		f := f
		go func() {
			// The serve slot bounds concurrent HANDLER execution — the thing a
			// flooder could use to spawn work. It is released the moment the
			// handler finishes, BEFORE the response is written. This matters
			// for correctness at the ceiling: the originating peer frees its
			// own slot when it RECEIVES the response and may immediately send
			// its next request; if the serve slot were held until this
			// goroutine's deferred cleanup ran (after the write + a contended
			// lock acquisition), that next request could be falsely rejected
			// even though the counterparty stayed within the agreed
			// concurrency. Freeing at handler completion makes exactly N
			// concurrent callers succeed with no spurious CodeInflightCap.
			resp := p.buildResponse(f)
			p.mu.Lock()
			p.serveInflight--
			p.mu.Unlock()
			if resp != nil {
				_ = p.writeFrame(resp)
			}
		}()
	case f.Method != "" && f.ID == nil:
		// Inbound NOTIFICATION. Dispatch in its own goroutine too —
		// notifications should not block the read loop either. module.cancel
		// is handled here via the built-in handler.
		f := f
		go p.serveNotification(f)
	case f.Method == "" && f.ID != nil:
		// Inbound RESPONSE. Deliver to our pending map. This is the ONLY
		// branch that touches p.pending, and it only ever looks up ids WE
		// originated — so a peer's id:7 lookup never sees the other peer's
		// id:7 (which was a request id on this side, handled above).
		p.deliverResponse(f)
	default:
		// Frame with neither method nor id. UnmarshalJSON already rejects
		// this shape, so reaching here indicates a codec/Frame bypass.
		p.setFatal(fmt.Errorf("%w: dispatch: frame has no method and no id", ErrInvalidFrame))
	}
}

// buildResponse runs the handler for an inbound request and returns the Frame
// the caller must write (never nil for a request — a request always gets a
// paired response so the originating Call unblocks). It does NOT write the
// frame itself: the caller writes it AFTER releasing the serve-inflight slot,
// so the write is not counted against the concurrent-handler cap.
func (p *Peer) buildResponse(f *Frame) *Frame {
	id := *f.ID
	p.mu.Lock()
	h, ok := p.handlers[f.Method]
	p.mu.Unlock()
	if !ok {
		// Method not found: a paired error response so the caller's Call
		// unblocks. A silent drop would hang the originating Call forever.
		return NewErrorResponse(id, CodeMethodNotFound, "method not found: "+f.Method, nil)
	}
	// Derive a cancellable context for this inbound request so module.cancel
	// can abort it and so Peer.Close cancels all in-flight handlers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.mu.Lock()
	p.cancels[id] = cancel
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.cancels, id)
		p.mu.Unlock()
	}()

	result, err := h(ctx, f.Params)
	if err != nil {
		// If the handler returned a *Error, echo its code; else default to
		// the standard internal-error code.
		we := AsError(err)
		if we == nil {
			we = &Error{
				Code:    CodeInternalError,
				Message: err.Error(),
			}
		}
		return NewErrorResponse(id, we.Code, we.Message, we.Data)
	}
	resultRaw, mErr := marshalParams(result)
	if mErr != nil {
		return NewErrorResponse(id, CodeInternalError, "marshal result: "+mErr.Error(), nil)
	}
	return NewSuccessResponse(id, resultRaw)
}

func (p *Peer) serveNotification(f *Frame) {
	p.mu.Lock()
	h, ok := p.handlers[f.Method]
	p.mu.Unlock()
	if !ok {
		// Unknown notification: silently drop. JSON-RPC 2.0 notifications
		// are not acknowledged, so there is nothing to write. We log via
		// the fatal hook only for diagnostics — NOT fatal.
		return
	}
	// Notifications get a context that is cancelled on Peer.Close. There is
	// no inbound id to register under, so we use a throwaway ctx.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Best-effort; errors are unobservable on the wire.
	_, _ = h(ctx, f.Params)
}

func (p *Peer) deliverResponse(f *Frame) {
	id := *f.ID
	p.mu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if !ok {
		// A response with no matching pending id. Under correct per-direction
		// correlation this is impossible on a healthy peer — treat as a
		// terminal protocol fault. During shutdown, however, late responses
		// for Calls that already gave up are expected; suppress in that case.
		if p.closed.Load() {
			return
		}
		p.setFatal(fmt.Errorf("%w: unsolicited response id=%d", ErrInvalidFrame, id))
		return
	}
	// ch is buffered (cap 1) so this never blocks even if the Call goroutine
	// has not yet entered its select.
	ch <- f
}

func (p *Peer) writeFrame(f *Frame) error {
	if err := p.codec.WriteFrame(f); err != nil {
		// A write failure (over-cap, broken pipe) is terminal: the peer is
		// desynchronized. Record it so in-flight Calls see a real error
		// rather than a hang on p.done.
		p.setFatal(err)
		return err
	}
	return nil
}

// ----- helpers -----

// marshalParams coerces several common input shapes to json.RawMessage. nil
// and empty json.RawMessage both produce nil (no params field on the wire).
func marshalParams(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	switch x := v.(type) {
	case json.RawMessage:
		if len(x) == 0 {
			return nil, nil
		}
		return x, nil
	case []byte:
		if len(x) == 0 {
			return nil, nil
		}
		// Validate: must be valid JSON.
		if !json.Valid(x) {
			return nil, fmt.Errorf("params bytes are not valid JSON")
		}
		return json.RawMessage(x), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

// builtinCancelHandler returns the handler for [MethodCancel]. The handler
// looks up the inbound request id encoded in [CancelParams].RequestID and
// cancels that request's context. RequestID is the string form of the inbound
// frame id (the JSON-RPC id the host used for module.http, etc.).
func builtinCancelHandler(p *Peer) Handler {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var cp CancelParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &cp); err != nil {
				return nil, nil // notifications are unobservable; drop on malformed
			}
		}
		// Parse the request_id. Under moduleproto it is the inbound frame
		// id; the host mints it as a string but it MUST parse to a uint64
		// matching a currently-served inbound id. Anything else is a no-op.
		var id uint64
		if _, err := fmt.Sscanf(cp.RequestID, "%d", &id); err != nil || id == 0 {
			return nil, nil
		}
		p.mu.Lock()
		cancel, ok := p.cancels[id]
		p.mu.Unlock()
		if ok {
			cancel()
		}
		return nil, nil
	}
}
