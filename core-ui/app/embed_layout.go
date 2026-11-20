package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// EmbedLayoutName is the layout name embedded surfaces render under.
const EmbedLayoutName = "embed"

// EmbedLayout returns the chrome-less layout embedded surfaces render under.
func EmbedLayout() *Layout {
	return NewLayout(EmbedLayoutName, LayoutSpec{}, func(ctx context.Context, l *LayoutTree) render.HTML {
		return l.Primary()
	})
}
