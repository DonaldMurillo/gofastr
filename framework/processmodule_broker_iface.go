package framework

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// ReverseBroker installs the host.* reverse-request handlers on a child's
// [moduleproto.Peer] and mints per-request delegation handles.
type ReverseBroker interface {
	// InstallHandlers registers the host.* handlers on p for the life of
	// this child connection, scoped to view.
	InstallHandlers(p *moduleproto.Peer, view ModuleGrantView)

	// MintDelegation issues a replica-local opaque handle for a proxied
	// call's [moduleproto.Caller]; the child echoes it on reverse calls. The
	// caller must invoke release when the parent call completes. r may be
	// nil for ambient (caller-less) module work.
	MintDelegation(r *http.Request, parentCallID uint64) (handle string, release func())
}

// ModuleGrantView is the grant snapshot the broker enforces for one child
// connection.
type ModuleGrantView struct {
	// Name is the module name.
	Name string

	// Grants is the effective grant set: requested ∩ approved, with
	// non-grantable scopes removed.
	Grants []access.Permission

	// Generation is the desired_generation the spawn read.
	Generation uint64
}

// NopBroker is the [ReverseBroker] that denies every reverse host.* call
// with a capability error and mints an empty delegation handle.
type NopBroker struct{}

var nopBrokerDeniedMethods = []string{
	moduleproto.MethodHostEntityQuery,
	moduleproto.MethodHostEntityCreate,
	moduleproto.MethodHostEntityUpdate,
	moduleproto.MethodHostEntityDelete,
	moduleproto.MethodHostSearchQuery,
	moduleproto.MethodHostEventEmit,
}

// InstallHandlers registers deny-all handlers on p for every host.* method.
func (NopBroker) InstallHandlers(p *moduleproto.Peer, _ ModuleGrantView) {
	if p == nil {
		return
	}
	for _, m := range nopBrokerDeniedMethods {
		mm := m
		_ = p.Handle(mm, func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, errNopBrokerDenied(mm)
		})
	}
}

// MintDelegation returns an empty handle and a no-op release.
func (NopBroker) MintDelegation(_ *http.Request, _ uint64) (string, func()) {
	return "", func() {}
}

type nopBrokerDeniedError struct{ Method string }

func (e *nopBrokerDeniedError) Error() string {
	return "moduleproto: reverse call denied by NopBroker: " + e.Method
}

func errNopBrokerDenied(method string) error {
	return &nopBrokerDeniedError{Method: method}
}
