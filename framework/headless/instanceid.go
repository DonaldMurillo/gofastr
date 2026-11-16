package headless

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func instanceID(ctx context.Context, explicit, derived string, family ...string) string {
	if explicit != "" {
		render.ReserveID(ctx, explicit, family...)
		return explicit
	}
	return render.UniqueID(ctx, derived, family...)
}
