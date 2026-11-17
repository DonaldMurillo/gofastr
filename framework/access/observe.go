package access

import (
	"context"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Evaluation is one permission check that ran, whatever its verdict.
type Evaluation struct {
	// Permission is the permission checked.
	Permission Permission
	// Granted is the verdict.
	Granted bool
	// Path names the entrypoint: "can", "can-resource", "can-resource-exact"
	// or "require-permission".
	Path string
	// Roles are the caller's roles at the check.
	Roles []string
}

var observer atomic.Pointer[func(Evaluation)]

// SetObserver installs fn to run inline on every permission evaluation; nil
// clears it. For test tooling. A panic in fn is recovered and logged.
func SetObserver(fn func(Evaluation)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

func observe(ctx context.Context, permission Permission, granted bool, path string) bool {
	if fn := observer.Load(); fn != nil {
		func() {
			defer textsafe.LogPanic(ctx, "access: observer panicked; ignored")
			(*fn)(Evaluation{
				Permission: permission,
				Granted:    granted,
				Path:       path,
				Roles:      GetRoles(ctx),
			})
		}()
	}
	return granted
}
