package framework

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// ReverseBroker installs the host.* reverse-request handlers on a child's
// [moduleproto.Peer] and mints per-request delegation handles. It is the
// capability broker for the module's reverse channel (design §5).
//
// The supervisor CONSUMES this interface; a later wave IMPLEMENTS it against
// the CRUD re-dispatch chokepoint (framework/crud/mcp.go's machinery, plus
// the [access.ScopeMatch] module-grant pre-filter and the CrossOwnerRead
// carve-out). Until that lands the supervisor wires [NopBroker], which denies
// every reverse call with a capability error — fail-closed by default.
//
// The shape is deliberately small and stable: install handlers once per
// child connection (handing the broker the live Peer + the child's grant
// view), and mint a delegation handle per inbound proxied call so reverse
// calls can re-attach the originating request's caller context.
type ReverseBroker interface {
	// InstallHandlers registers the host.* reverse-request handlers on p
	// for the lifetime of this child connection. The handler set is scoped
	// to view: a reverse call's derived required permission must be in the
	// module-grant view (intersected with the caller's authority on the
	// delegated path), else the broker denies before re-dispatch. The
	// CrossOwnerRead / cross-tenant carve-out (design §5) is enforced on
	// BOTH the module-grant path and the delegated-caller path.
	InstallHandlers(p *moduleproto.Peer, view ModuleGrantView)

	// MintDelegation issues an in-memory, replica-local opaque handle that
	// the supervisor attaches to a proxied call's [moduleproto.Caller].
	// The child echoes it on reverse host.* calls so the broker re-attaches
	// the originating request's caller context to the internal re-dispatch.
	// The returned release func MUST be invoked when the parent call
	// completes (including buffered-503 crash paths) so the handle table
	// does not leak. r may be nil for ambient (caller-less) module work.
	MintDelegation(r *http.Request, parentCallID uint64) (handle string, release func())
}

// ModuleGrantView is the grant snapshot the broker enforces for one child
// connection. It is the supervisor's effective-grant set for the module at
// the spawn's read desired_generation — the binding the live stdio
// connection authenticates (design §5 "binding + revocation").
type ModuleGrantView struct {
	// Name is the module name (descriptor-supplied, operator-approved).
	Name string

	// Grants is the effective grant set (descriptor.requested ∩
	// operator.approved), with the non-grantable carve-out (design §5)
	// already applied at install time.
	Grants []access.Permission

	// Generation is the ProcessModuleStore's desired_generation read at
	// spawn. The supervisor rejects a reverse call whose echoed generation
	// no longer matches the store's current value — the revoke path.
	Generation uint64
}

// NopBroker is the no-op [ReverseBroker]: it installs handlers that deny
// every reverse host.* call with a capability error, and mints an empty
// delegation handle whose release is a no-op. It is the supervisor default
// until the real broker lands (design §5) and the test fake when a test
// does not exercise the reverse channel.
//
// Fail-closed: a child that issues host.entity.query / host.search.query /
// host.event.emit under a NopBroker receives a JSON-RPC error response for
// every call — no host data is brokered, ambient or delegated.
type NopBroker struct{}

// nopBrokerDeniedMethods is the full reverse catalog (design §4.4). A NopBroker
// refuses each one with errNopBrokerDenied.
var nopBrokerDeniedMethods = []string{
	moduleproto.MethodHostEntityQuery,
	moduleproto.MethodHostEntityCreate,
	moduleproto.MethodHostEntityUpdate,
	moduleproto.MethodHostEntityDelete,
	moduleproto.MethodHostSearchQuery,
	moduleproto.MethodHostEventEmit,
}

// InstallHandlers registers deny-all handlers on p for every host.* method
// in the moduleproto catalog. The view is accepted but unused — a NopBroker
// grants nothing by construction.
func (NopBroker) InstallHandlers(p *moduleproto.Peer, _ ModuleGrantView) {
	if p == nil {
		return
	}
	for _, m := range nopBrokerDeniedMethods {
		mm := m // capture for the closure
		_ = p.Handle(mm, func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, errNopBrokerDenied(mm)
		})
	}
}

// MintDelegation returns an empty handle and a no-op release. Under NopBroker
// no reverse call can succeed anyway, so the handle is unused; the empty
// string is a legal Caller.Delegation value (omitted on the wire).
func (NopBroker) MintDelegation(_ *http.Request, _ uint64) (string, func()) {
	return "", func() {}
}

// nopBrokerDeniedError is the capability error every NopBroker reverse handler
// returns. The wire form is a moduleproto.Error with [moduleproto.CodeInternalError]
// (Peer.serveRequest maps a non-*Error return into that code) — the supervisor's
// reverse path does not depend on the exact code because the caller is always
// the child, which surfaces the error in its own response.
type nopBrokerDeniedError struct{ Method string }

func (e *nopBrokerDeniedError) Error() string {
	return "moduleproto: reverse call denied by NopBroker: " + e.Method
}

func errNopBrokerDenied(method string) error {
	return &nopBrokerDeniedError{Method: method}
}
