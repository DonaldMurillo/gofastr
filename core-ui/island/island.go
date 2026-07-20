package island

import (
	"fmt"
	"log"
	"runtime/debug"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
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
// A nil Component renders as an empty island shell rather than panicking
// — the wrapper element still ships so client-side runtime hydration can
// find it. A Component whose Render() panics is recovered, logged, and
// surfaced to the client as an empty shell. The alternative — letting
// the panic propagate up the HTTP handler stack — would crash the
// goroutine and serve a 500 for any sibling component on the same page.
func (is *Island) Render() (out render.HTML) {
	if is == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("island: render panic for %q: %v\n%s", is.ID, r, debug.Stack())
			out = render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
		}
	}()
	if is.Component == nil {
		return render.Raw(fmt.Sprintf(`<div data-island="%s"></div>`, render.Escape(is.ID)))
	}
	inner := is.Component.Render()
	return render.Raw(fmt.Sprintf(`<div data-island="%s">%s</div>`, render.Escape(is.ID), string(inner)))
}
