package access_test

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// docPolicy mirrors the EXACT signature shown in
// framework/docs/content/access-control.md → "The Policy interface".
// If the doc drifts from the real 2-arg interface, this stops compiling.
type docPolicy struct{}

func (docPolicy) Can(ctx context.Context, permission access.Permission) bool {
	return false
}

var _ access.Policy = docPolicy{}
