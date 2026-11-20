package island

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Island is a server-driven widget that receives HTML updates via SSE.
type Island struct {
	ID        string              // unique island ID
	Component component.Component // the component that renders this island
}

// NewIsland creates a new server-driven island.
func NewIsland(id string, comp component.Component) *Island {
	return &Island{
		ID:        id,
		Component: comp,
	}
}

// Render renders the island's component wrapped in a data-island attribute.
func (is *Island) Render() (out render.HTML) {
	if is == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("island: render panic for %q: %s\n%s", is.ID, textsafe.Recovered(r), debug.Stack())
			out = render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
		}
	}()
	if is.Component == nil {
		return render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
	}
	inner := is.Component.Render()
	return render.Raw(fmt.Sprintf(`<div data-island="%s">%s</div>`, render.Escape(is.ID), string(inner)))
}

// RenderCtx is Render with a render context, for a component that reads one
// (component.ContextComponent).
func (is *Island) RenderCtx(ctx context.Context) render.HTML {
	if is == nil {
		return ""
	}
	shell := render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
	if is.Component == nil {
		return shell
	}
	ids := render.NewRegionBatch(render.WithIDNamespace(ctx, is.ID), 1)
	inner, err := component.SafeRenderCtx(ids.Context(0), is.Component)
	if err != nil {
		log.Printf("island: render failed for %q: %s", is.ID, textsafe.Recovered(err.Error()))
		return shell
	}
	ids.ReserveRendered(0, inner)
	inner = ids.Settle(0)(inner)
	return render.Raw(fmt.Sprintf(`<div data-island="%s">%s</div>`, render.Escape(is.ID), string(inner)))
}
