// Get returns the context's owner id, or (nil, false) with no extractor or no owner.
// Package owner is the seam CRUD reads a row's owner through: Get, and the extractor an auth battery installs with SetExtractor.
package owner

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Extractor returns the current request's owner id, ok=false when there is none.
type Extractor func(ctx context.Context) (id any, ok bool)

type crossOwnerKey struct{}

// AllowCrossOwner lifts owner scoping for in-process CrudHandler calls; set it only from server-side Go you have gated, never from client input.
func AllowCrossOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, crossOwnerKey{}, true)
}

// IsCrossOwner reports whether ctx was marked by AllowCrossOwner.
func IsCrossOwner(ctx context.Context) bool {
	v, _ := ctx.Value(crossOwnerKey{}).(bool)
	return v
}

var extractor atomic.Pointer[Extractor]

// SetExtractor installs the process-wide extractor (nil clears), warning when it replaces one; call it only at init.
func SetExtractor(fn Extractor) {
	prev := extractor.Load()
	if fn == nil {
		extractor.Store(nil)
		return
	}
	extractor.Store(&fn)
	if prev != nil {
		slog.Default().Warn("framework/owner: SetExtractor replaced an existing extractor: likely an import-order accident between two batteries that both register one. The last-call-wins extractor is the one currently active.",
			"component", "framework/owner")
	}
}

// GetExtractor returns the installed extractor, or nil.
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
