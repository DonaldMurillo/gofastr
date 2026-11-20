package control

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Client is one attached controller; every transport implements it and the engine sees only this interface.
type Client interface {
	// ID is stable for the attach and is the Originator on events for turns it started.
	ID() ids.ClientID
	// IdentityClass says whether a human or an agent drives the client.
	IdentityClass() IdentityClass
	// Subscribe returns the client's events; the channel closes on detach or when ctx is done.
	Subscribe(ctx context.Context) <-chan EventEnvelope
	// Send delivers a command; it errors when the transport is closed or the command is invalid.
	Send(ctx context.Context, cmd Command) error
	// Close detaches the client without ending the session.
	Close() error
}

// ErrClientClosed is returned when a client method is called after Close.
var ErrClientClosed = errors.New("control: client closed")
