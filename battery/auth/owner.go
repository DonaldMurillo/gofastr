package auth

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// init wires battery/auth into framework/owner so that any host importing
// auth automatically gets per-request owner-id extraction for CRUD's
// EntityConfig.OwnerField scoping. The extractor reads the User stashed
// on the context by RequireAuth (JWT path) or SessionMiddleware (cookie
// path) and returns its GetID().
//
// Hosts that don't use battery/auth never pay for this: framework/owner
// stays in its zero state. Hosts that DO use auth get owner scoping
// "for free" the moment they set EntityConfig.OwnerField on an entity.
func init() {
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		u := GetCurrentUser(ctx)
		if u == nil {
			return nil, false
		}
		id := u.GetID()
		if id == "" {
			return nil, false
		}
		return id, true
	})
}
