package render

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/kiln/noderender"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// RenderNode walks a world.Node tree and emits HTML. The implementation lives
// in the leaf package kiln/noderender (which imports only core-ui/html +
// core/render + kiln/world) so that generated/frozen apps can render node trees
// without dragging in Kiln's authoring engine (kiln/expr, kiln/effect,
// framework). This thin re-export keeps the live build-mode render path here.
func RenderNode(n world.Node) render.HTML { return noderender.RenderNode(n) }
