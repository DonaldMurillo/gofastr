package framework

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/unmounted"
)

// Unmounted lists the gofastr packages this binary carries as unmounted
// copies: packages `gofastr unmount` copied into the app's own tree. Each
// copy registers itself from its generated unmounted.gen.go, so the list
// is exactly what was compiled in. A copy no longer receives framework
// fixes on upgrade; the list is how an operator or an agent finds them.
func (a *App) Unmounted() []unmounted.Copy {
	return unmounted.List()
}

func (a *App) toolUnmounted(_ context.Context, _ map[string]any) (any, error) {
	copies := a.Unmounted()
	return map[string]any{
		"unmounted": copies,
		"count":     len(copies),
	}, nil
}
