package control

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Client is the transport-agnostic abstraction the multiplexer uses
// to route commands and events. Every transport (inproc, rest, ws,
// mcpserver) provides a Client implementation.
//
// Per hard rule 7, the engine sees only this interface — it never
// inspects how a client is wired.
type Client interface {
	// ID returns the client's stable identifier for the lifetime of
	// the attach. Cross-references the OriginatorID embedded in
	// event envelopes the engine emits for turns this client started.
	ID() ids.ClientID

	// IdentityClass distinguishes human-driven clients from
	// agent-driven ones. Permission middleware honors this.
	IdentityClass() IdentityClass

	// Subscribe returns a receive-only channel of events broadcast
	// to this client. The channel is closed when the client is
	// detached or the context is done.
	Subscribe(ctx context.Context) <-chan EventEnvelope

	// Send delivers a command from the client to the multiplexer.
	// Returns an error if the transport is closed or the command
	// fails wire-format validation.
	Send(ctx context.Context, cmd Command) error

	// Close detaches the client. Detach is non-destructive at the
	// engine level — see § Multi-client semantics.
	Close() error
}

// ErrClientClosed is returned when a client method is called after Close.
var ErrClientClosed = errors.New("control: client closed")
