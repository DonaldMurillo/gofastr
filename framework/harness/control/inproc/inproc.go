// Package inproc implements the in-process Client transport: Go
// channels carrying canonical event envelopes from the engine's bus
// directly to bundled clients (TUI, web) and Commands from those
// clients back into the multiplexer.
//
// inproc is the only transport that runs entirely in one process —
// the others (rest, ws, mcpserver) are the same logical Client
// interface but with HTTP/WS/JSON-RPC framing.
package inproc

import (
	"context"
	"errors"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Client is an in-process implementation of control.Client. It bridges
// the engine's per-session Bus to a Go channel the client surface
// reads from.
type Client struct {
	id    ids.ClientID
	class control.IdentityClass

	bus *engine.Bus // event source

	// dispatcher is the multiplexer (or any control.Client-handling
	// thing). The mux.Dispatch method takes a Client and a Command;
	// inproc embeds the mux reference here.
	dispatcher Dispatcher

	closeOnce sync.Once
	closed    chan struct{}
}

// Dispatcher is the subset of *multiplex.Mux that inproc needs. We
// use an interface so this package doesn't import multiplex
// (which would import engine, then back-imports). Multiplex
// satisfies this implicitly.
type Dispatcher interface {
	Dispatch(ctx context.Context, c control.Client, cmd control.Command) error
}

// New returns an inproc Client.
//
//   - id      stable identity for permission and originator tracking
//   - class   human (TUI/web) or agent (a Go-level harness driver)
//   - bus     the engine.Bus for the session this client is attached to
//   - mux     the Dispatcher (typically *multiplex.Mux)
func New(id ids.ClientID, class control.IdentityClass, bus *engine.Bus, mux Dispatcher) *Client {
	return &Client{
		id:         id,
		class:      class,
		bus:        bus,
		dispatcher: mux,
		closed:     make(chan struct{}),
	}
}

// ID implements control.Client.
func (c *Client) ID() ids.ClientID { return c.id }

// IdentityClass implements control.Client.
func (c *Client) IdentityClass() control.IdentityClass { return c.class }

// Subscribe implements control.Client. Wraps the bus subscription so
// the channel closes when the Client is Closed.
func (c *Client) Subscribe(ctx context.Context) <-chan control.EventEnvelope {
	if c.bus == nil {
		ch := make(chan control.EventEnvelope)
		close(ch)
		return ch
	}
	// Bus already handles ctx cancellation; layer in our close channel.
	subCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-c.closed:
			cancel()
		case <-ctx.Done():
		}
	}()
	return c.bus.Subscribe(subCtx)
}

// Send implements control.Client by routing through the dispatcher.
func (c *Client) Send(ctx context.Context, cmd control.Command) error {
	if c.dispatcher == nil {
		return errors.New("inproc: no dispatcher wired")
	}
	select {
	case <-c.closed:
		return control.ErrClientClosed
	default:
	}
	return c.dispatcher.Dispatch(ctx, c, cmd)
}

// Close implements control.Client. Subsequent Send returns ErrClientClosed;
// Subscribe channels close.
func (c *Client) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

// IsClosed reports whether Close has been called.
func (c *Client) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
