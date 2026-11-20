// Package uinodev1 is the closed ui.node.v1 wire type and its validator, for
// screen trees from process-isolated third-party modules. Callers reading the
// tree from an unbounded source cap the reader before Validate.
package uinodev1

import "github.com/DonaldMurillo/gofastr/core/render"

// Renderer maps a validated Tree to host-owned HTML. It assigns every id,
// class, ARIA attribute and variant, resolves each ActionRef against the
// module's installed routes, and returns an error for any node or ref it
// cannot map.
type Renderer interface {
	Render(t *Tree) (render.HTML, error)
}
