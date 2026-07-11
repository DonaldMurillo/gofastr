// Package owner provides a single seam for "who owns this row" lookups
// during CRUD operations. CRUD calls owner.Get(ctx) to discover the
// current owner id; a battery (typically battery/auth) registers the
// extractor at init time so the framework core stays free of any
// authentication dependency.
//
// The default state is "no extractor" — owner.Get always returns
// (nil, false). Hosts that never wire an extractor see no behavioural
// change: EntityConfig.OwnerField stays inert and CRUD operates as
// before. Hosts that import battery/auth pick up the extractor
// automatically.
package owner

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Extractor returns the identity of the current request's owner — typically
// a user id pulled from request context. ok=false means no owner is
// associated with this context (anonymous request, background job, etc.).
type Extractor func(ctx context.Context) (id any, ok bool)

// crossOwnerKey marks a context as deliberately permitted to read across
// owners. It must ONLY ever be set server-side, in Go, via AllowCrossOwner —
// never derived from a client-supplied header, query param, or body — or it
// becomes an owner-isolation bypass. The key type is unexported precisely so
// no HTTP-derived context can ever carry it: there is no way to set it except
// by calling AllowCrossOwner from your own process code.
type crossOwnerKey struct{}

// AllowCrossOwner returns a context that lifts owner scoping for the Go-level
// CrudHandler methods (ListAll, CountAll, GetOne — and, because they share the
// same scope helpers, the mutate-by-id methods UpdateOne/DeleteOne and their
// batch variants). Reads then span every owner's rows instead of being
// confined to the signed-in user. This is the sanctioned escape for
// app-legitimate cross-owner work — e.g. "spots remaining = capacity −
// COUNT(bookings for this class across ALL members)", or reading the whole
// waitlist to "promote the oldest waitlisted booking" (which belongs to
// another member) — that would otherwise force raw SQL against
// framework-managed tables.
//
// SECURITY:
//   - Set this ONLY from your own server-side Go (a service method, cron job,
//     or admin action you have already gated with a permission check). There
//     is NO built-in role check here.
//   - NEVER pass through a context that came from client-controlled input, and
//     never plumb it onto the request context of an auto-CRUD HTTP route.
//   - The auto-generated HTTP CRUD endpoints have no path to this marker and
//     stay owner-scoped, always. It is an escape for the in-process Go API
//     surface only.
//
// For a declarative, RBAC-checked widening that applies to BOTH HTTP and
// in-process reads, see EntityConfig.CrossOwnerRead (names a permission;
// fail-closed; reads only).
func AllowCrossOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, crossOwnerKey{}, true)
}

// IsCrossOwner reports whether ctx was explicitly marked for cross-owner
// access via AllowCrossOwner.
func IsCrossOwner(ctx context.Context) bool {
	v, _ := ctx.Value(crossOwnerKey{}).(bool)
	return v
}

// extractor is stored in an atomic so reads from concurrent request
// handlers don't race with the (rare) init-time SetExtractor call.
var extractor atomic.Pointer[Extractor]

// SetExtractor installs the global owner extractor. Subsequent calls
// replace the previous extractor. Pass nil to clear. Emits a WARN log
// when REPLACING an existing non-nil extractor — that's almost always
// an import-order accident (two packages both calling SetExtractor,
// whichever Go's init order picks runs last and silently overrides
// the other). Operators see the warning and can fix the wiring.
//
// CONTRACT: call ONLY from package init() or early process startup.
// Calling SetExtractor while requests are in flight is racy in a
// specific way: the swap itself is atomic, but a single request might
// observe a different extractor in two consecutive Get() calls (e.g.
// ApplyOwnerScope for the read query then InjectOwner for a create).
// That can produce a row scoped to one owner and stamped with another.
// Don't do it.
func SetExtractor(fn Extractor) {
	prev := extractor.Load()
	if fn == nil {
		extractor.Store(nil)
		return
	}
	extractor.Store(&fn)
	if prev != nil {
		slog.Default().Warn("framework/owner: SetExtractor replaced an existing extractor — likely an import-order accident between two batteries that both register one. The last-call-wins extractor is the one currently active.",
			"component", "framework/owner")
	}
}

// GetExtractor returns the currently installed extractor (or nil).
// Useful for tests that want to save / restore the previous extractor.
func GetExtractor() Extractor {
	p := extractor.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Get returns the current owner id for the given context. It returns
// (nil, false) when no extractor is registered or when the extractor
// reports no owner.
func Get(ctx context.Context) (any, bool) {
	p := extractor.Load()
	if p == nil {
		return nil, false
	}
	return (*p)(ctx)
}
