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
