package access

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// RequestUserRoles returns the roles of the signed-in request user, or
// nil when there is no user or the user type carries no GetRoles. It is
// the role callback a generated app hands to Middleware and
// ui.SetRolesExtractor.
func RequestUserRoles(ctx context.Context) []string {
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		if rh, ok := u.(interface{ GetRoles() []string }); ok {
			return rh.GetRoles()
		}
	}
	return nil
}
