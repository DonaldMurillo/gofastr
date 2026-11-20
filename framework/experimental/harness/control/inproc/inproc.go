package inproc

import (
	"context"
	"errors"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Client is the in-process control.Client: events come off a session's Bus, commands go to a Dispatcher.
type Client struct {
	id    ids.ClientID
	class control.IdentityClass

	bus        *engine.Bus
	dispatcher Dispatcher

	closeOnce sync.Once
	closed    chan struct{}
}

// Dispatcher routes a client's command; *multiplex.Mux is one.
type Dispatcher interface {
	Dispatch(ctx context.Context, c control.Client, cmd control.Command) error
}

// New returns a Client reading events from bus and sending commands through mux.
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

// Subscribe implements control.Client; the channel closes when ctx ends or the Client closes.
func (c *Client) Subscribe(ctx context.Context) <-chan control.EventEnvelope {
	if c.bus == nil {
		ch := make(chan control.EventEnvelope)
		close(ch)
		return ch
	}
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

// Close implements control.Client; later Sends return ErrClientClosed.
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
