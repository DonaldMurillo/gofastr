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
// It wraps a component and an SSE stream endpoint.
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
// Output: <div data-island="{id}">{component.Render()}</div>
//
// A nil Component renders as an empty island shell rather than panicking,
// the wrapper element still ships so client-side runtime hydration can
// find it. A Component whose Render() panics is recovered, logged, and
// surfaced to the client as an empty shell. The alternative, letting
// the panic propagate up the HTTP handler stack, would crash the
// goroutine and serve a 500 for any sibling component on the same page.
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

// RenderCtx is Render with a render context, for a component that
// reads one (component.ContextComponent). The component renders under
// the island's id namespace (render.WithIDNamespace with the island's
// ID), so an id it derives ("cart--features" for a section titled
// "Features" in island "cart") is the same on the page and in the RPC
// handler that re-renders the island, and apart from the page's own
// ids. The island is one id region (render.NewRegionBatch) inside its
// namespace, so an explicit id in it wins over a derived one wherever
// the two sit. Render the island with RenderCtx in both places. A panicking
// component gives the empty shell, as Render does.
func (is *Island) RenderCtx(ctx context.Context) render.HTML {
	if is == nil {
		return ""
	}
	shell := render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
	if is.Component == nil {
		return shell
	}
	// One id region inside the namespace: its explicit ids are reserved
	// before its derived ones are handed out, so Section{ID:
	// "cart--features"} keeps its id below an auto "Features" section,
	// on the page and in the RPC answer alike.
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
