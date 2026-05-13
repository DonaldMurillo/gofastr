package island

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Island is a server-driven widget that receives HTML updates via SSE.
// It wraps a component and an SSE stream endpoint.
type Island struct {
	ID        string              // unique island ID
	Component component.Component // the component that renders this island
	SessionID string              // client session identifier
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
func (is *Island) Render() render.HTML {
	inner := is.Component.Render()
	return render.Raw(fmt.Sprintf(`<div data-island="%s">%s</div>`, render.Escape(is.ID), string(inner)))
}

// Update re-renders the component and returns the HTML fragment.
func (is *Island) Update() render.HTML {
	return is.Render()
}
