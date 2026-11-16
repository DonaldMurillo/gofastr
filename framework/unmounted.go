package framework

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/unmounted"
)

// Unmounted lists the packages `gofastr unmount` copied into the app that this
// binary compiled in.
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
