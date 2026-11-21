package access

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// UserID returns the signed-in user's id, or "" when the context carries
// no user or the user type has no string id. The GetID() string
// convention is the repo-wide shape of a user; callers that key data or
// audit rows on the caller use this one extractor rather than each
// spelling the type assertion.
func UserID(ctx context.Context) string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return ""
	}
	if id, ok := u.(interface{ GetID() string }); ok {
		return id.GetID()
	}
	return ""
}
