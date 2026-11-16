package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type requestSlot struct {
	build func(ctx context.Context) component.Component
}

func (s requestSlot) RenderCtx(ctx context.Context) render.HTML {
	if ctx == nil {
		ctx = context.Background()
	}
	return s.build(ctx).Render()
}

func (s requestSlot) Render() render.HTML { return s.RenderCtx(context.Background()) }
