package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Role says which side of the pipe a Peer is; both roles can call and serve.
type Role int

const (
	// RoleHost sends module.* requests and serves host.* ones.
	RoleHost Role = iota
	// RoleChild serves module.* requests and sends host.* ones.
	RoleChild
)

// String names the role.
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

// Handler serves one inbound request or notification. A *Error crosses the
// wire as itself; any other error as CodeInternalError with a generic text.
type Handler func(ctx context.Context, params json.RawMessage) (result any, err error)

// Peer is one full-duplex endpoint over a Codec: it sends requests and
// serves the counterparty's on the same connection, each side with its own ids.
type Peer struct {
	codec *Codec
	role  Role

	maxInflight      int
	maxServeInflight int
	serveGate        func(method string) *Error

	nextID atomic.Uint64

	mu            sync.Mutex
	pending       map[uint64]chan *Frame
	abandoned     map[uint64]struct{}
	abandonedFIFO []uint64
	inflight      int
	serveInflight int
	respPending   int
	notifyPending int
	handlers      map[string]Handler
	cancels       map[uint64][]*cancelSlot

	started   atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
	done      chan struct{}

	serveCtx    context.Context
	stopServing context.CancelFunc

	fatalMu sync.Mutex
	fatal   error
	fatalCh chan struct{}
}

// PeerOption configures a Peer at construction.
type PeerOption func(*Peer)

// WithMaxInflight sets the cap on originated requests and on pending
// Notify writes (default DefaultMaxInflight).
func WithMaxInflight(n int) PeerOption {
	return func(p *Peer) {
		if n > 0 {
			p.maxInflight = n
		}
	}
}

// WithMaxServeInflight caps served handlers and unwritten served responses
// (default DefaultMaxInflight): an extra request is answered CodeInflightCap,
// an extra notification dropped, an extra response faults with ErrStalled.
func WithMaxServeInflight(n int) PeerOption {
	return func(p *Peer) {
		if n > 0 {
			p.maxServeInflight = n
		}
	}
}

// WithServeGate runs gate before every served request or notification
// except module.cancel; a non-nil *Error refuses the request with it (a
// notification is dropped). A panicking gate refuses.
func WithServeGate(gate func(method string) *Error) PeerOption {
	return func(p *Peer) { p.serveGate = gate }
}

// WithHandler registers a handler at construction.
func WithHandler(method string, h Handler) PeerOption {
	return func(p *Peer) {
		if p.handlers == nil {
			p.handlers = make(map[string]Handler)
		}
		p.handlers[method] = h
	}
}

// NewPeer builds a Peer over codec; Start launches its read loop.
func NewPeer(codec *Codec, role Role, opts ...PeerOption) *Peer {
	p := &Peer{
		codec:            codec,
		role:             role,
		maxInflight:      DefaultMaxInflight,
		maxServeInflight: DefaultMaxInflight,
		pending:          make(map[uint64]chan *Frame),
		abandoned:        make(map[uint64]struct{}),
		handlers:         make(map[string]Handler),
		cancels:          make(map[uint64][]*cancelSlot),
		closeCh:          make(chan struct{}),
		done:             make(chan struct{}),
		fatalCh:          make(chan struct{}),
	}
	p.serveCtx, p.stopServing = context.WithCancel(context.Background())
	for _, opt := range opts {
		opt(p)
	}
	p.handlers[MethodCancel] = builtinCancelHandler(p)
	return p
}

// Role returns the Peer's role.
func (p *Peer) Role() Role { return p.role }

// Handle registers or replaces a handler. It refuses an empty method and
// module.cancel, whose handler is built in.
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

// Start launches the read loop. It panics when called twice.
func (p *Peer) Start() {
	if !p.started.CompareAndSwap(false, true) {
		panic("moduleproto: Peer.Start called twice")
	}
	go p.readLoop()
}

// Done is closed when the read loop has exited.
func (p *Peer) Done() <-chan struct{} { return p.done }

// FatalError returns the first terminal fault, or nil.
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
		p.closed.Store(true)
		p.stopServing()
	}
}

// FatalDone is closed on the first terminal fault; the transport's owner
// tears the transport down when it fires.
func (p *Peer) FatalDone() <-chan struct{} { return p.fatalCh }

// Call sends a request and returns its result, or ctx.Err(), ErrInflightCap
// (nothing written), ErrClosed or the terminal fault, or the response's
// *Error. params may be nil, a value or json.RawMessage.
func (p *Peer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.call(ctx, method, params, nil)
}

// CallWithID is Call that reports the frame id to onID, synchronously,
// once the ctx and params checks pass and before the frame is written. A
// ctx already ended when onID returns gives ctx.Err() with nothing
// written; one that ends later can give ctx.Err() after the write.
// module.cancel names that id.
func (p *Peer) CallWithID(ctx context.Context, method string, params any, onID func(id uint64)) (json.RawMessage, error) {
	return p.call(ctx, method, params, onID)
}

