package auth

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/owner"
)

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
