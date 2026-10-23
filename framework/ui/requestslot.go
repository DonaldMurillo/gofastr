package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// requestSlot is a mounted widget's slot built per request. A widget is
// mounted once at startup, but its body renders on each page that
// carries it and on each open, and the widget server hands the slot
// that request's context. build makes the slot for that context, so
// the words it says follow the reader's locale rather than the context
// the widget was mounted with, which has no reader at all.
type requestSlot struct {
	build func(ctx context.Context) component.Component
}

// RenderCtx renders the slot build makes for ctx.
func (s requestSlot) RenderCtx(ctx context.Context) render.HTML {
	if ctx == nil {
		ctx = context.Background()
	}
	return s.build(ctx).Render()
}

// Render is RenderCtx with no reader: the English slot.
func (s requestSlot) Render() render.HTML { return s.RenderCtx(context.Background()) }