func (p *Peer) call(ctx context.Context, method string, params any, onID func(id uint64)) (json.RawMessage, error) {
	if p.closed.Load() {
		return nil, ErrClosed
	}
	if method == "" {
		return nil, fmt.Errorf("moduleproto: empty method")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	atCap := p.inflight >= p.maxInflight
	p.mu.Unlock()
	if atCap {
		return nil, ErrInflightCap
	}

	paramsRaw, err := marshalParams(params)
	if err != nil {
		return nil, fmt.Errorf("moduleproto: marshal params: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	if p.inflight >= p.maxInflight {
		p.mu.Unlock()
		return nil, ErrInflightCap
	}
	p.inflight++
	p.mu.Unlock()

	id := p.nextID.Add(1)

	ch := make(chan *Frame, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()

	var writing chan error

	defer func() {
		p.mu.Lock()
		if _, stillPending := p.pending[id]; stillPending {
			delete(p.pending, id)
			p.noteAbandonedLocked(id)
		}
		if writing == nil {
			p.inflight--
		}
		p.mu.Unlock()
		if writing != nil {
			go func(w <-chan error) {
				<-w
				p.mu.Lock()
				p.inflight--
				p.mu.Unlock()
			}(writing)
		}
	}()

	if onID != nil {
		onID(id)
	}

	idCopy := id
	frame := &Frame{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Method:  method,
		Params:  paramsRaw,
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wrote := make(chan error, 1)
	writing = wrote
	go func() { wrote <- p.codec.WriteFrame(frame) }()

	var answer *Frame
	for {
		select {
		case err := <-writing:
			writing = nil
			if answer != nil {
				return answerResult(answer)
			}
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.closeCh:
			return nil, ErrClosed
		case <-p.fatalCh:
			return nil, p.FatalError()
		case <-p.done:
			if ferr := p.FatalError(); ferr != nil {
				return nil, ferr
			}
			return nil, ErrClosed
		case f := <-ch:
			if writing != nil {
				answer = f
				continue
			}
			return answerResult(f)
		}
	}
}

func answerResult(f *Frame) (json.RawMessage, error) {
	if f.Error != nil {
		return nil, f.Error
	}
	return f.Result, nil
}

// Notify sends a notification. When ctx ends first it returns ctx.Err()
// while the write goes on; past the inflight cap of pending writes it
// returns ErrInflightCap without writing.
func (p *Peer) Notify(ctx context.Context, method string, params any) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if method == "" {
		return fmt.Errorf("moduleproto: empty method")
	}
	if err := ctx.Err(); err != nil {
		return err
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
	p.mu.Lock()
	if p.notifyPending >= p.maxInflight {
		p.mu.Unlock()
		return ErrInflightCap
	}
	p.notifyPending++
	p.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		err := p.codec.WriteFrame(frame)
		p.mu.Lock()
		p.notifyPending--
		p.mu.Unlock()
		done <- err
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

// Close makes Call and Notify return ErrClosed and cancels served handlers;
// it leaves the transport to its owner. Safe to call twice.
func (p *Peer) Close() error {
	p.closed.Store(true)
	p.stopServing()
	p.closeOnce.Do(func() { close(p.closeCh) })
	return nil
}

type cancelSlot struct{ cancel context.CancelFunc }

func (p *Peer) retireCancel(id uint64, slot *cancelSlot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	slots := p.cancels[id]
	for i, s := range slots {
		if s != slot {
			continue
		}
		slots = append(slots[:i], slots[i+1:]...)
		break
	}
	if len(slots) == 0 {
		delete(p.cancels, id)
		return
	}
	p.cancels[id] = slots
}

func (p *Peer) readLoop() {
	defer close(p.done)
	defer p.stopServing()
	for {
		f, err := p.codec.ReadFrame()
		if err != nil {
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

func (p *Peer) dispatch(f *Frame) {
	select {
	case <-p.fatalCh:
		return
	default:
	}
	switch {
	case f.Method != "" && f.ID != nil:
		p.mu.Lock()
		if p.serveInflight >= p.maxServeInflight {
			stalled := p.respPending >= p.maxServeInflight
			if !stalled {
				p.respPending++
			}
			p.mu.Unlock()
			if stalled {
				p.setFatal(fmt.Errorf("%w: %d responses unwritten", ErrStalled, p.maxServeInflight))
				return
			}
			go p.writeServed(NewErrorResponse(*f.ID, CodeInflightCap,
				"serve inflight cap reached", nil))
			return
		}
		p.serveInflight++
		p.mu.Unlock()
		f := f
		go func() {
			resp := p.buildResponse(f)
			p.mu.Lock()
			p.serveInflight--
			stalled := resp != nil && p.respPending >= p.maxServeInflight
			if resp != nil && !stalled {
				p.respPending++
			}
			p.mu.Unlock()
			if stalled {
				p.setFatal(fmt.Errorf("%w: %d responses unwritten", ErrStalled, p.maxServeInflight))
				return
			}
			if resp != nil {
				p.writeServed(resp)
			}
		}()
	case f.Method != "" && f.ID == nil:
		if f.Method == MethodCancel {
			p.serveNotification(f)
			return
		}
		p.mu.Lock()
		_, known := p.handlers[f.Method]
		if !known {
			p.mu.Unlock()
			return
		}
		if p.serveInflight >= p.maxServeInflight {
			p.mu.Unlock()
			return
		}
		p.serveInflight++
		p.mu.Unlock()
		f := f
		go func() {
			defer func() {
				p.mu.Lock()
				p.serveInflight--
				p.mu.Unlock()
			}()
			p.serveNotification(f)
		}()
	case f.Method == "" && f.ID != nil:
		p.deliverResponse(f)
	default:
		p.setFatal(fmt.Errorf("%w: dispatch: frame has no method and no id", ErrInvalidFrame))
	}
}

func (p *Peer) buildResponse(f *Frame) *Frame {
	id := *f.ID
	if refused := p.gate(f.Method); refused != nil {
		return NewErrorResponse(id, refused.Code, refused.Message, refused.Data)
	}
	p.mu.Lock()
	h, ok := p.handlers[f.Method]
	p.mu.Unlock()
	if !ok {
		return NewErrorResponse(id, CodeMethodNotFound, "method not found: "+f.Method, nil)
	}
	ctx, cancel := context.WithCancel(p.serveCtx)
	defer cancel()
	slot := &cancelSlot{cancel: cancel}
	p.mu.Lock()
	p.cancels[id] = append(p.cancels[id], slot)
	p.mu.Unlock()
	defer p.retireCancel(id, slot)

	result, err := runHandler(h, ctx, f.Method, f.Params)
	if err != nil {
		we := AsError(err)
		if we == nil {
			we = &Error{
				Code:    CodeInternalError,
				Message: "internal handler error",
			}
		}
		return NewErrorResponse(id, we.Code, we.Message, we.Data)
	}
	resultRaw, mErr := marshalParams(result)
	if mErr != nil {
		return NewErrorResponse(id, CodeInternalError, "internal error marshaling result", nil)
	}
	return NewSuccessResponse(id, resultRaw)
}

var errHandlerPanicked = errors.New("moduleproto: handler panicked")

func runHandler(h Handler, ctx context.Context, method string, params json.RawMessage) (result any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in module handler recovered", "operation", "handler",
				"method", textsafe.SanitizeControlBytes(method), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			result = nil
			err = errHandlerPanicked
		}
	}()
	return h(ctx, params)
}

func (p *Peer) serveNotification(f *Frame) {
	if p.gate(f.Method) != nil {
		return
	}
	p.mu.Lock()
	h, ok := p.handlers[f.Method]
	p.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(p.serveCtx)
	defer cancel()
	_, _ = runHandler(h, ctx, f.Method, f.Params)
}

func (p *Peer) gate(method string) (refused *Error) {
	if p.serveGate == nil || method == MethodCancel {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in module serve gate recovered", "operation", "serve gate",
				"method", textsafe.SanitizeControlBytes(method), "panic", textsafe.Recovered(r), "stack", string(debug.Stack()))
			refused = &Error{Code: CodeInternalError, Message: "serve gate failed"}
		}
	}()
	return p.serveGate(method)
}

const maxAbandonedIDs = 1024

func (p *Peer) noteAbandonedLocked(id uint64) {
	if p.abandoned == nil {
		p.abandoned = make(map[uint64]struct{})
	}
	p.abandoned[id] = struct{}{}
	p.abandonedFIFO = append(p.abandonedFIFO, id)
	for len(p.abandoned) > maxAbandonedIDs {
		oldest := p.abandonedFIFO[0]
		p.abandonedFIFO = p.abandonedFIFO[1:]
		delete(p.abandoned, oldest)
	}
	if len(p.abandonedFIFO) > 2*maxAbandonedIDs {
		live := make([]uint64, 0, len(p.abandoned))
		for _, a := range p.abandonedFIFO {
			if _, ok := p.abandoned[a]; ok {
				live = append(live, a)
			}
		}
		p.abandonedFIFO = live
	}
}

func (p *Peer) deliverResponse(f *Frame) {
	id := *f.ID
	p.mu.Lock()
	ch, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	} else if _, abandoned := p.abandoned[id]; abandoned {
		delete(p.abandoned, id)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if !ok {
		if p.closed.Load() {
			return
		}
		p.setFatal(fmt.Errorf("%w: unsolicited response id=%d", ErrInvalidFrame, id))
		return
	}
	ch <- f
}

func (p *Peer) writeServed(resp *Frame) {
	err := p.codec.writeFrame(resp, func() {
		p.mu.Lock()
		p.respPending--
		p.mu.Unlock()
	})
	if err != nil {
		p.setFatal(err)
	}
}

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

func builtinCancelHandler(p *Peer) Handler {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var cp CancelParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &cp); err != nil {
				return nil, nil
			}
		}
		id, err := strconv.ParseUint(cp.RequestID, 10, 64)
		if err != nil || id == 0 || cp.RequestID != strconv.FormatUint(id, 10) {
			return nil, nil
		}
		p.mu.Lock()
		slots := append([]*cancelSlot(nil), p.cancels[id]...)
		p.mu.Unlock()
		for _, slot := range slots {
			slot.cancel()
		}
		return nil, nil
	}
}
