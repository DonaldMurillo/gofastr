package access

import (
	"context"
	"sync/atomic"
)

// Evaluation is one permission check that actually happened, whatever the
// verdict. A denial proves the boundary exists as well as a grant does —
// arguably better, since the interesting failure is a check that is never
// reached at all.
type Evaluation struct {
	// Permission is the permission string that was checked.
	Permission Permission
	// Granted is the verdict.
	Granted bool
	// Path names which entrypoint ran the check: "can",
	// "can-resource", or "require-permission". Useful when a permission
	// looks covered but only through the coarse path.
	Path string
	// Roles are the caller's effective roles at the moment of the check.
	// Resolved only when an observer is installed, so the request path
	// pays nothing for it in production.
	Roles []string
}

// observer is the process-wide evaluation hook. It is an atomic pointer
// rather than a mutex-guarded field because Can sits on the request hot
// path: an uninstalled observer costs one atomic load and a nil check,
// which is what production pays.
//
// This is a hook rather than a direct import of framework/semcov for the
// same reason router.SetServeHook is: access is a leaf package, and the
// coverage recorder is test tooling. Inverting it here keeps the leaf's
// dependency set at core/handler alone.
var observer atomic.Pointer[func(Evaluation)]

// SetObserver installs a callback fired for every permission evaluation.
// Pass nil to clear. Intended for test tooling — framework's semantic
// coverage recorder is the first consumer, and nothing installs one in a
// production binary.
//
// The callback runs inline on the request path, so it must be cheap and
// must not block. It must also tolerate concurrent calls.
func SetObserver(fn func(Evaluation)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

// observe reports an evaluation to the installed observer, if any, and
// returns the verdict unchanged so call sites can wrap their result.
//
// Role resolution happens inside the nil check: GetRoles walks the
// context, and doing that on every permission check in production — for
// a value nobody reads — would be a real cost for no benefit.
func observe(ctx context.Context, permission Permission, granted bool, path string) bool {
	if fn := observer.Load(); fn != nil {
		(*fn)(Evaluation{
			Permission: permission,
			Granted:    granted,
			Path:       path,
			Roles:      GetRoles(ctx),
		})
	}
	return granted
}
